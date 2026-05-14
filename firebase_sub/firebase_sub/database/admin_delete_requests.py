import logging
from collections.abc import Callable
from datetime import UTC, datetime
from typing import Any, cast

from firebase_admin import auth as firebase_auth
from google.cloud.firestore_v1 import Increment, SERVER_TIMESTAMP
from google.cloud.firestore_v1.base_document import DocumentSnapshot

_log = logging.getLogger(__name__)

# Valid state transitions in the admin delete request state machine
_VALID_TRANSITIONS = {
    "pending": {"dry_run_validated", "failed_precondition", "failed_terminal"},
    "dry_run_validated": {"auth_deleting", "auth_delete_blocked"},
    "failed_precondition": set(),
    "failed_terminal": set(),
    "auth_deleting": {"auth_deleted", "auth_delete_failed"},
    "auth_deleted": set(),
    "auth_delete_failed": set(),
    "auth_delete_blocked": set(),
}


class AdminDeleteRequestHandler:
    def __init__(
        self,
        db,
        *,
        enabled: bool,
        dry_run: bool = True,
        enable_real_auth_delete: bool = False,
        auth_deleter: Callable[[str], None] | None = None,
    ):
        self.db = db
        self.enabled = enabled
        self.dry_run = dry_run
        self.enable_real_auth_delete = enable_real_auth_delete
        self._auth_deleter = auth_deleter or firebase_auth.delete_user

    def _user_doc_exists(self, collection_name: str, uid: str) -> bool:
        snap = cast(DocumentSnapshot, self.db.collection(collection_name).document(uid).get())
        return bool(snap.exists)

    def _request_ref(self, request_id: str):
        return self.db.collection("admin_delete_requests").document(request_id)

    def _check_kill_switch(self) -> bool:
        """Check if admin delete processing is paused.
        
        Returns:
            True if processing is paused; False otherwise.
        """
        try:
            kill_switch_doc = cast(
                DocumentSnapshot,
                self.db.collection("system_config").document("admin_delete").get(),
            )
            if kill_switch_doc.exists:
                data = kill_switch_doc.to_dict() or {}
                if data.get("paused"):
                    reason = data.get("reason", "no reason specified")
                    _log.info("Admin delete processing paused: %s", reason)
                    return True
        except Exception as e:
            _log.warning("Failed to check kill-switch document: %s", e)
        return False

    def _write_audit(self, request_id: str, payload: dict[str, Any]) -> None:
        """Write immutable audit record with composite key.
        
        Uses composite key (requestId_status_timestamp_microseconds) to ensure
        each status transition creates a new immutable audit record.
        """
        status = payload.get("outcome", "unknown")
        now = datetime.now(UTC)
        # Use microseconds precision for uniqueness within same second
        timestamp_micros = int(now.timestamp() * 1_000_000)
        
        audit_record_id = f"{request_id}_{status}_{timestamp_micros}"
        audit_payload = {
            "requestId": request_id,
            "outcome": status,
            "at": now.isoformat(),
            **payload,
        }
        self.db.collection("admin_delete_request_audit").document(
            audit_record_id
        ).set(audit_payload)
        self._emit_outcome_metric(request_id=request_id, outcome=str(status), at=now)

    def _emit_outcome_metric(self, *, request_id: str, outcome: str, at: datetime) -> None:
        """Emit best-effort counters for alerting dashboards.

        Metrics are stored in Firestore to avoid introducing external telemetry
        dependencies in this phase. Writes are best-effort and never block request
        processing.
        """
        try:
            daily_key = at.date().isoformat()
            metric_update = {
                "updatedAt": SERVER_TIMESTAMP,
                "lastOutcome": outcome,
                "lastRequestId": request_id,
                "total": Increment(1),
                "outcomes": {
                    outcome: Increment(1),
                },
            }
            metrics = self.db.collection("admin_delete_request_metrics")
            metrics.document("global").set(metric_update, merge=True)
            metrics.document(f"daily-{daily_key}").set(metric_update, merge=True)
        except Exception as exc:
            _log.warning("Failed to write admin delete outcome metric: %s", exc)

    def _is_valid_transition(self, current_status: str, new_status: str) -> bool:
        """Check if a status transition is valid.
        
        Returns:
            True if transition is allowed; False otherwise.
        """
        allowed_next_states = _VALID_TRANSITIONS.get(current_status, set())
        return new_status in allowed_next_states

    def _mark_request(self, request_id: str, new_status: str, **extra: Any) -> None:
        """Update request status with transition validation.
        
        Only allows valid state transitions. Current status is read before update.
        """
        # Fetch current status
        current_doc = cast(DocumentSnapshot, self._request_ref(request_id).get())
        if not current_doc.exists:
            _log.error("Request document %s not found for status update", request_id)
            return
        
        current_data = current_doc.to_dict() or {}
        current_status = current_data.get("status", "unknown")
        
        # Validate transition
        if not self._is_valid_transition(current_status, new_status):
            _log.error(
                "Invalid state transition for request %s: %s -> %s",
                request_id,
                current_status,
                new_status,
            )
            return
        
        update = {
            "status": new_status,
            "updatedAt": SERVER_TIMESTAMP,
            **extra,
        }
        self._request_ref(request_id).set(update, merge=True)

    def handle_request_document(self, document: DocumentSnapshot | None) -> None:
        if document is None:
            raise ValueError("Admin delete request event missing document")

        if not self.enabled:
            _log.info("Admin delete request processing disabled; ignoring %s", document.id)
            return

        # Check kill-switch before processing
        if self._check_kill_switch():
            _log.info("Admin delete processing paused; skipping request %s", document.id)
            return

        data = cast(dict[str, Any] | None, document.to_dict()) or {}
        status = data.get("status")
        if status != "pending":
            _log.info(
                "Skipping admin delete request %s with non-pending status=%s",
                document.id,
                status,
            )
            return

        target_uid = data.get("targetUid")
        if not isinstance(target_uid, str) or not target_uid.strip():
            self._write_audit(
                document.id,
                {
                    "outcome": "invalid_request",
                    "reason": "missing_target_uid",
                },
            )
            self._mark_request(
                document.id,
                "failed_terminal",
                lastError="missing_target_uid",
            )
            return

        user_exists = self._user_doc_exists("users", target_uid)
        user_public_exists = self._user_doc_exists("user-public", target_uid)
        if user_exists or user_public_exists:
            self._write_audit(
                document.id,
                {
                    "outcome": "failed_precondition",
                    "reason": "user_docs_still_exist",
                    "targetUid": target_uid,
                    "usersDocExists": user_exists,
                    "userPublicDocExists": user_public_exists,
                },
            )
            self._mark_request(
                document.id,
                "failed_precondition",
                lastError="user_docs_still_exist",
                usersDocExists=user_exists,
                userPublicDocExists=user_public_exists,
            )
            return

        self._write_audit(
            document.id,
            {
                "outcome": "dry_run_validated",
                "targetUid": target_uid,
            },
        )
        self._mark_request(document.id, "dry_run_validated")

        if self.dry_run:
            return

        if not self.enable_real_auth_delete:
            self._write_audit(
                document.id,
                {
                    "outcome": "auth_delete_blocked",
                    "reason": "real_auth_delete_not_enabled",
                    "targetUid": target_uid,
                },
            )
            self._mark_request(
                document.id,
                "auth_delete_blocked",
                lastError="real_auth_delete_not_enabled",
            )
            return

        self._write_audit(
            document.id,
            {
                "outcome": "auth_deleting",
                "targetUid": target_uid,
            },
        )
        self._mark_request(document.id, "auth_deleting")

        try:
            self._auth_deleter(target_uid)
        except firebase_auth.UserNotFoundError:
            # Idempotent behavior: already missing user counts as deleted.
            self._write_audit(
                document.id,
                {
                    "outcome": "auth_deleted",
                    "targetUid": target_uid,
                    "idempotent": True,
                    "reason": "auth_user_not_found",
                },
            )
            self._mark_request(
                document.id,
                "auth_deleted",
                authDeletedAt=SERVER_TIMESTAMP,
            )
            return
        except Exception as exc:
            self._write_audit(
                document.id,
                {
                    "outcome": "auth_delete_failed",
                    "targetUid": target_uid,
                    "reason": type(exc).__name__,
                    "error": str(exc),
                },
            )
            self._mark_request(
                document.id,
                "auth_delete_failed",
                lastError=f"{type(exc).__name__}: {exc}",
            )
            return

        self._write_audit(
            document.id,
            {
                "outcome": "auth_deleted",
                "targetUid": target_uid,
            },
        )
        self._mark_request(
            document.id,
            "auth_deleted",
            authDeletedAt=SERVER_TIMESTAMP,
        )
