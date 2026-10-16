from typing import Dict, Any, List, Optional
from odoo_mcp.core.exceptions import OdooSecurityError
from .policy import get_denied_write_fields, DEFAULT_DENIED_MODELS

def guard_model_access(model_name: str, client=None, sender_id: Optional[int] = None) -> None:
    """Protect bridge-internal models; native Odoo checks authorize operations.

    Model discovery visibility does not determine whether a user may operate on
    a business record. Every call still requires the inbound user's capability.
    """
    # Blacklist always wins - even if model is in escape hatch
    if model_name in DEFAULT_DENIED_MODELS:
        raise OdooSecurityError(
            f"Model '{model_name}' is not authorized (denied by security policy)."
        )
    
    # Schema visibility is not authorization: some users cannot list ir.model
    # but can work on business models. The capability-bound RPC applies native
    # model ACLs, record rules and field permissions for every actual operation.
    if not isinstance(model_name, str) or not model_name.strip():
        raise OdooSecurityError("Provide an installed Odoo model name.")


def guard_write_fields(values: Dict[str, Any]) -> None:
    """Raises OdooSecurityError if attempting to write to a restricted field."""
    denied = get_denied_write_fields()
    for field in values.keys():
        if field in denied:
            raise OdooSecurityError(f"Field '{field}' is restricted from direct write operations via MCP.")

def guard_unlink(model_name: str) -> None:
    """Deletion is delegated to Odoo; protect only bridge-internal models."""
    if model_name in DEFAULT_DENIED_MODELS:
        raise OdooSecurityError(f"Model '{model_name}' is internal to the bridge.")

def guard_action_name(method: str) -> None:
    """Ensures that invoked methods are safe workflow actions."""
    if not isinstance(method, str) or not method or method.startswith("_") or method in {
        "init", "sudo", "with_user", "with_env", "with_context", "with_company",
    }:
        raise OdooSecurityError("Private methods and environment impersonation are unavailable.")
