from typing import Any, Dict
from odoo_mcp.core.client import OdooClient
from odoo_mcp.core.serializers import serialize_schema
from odoo_mcp.services.capability_service import get_capabilities
import logging

_logger = logging.getLogger(__name__)


def odoo_model_schema(client: OdooClient, user_id: int, model: str, field_names=None) -> str:
    """Retrieve and serialize fields schema for a given model."""
    try:
        if field_names is not None and (not isinstance(field_names, list) or not 1 <= len(field_names) <= 50
                                       or any(not isinstance(name, str) or not name for name in field_names)):
            raise ValueError("field_names must contain 1-50 field names")
        fields_info = client.call_kw(model, "fields_get", args=[field_names] if field_names else [],
                                    kwargs={"attributes": ["type", "string", "required", "readonly", "relation", "selection"]},
                                    sender_id=user_id)
        summary = {}
        for fname, fprops in fields_info.items():
            summary[fname] = {
                "type": fprops.get("type"),
                "string": fprops.get("string"),
                "required": fprops.get("required", False),
                "readonly": fprops.get("readonly", False),
            }
            if fprops.get("type") in ["many2one", "one2many", "many2many"]:
                summary[fname]["relation"] = fprops.get("relation")
            if fprops.get("type") == "selection":
                summary[fname]["selection"] = fprops.get("selection")

        return serialize_schema({"model": model, "fields": summary})
    except Exception as e:
        _logger.error(f"Error getting schema for {model}: {e}")
        return serialize_schema({"error": str(e), "model": model})


def odoo_get_capabilities(client: OdooClient, user_id: int) -> Dict[str, Any]:
    return get_capabilities(client, user_id)
