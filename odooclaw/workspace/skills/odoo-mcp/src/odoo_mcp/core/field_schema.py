"""Live discovery of the fields an Odoo instance actually exposes.

Why this exists
---------------
Hardcoded field lists are wrong on a real deployment in two different ways:

* they are never exhaustive. ``mobile`` exists on every Odoo, it was not in the
  list, and the bot answered "that field is not available" for a partner whose
  mobile was filled in (github.com/nicolasramos/odooclaw#81).
* they keep fields this instance does not have. Odoo's ``read`` raises
  ``ValueError: Invalid field ...`` for those, so on an instance without
  ``account`` the partner summary failed outright on ``credit`` / ``debit``.

``fields_get`` answers both questions for the *caller*: Odoo skips every field
the user may not read (``field.is_accessible``), so the schema it returns is
already permission-aware, and it is the same call the ``odoo_get_model_schema``
tool exposes. The result is cached per model for 60 seconds — the same TTL the
record field validation already used — so a summary costs at most one extra RPC
per model per minute.
"""

from __future__ import annotations

import logging
import time
from typing import Any, Dict, Iterable, List, Optional

_logger = logging.getLogger(__name__)

FIELD_CACHE_TTL_SECONDS = 60.0

#: ``{model: {field_name: fields_get properties}}``, shared by the summary
#: services and by the field validation in ``tools.records``.
_field_cache: Dict[str, Dict[str, Any]] = {}
_field_cache_timestamps: Dict[str, float] = {}

#: Field types a summary must not carry: binaries (images, attachments) are
#: base64 blobs, and a one2many value is a list of ids that runs to thousands
#: on any record with chatter (``message_ids``). The model can still be read
#: explicitly through ``odoo_read`` when the caller really wants them.
_NON_SUMMARY_TYPES = frozenset({"binary", "one2many"})

#: Never surfaced in a summary, whatever the schema says.
EXCLUDED_SUMMARY_FIELDS = frozenset({"password"})

#: Fields kept when a full read has to be retried: the smallest set that still
#: identifies the record on any model.
_FALLBACK_FIELDS = ("name", "display_name", "id")


def get_field_schema(
    client: Any, model: str, sender_id: Optional[int] = None
) -> Dict[str, Any]:
    """Return ``fields_get(model)`` for the caller, cached for 60 seconds.

    An unknown model, a permission failure or a transport error all yield an
    empty schema instead of raising: every caller uses this to *decide* what to
    ask Odoo for, and "this instance does not have it" is a valid answer.
    """
    cached_at = _field_cache_timestamps.get(model)
    if cached_at is not None and (time.monotonic() - cached_at) < FIELD_CACHE_TTL_SECONDS:
        return _field_cache[model]

    try:
        fields_info = client.call_kw(model, "fields_get", sender_id=sender_id)
        schema = fields_info if isinstance(fields_info, dict) else {}
    except Exception as exc:  # noqa: BLE001 - degraded schema, never a crash
        _logger.info("fields_get failed for %s: %s", model, exc)
        schema = {}

    _field_cache[model] = schema
    _field_cache_timestamps[model] = time.monotonic()
    return schema


def reset_field_cache() -> None:
    """Drop every cached schema (tests, and the ``reset_*_cache`` MCP tool)."""
    _field_cache.clear()
    _field_cache_timestamps.clear()


def select_summary_fields(
    client: Any,
    model: str,
    sender_id: Optional[int] = None,
    exclude: Iterable[str] = (),
) -> List[str]:
    """The fields a summary of *model* should read on this instance.

    Technical fields (``_``-prefixed, which also covers ``__``-prefixed synthetic
    keys), binaries, one2many collections and :data:`EXCLUDED_SUMMARY_FIELDS`
    are dropped; everything else the instance exposes — custom fields, fields
    from optional modules, fields that only exist in some Odoo versions — is
    kept. Returns an empty list when the schema is unavailable, so callers can
    tell "no fields" from "no record".
    """
    schema = get_field_schema(client, model, sender_id)
    if not schema:
        return []
    denied = EXCLUDED_SUMMARY_FIELDS | set(exclude)
    return [
        name
        for name, props in schema.items()
        if not name.startswith("_")
        and name not in denied
        and (props or {}).get("type") not in _NON_SUMMARY_TYPES
    ]


def drop_empty_fields(values: Dict[str, Any]) -> Dict[str, Any]:
    """Keep only non-empty values.

    ``False``/``0``/``""``/``[]``/``None`` are dropped — a summary is what is
    *there*, and a caller who needs the absent fields can ask for them.
    """
    return {key: value for key, value in values.items() if value}


def read_summary_record(
    client: Any,
    model: str,
    res_id: int,
    fields: List[str],
    sender_id: Optional[int] = None,
) -> Optional[Dict[str, Any]]:
    """Read one record, degrading instead of failing.

    Returns the raw record (``id`` included, values untouched) or ``None`` when
    the record does not exist. When the read of the discovered fields fails —
    one broken compute on one field must not sink the whole summary — it is
    retried with the minimal universally-available set; if that fails too, the
    original error propagates rather than being swallowed.
    """
    requested = list(fields) or list(_FALLBACK_FIELDS)
    try:
        records = _read(client, model, res_id, requested, sender_id)
    except Exception as exc:
        schema = get_field_schema(client, model, sender_id)
        minimal = [
            f for f in _FALLBACK_FIELDS if f in schema and f in requested
        ]
        if not minimal or len(minimal) >= len(requested):
            raise
        _logger.warning(
            "summary read of %s[%s] failed on %s; retrying with %s",
            model,
            res_id,
            exc,
            minimal,
        )
        records = _read(client, model, res_id, minimal, sender_id)
    if not records:
        return None
    return records[0]


def _read(
    client: Any,
    model: str,
    res_id: int,
    fields: List[str],
    sender_id: Optional[int],
) -> List[Dict[str, Any]]:
    return client.call_kw(
        model,
        "read",
        args=[[res_id]],
        kwargs={"fields": fields},
        sender_id=sender_id,
    )
