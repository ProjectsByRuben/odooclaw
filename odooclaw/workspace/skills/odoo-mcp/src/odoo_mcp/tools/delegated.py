from odoo_mcp.security.guards import guard_model_access, guard_action_name, guard_unlink
from odoo_mcp.security.audit import audit_action


def unlink_records(client, user_id, model, ids):
    guard_model_access(model, client, sender_id=user_id)
    guard_unlink(model)
    if not ids or any(type(record_id) is not int or record_id <= 0 for record_id in ids):
        raise ValueError("Provide explicit positive record IDs to delete.")
    audit_action("UNLINK", user_id, model, ids, {})
    return client.call_kw(model, "unlink", args=[ids], sender_id=user_id)


def execute_method(client, user_id, model, method, args=None, kwargs=None):
    guard_model_access(model, client, sender_id=user_id)
    guard_action_name(method)
    audit_action("EXECUTE", user_id, model, [], {"method": method})
    return client.call_kw(model, method, args=args or [], kwargs=kwargs or {}, sender_id=user_id)


def model_permissions(client, user_id, model):
    guard_model_access(model, client, sender_id=user_id)
    return {operation: client.call_kw(model, "check_access_rights", args=[operation],
            kwargs={"raise_exception": False}, sender_id=user_id)
            for operation in ("read", "create", "write", "unlink")}
