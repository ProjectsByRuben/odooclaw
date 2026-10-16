import logging
import os
from typing import Any, Dict, List, Optional
import requests
from .session import OdooSession
from .exceptions import OdooRPCError

from contextvars import ContextVar
request_capability = ContextVar("odooclaw_request_capability", default=None)

_logger = logging.getLogger(__name__)


class OdooClient:
    """Core client to execute Odoo RPC methods."""

    def __init__(self, session: OdooSession):
        self.odoo_session = session

    def _ensure_authenticated(self) -> None:
        if not self.odoo_session.is_authenticated():
            self.odoo_session.authenticate()

    def call_kw(
        self,
        model: str,
        method: str,
        args: Optional[List[Any]] = None,
        kwargs: Optional[Dict[str, Any]] = None,
        sender_id: Optional[int] = None,
    ) -> Any:
        """
        Executes a method on an Odoo model.
        If sender_id is provided, it attempts to route through the secure endpoint /odooclaw/call_kw_as_user
        to enforce Odoo's native Record Rules and Access Rights using the delegation mechanism.
        """
        self._ensure_authenticated()
        args = args or []
        kwargs = dict(kwargs or {})

        # Never fall back to the technical account for business operations.
        if (type(sender_id) is not int or sender_id <= 1
                or sender_id == self.odoo_session.get_uid()):
            raise OdooRPCError("Permission denied: a valid authenticated Odoo user is required.")
        return self._call_kw_as_user(sender_id, model, method, args, kwargs)

    def _call_kw_as_user(
        self,
        user_id: int,
        model: str,
        method: str,
        args: List[Any],
        kwargs: Dict[str, Any],
    ) -> Any:
        """Delegated execution leveraging Odoo's native security via mail_bot_odooclaw endpoint."""
        endpoint = f"{self.odoo_session.url}/odooclaw/call_kw_as_user"

        # Merge session context into kwargs (thread-safe read)
        context = kwargs.pop("context", {})
        # The Odoo controller supplies the delegated user defaults.
        merged_context = dict(context)

        payload = {
            "user_id": user_id,
            "request_token": request_capability.get(),
            "model": model,
            "method": method,
            "args": args,
            "kwargs": kwargs,
            "context": merged_context,
        }

        _logger.debug(
            f"Calling endpoint {endpoint} impersonating User {user_id} on {model}.{method}"
        )
        return self._do_post(endpoint, payload)

    def _do_post(self, endpoint: str, payload: dict) -> Any:
        try:
            headers = {}
            token = os.environ.get("ODOOCLAW_REPLY_TOKEN", "")
            if token and endpoint == f"{self.odoo_session.url}/odooclaw/call_kw_as_user":
                headers["X-OdooClaw-Token"] = token
            # Administration and deletion can rebuild access/group caches and
            # take substantially longer than ordinary reads on large databases.
            long_operation = payload.get("method") == "unlink" or payload.get("model") in {"res.users", "res.groups", "ir.module.module"}
            response = self.odoo_session.session.post(
                endpoint, json=payload, headers=headers, timeout=(10, 300 if long_operation else 60)
            )
            if response.status_code == 403:
                raise OdooRPCError(
                    "Permission denied: the active Odoo user is not allowed to perform this operation."
                )
            if response.status_code == 422:
                raise OdooRPCError("Odoo: " + response.json().get("reason", "Operation rejected by Odoo."))
            if response.status_code >= 400:
                raise OdooRPCError(f"Odoo rejected the operation (HTTP {response.status_code}); no success was confirmed.")
            response.raise_for_status()
            result = response.json()

            # Check for generic server errors (e.g. from call_kw_as_user controller)
            if result.get("status") == "error":
                raise OdooRPCError(f"Delegated RPC Error: {result.get('reason')}")

            # Check for JSON-RPC specific errors
            if "error" in result:
                err_data = result["error"].get("data", {})
                err_msg = err_data.get("message", "Unknown error")
                err_debug = err_data.get("debug", "")
                raise OdooRPCError(f"RPC Error: {err_msg}\n{err_debug}")

            if "result" in result:
                # call_kw_as_user wraps result in {"status": "ok", "result": ...}
                if (
                    isinstance(result["result"], dict)
                    and result["result"].get("status") == "ok"
                ):
                    return result["result"].get("result")
                return result["result"]

            return True

        except requests.RequestException as e:
            raise OdooRPCError(f"Network error during RPC call: {str(e)}. The operation result is unknown; verify record state before retrying a mutation.")

    def try_call_kw(
        self,
        model: str,
        method: str,
        args: Optional[List[Any]] = None,
        kwargs: Optional[Dict[str, Any]] = None,
        sender_id: Optional[int] = None,
        default: Any = None,
    ) -> Any:
        try:
            return self.call_kw(
                model, method, args=args, kwargs=kwargs, sender_id=sender_id
            )
        except OdooRPCError:
            return default

    def get_model_fields(
        self, model: str, sender_id: Optional[int] = None
    ) -> Dict[str, Any]:
        return self.call_kw(model, "fields_get", sender_id=sender_id)

    def try_get_model_fields(
        self, model: str, sender_id: Optional[int] = None
    ) -> Optional[Dict[str, Any]]:
        return self.try_call_kw(model, "fields_get", sender_id=sender_id, default=None)

    def model_exists(self, model: str, sender_id: Optional[int] = None) -> bool:
        return self.try_get_model_fields(model, sender_id=sender_id) is not None

    def field_exists(
        self, model: str, field_name: str, sender_id: Optional[int] = None
    ) -> bool:
        fields = self.try_get_model_fields(model, sender_id=sender_id)
        return bool(fields and field_name in fields)
