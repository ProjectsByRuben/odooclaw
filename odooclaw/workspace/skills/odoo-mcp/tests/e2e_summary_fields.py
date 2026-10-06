"""End-to-end check for the dynamic summary fields (odooclaw#81).

Runs the real MCP tools against a real Odoo — login, ``fields_get`` discovery,
the ``/odooclaw/call_kw_as_user`` delegation, the summary — and asserts what the
hardcoded implementation could not deliver.

Required environment (same convention as ``get_odoo_client``)::

    ODOO_URL  ODOO_DB  ODOO_USERNAME  ODOO_PASSWORD  ODOOCLAW_REPLY_TOKEN

Optional: ``E2E_PARTNER_NAME`` (default ``E2E Mobile Co`` — a partner with
``mobile`` filled in).

Exit code 0 when every check passes, 1 otherwise. Run it once per instance::

    PYTHONPATH=src python tests/e2e_summary_fields.py
"""

import json
import os
import sys

sys.path.append(os.path.join(os.path.dirname(__file__), "../src"))

from odoo_mcp.server import (  # noqa: E402
    get_odoo_client,
    odoo_get_record_summary,
    odoo_get_partner_summary,
    odoo_search,
)

PARTNER_NAME = os.environ.get("E2E_PARTNER_NAME", "E2E Mobile Co")
# A model that exists on any Odoo and was never in the old SUMMARY_FIELDS table.
PROBE_MODEL = os.environ.get("E2E_PROBE_MODEL", "res.country")

_results = []


def check(name, ok, detail=""):
    _results.append(ok)
    mark = "\033[92m[PASS]\033[0m" if ok else "\033[91m[FAIL]\033[0m"
    print(f"{mark} {name:<58} {detail}")


def _schema(client, model, uid):
    try:
        return client.call_kw(model, "fields_get", sender_id=uid) or {}
    except Exception:
        return {}


def run():
    print("\nE2E - dynamic summary fields (github.com/nicolasramos/odooclaw#81)\n")
    print(f"ODOO_URL={os.environ.get('ODOO_URL')}  ODOO_DB={os.environ.get('ODOO_DB')}\n")

    try:
        client = get_odoo_client()
        uid = client.odoo_session.get_uid()
    except Exception as exc:  # noqa: BLE001
        check("login", False, str(exc))
        return

    partner_schema = _schema(client, "res.partner", uid)
    check("login (session established)", bool(uid), f"uid={uid}")
    check("instance exposes fields_get", bool(partner_schema), f"{len(partner_schema)} fields")

    # --- the partner -------------------------------------------------------
    ids = odoo_search(
        model="res.partner", domain=[["name", "=", PARTNER_NAME]], limit=1, sender_id=uid
    )
    if not ids:
        check(f"found partner '{PARTNER_NAME}'", False, "create it in the instance first")
        return
    partner_id = ids[0]

    summary = odoo_get_partner_summary(partner_id=partner_id, sender_id=uid)
    print("\npartner summary ->\n" + json.dumps(summary, indent=2, default=str)[:1600] + "\n")

    check(
        "partner summary returns 'mobile' (#81's exact symptom)",
        summary.get("mobile") == os.environ.get("E2E_MOBILE", "+34600000001"),
        f"mobile={summary.get('mobile')!r}",
    )
    check(
        "fields outside the old hardcoded list are reported",
        bool(summary.get("street")) and bool(summary.get("city")),
        f"street={summary.get('street')!r} city={summary.get('city')!r}",
    )
    check("summary carries the record id", summary.get("id") == partner_id, f"id={summary.get('id')}")

    empty = [k for k, v in summary.items() if v in ("", [], {}, None) and k not in ("open_sale_orders", "open_invoices")]
    check("no empty values in the summary", not empty, f"empty={empty}")

    forbidden = [
        k
        for k in summary
        if k in ("child_ids", "message_ids", "image_1920", "password")
        or k.startswith("_")
    ]
    check("no collections / binaries / technical fields", not forbidden, f"present={forbidden}")

    # --- the guards ---------------------------------------------------------
    has_account = "credit" in partner_schema
    has_sale = bool(_schema(client, "sale.order", uid))
    has_account_move = bool(_schema(client, "account.move", uid))

    check(
        "financial_balance present iff accounting is installed",
        ("financial_balance" in summary) == has_account,
        f"account={has_account} balance={summary.get('financial_balance')!r}",
    )
    check(
        "open_sale_orders is a count iff sale is installed (else null)",
        (isinstance(summary.get("open_sale_orders"), int) if has_sale else summary.get("open_sale_orders") is None),
        f"sale={has_sale} value={summary.get('open_sale_orders')!r}",
    )
    check(
        "open_invoices is a count iff accounting is installed (else null)",
        (isinstance(summary.get("open_invoices"), int) if has_account_move else summary.get("open_invoices") is None),
        f"account.move={has_account_move} value={summary.get('open_invoices')!r}",
    )
    check(
        "credit/debit folded into the balance, never reported raw",
        ("credit" not in summary and "debit" not in summary) if has_account else True,
        f"credit={summary.get('credit')!r} debit={summary.get('debit')!r}",
    )

    # --- generic record summary --------------------------------------------
    probe_ids = odoo_search(model=PROBE_MODEL, domain=[], limit=1, sender_id=uid)
    probe = (
        odoo_get_record_summary(model=PROBE_MODEL, res_id=probe_ids[0], sender_id=uid)
        if probe_ids
        else {}
    )
    print(f"\n{PROBE_MODEL} summary ->\n" + json.dumps(probe, indent=2, default=str)[:900] + "\n")
    check(
        f"record summary of {PROBE_MODEL} uses the live schema (not display_name only)",
        len([k for k in probe if k != "id"]) > 1,
        f"keys={sorted(probe)}",
    )

    unknown = odoo_get_record_summary(model="does.not.exist", res_id=1, sender_id=uid)
    check(
        "unknown model answers with an actionable error",
        isinstance(unknown, dict)
        and "error" in unknown
        and "odoo_get_model_schema" in unknown["error"],
        f"error={str(unknown.get('error', ''))[:90]!r}",
    )

    missing = odoo_get_partner_summary(partner_id=999999999, sender_id=uid)
    check(
        "missing partner is an error dict, not a crash",
        isinstance(missing, dict) and "error" in missing,
        f"error={missing.get('error') if isinstance(missing, dict) else missing!r}",
    )


def main():
    try:
        run()
    except Exception as exc:  # noqa: BLE001 - a raised error is a failed suite
        check("unexpected exception", False, f"{type(exc).__name__}: {exc}")
    passed = sum(1 for r in _results if r)
    print(f"\n{passed}/{len(_results)} checks passed")
    sys.exit(0 if _results and all(_results) else 1)


if __name__ == "__main__":
    main()
