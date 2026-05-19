from unittest.mock import ANY, MagicMock, patch

from firebase_sub.database.admin_delete_requests import AdminDeleteRequestHandler


def _snapshot(doc_id: str, payload: dict | None) -> MagicMock:
    snapshot = MagicMock()
    snapshot.id = doc_id
    snapshot.to_dict.return_value = payload
    return snapshot


def _collection_side_effect(
    *,
    users_exists: bool,
    user_public_exists: bool,
    kill_switch_paused: bool = False,
    request_doc_status: str | None = None,
):
    """Factory for db.collection side effects.

    Handles:
    - users collection
    - user-public collection
    - system_config collection (for kill-switch)
    - admin_delete_requests collection (for reading current status)
    """
    users_doc = MagicMock()
    users_doc.exists = users_exists
    user_public_doc = MagicMock()
    user_public_doc.exists = user_public_exists

    # Kill-switch doc
    kill_switch_doc = MagicMock()
    kill_switch_doc.exists = kill_switch_paused
    kill_switch_doc.to_dict.return_value = (
        {"paused": True, "reason": "testing"} if kill_switch_paused else {}
    )

    # Request doc (for reading current status)
    request_doc = MagicMock()
    request_doc.exists = request_doc_status is not None
    request_doc.to_dict.return_value = (
        {"status": request_doc_status} if request_doc_status else {}
    )

    def inner(name: str):
        coll = MagicMock()
        if name == "users":
            coll.document.return_value.get.return_value = users_doc
        elif name == "user-public":
            coll.document.return_value.get.return_value = user_public_doc
        elif name == "system_config":
            coll.document.return_value.get.return_value = kill_switch_doc
        elif name == "admin_delete_requests":
            coll.document.return_value.get.return_value = request_doc
        return coll

    return inner


def test_disabled_handler_ignores_requests() -> None:
    db = MagicMock()
    handler = AdminDeleteRequestHandler(db, enabled=False, dry_run=True)

    handler.handle_request_document(
        _snapshot("req-1", {"status": "pending", "targetUid": "u1"})
    )

    db.collection.assert_not_called()


def test_handle_delegates_to_document_handler() -> None:
    db = MagicMock()
    handler = AdminDeleteRequestHandler(db, enabled=True, dry_run=True)
    document = MagicMock()
    pubs_list = MagicMock()

    with patch.object(handler, "handle_request_document") as mock_handle_request_document:
        handler.handle(document, pubs_list)

    mock_handle_request_document.assert_called_once_with(document)


def test_handle_passes_through_none_document() -> None:
    db = MagicMock()
    handler = AdminDeleteRequestHandler(db, enabled=True, dry_run=True)
    pubs_list = MagicMock()

    with patch.object(handler, "handle_request_document") as mock_handle_request_document:
        handler.handle(None, pubs_list)

    mock_handle_request_document.assert_called_once_with(None)


def test_kill_switch_paused_skips_processing() -> None:
    db = MagicMock()
    db.collection.side_effect = _collection_side_effect(
        users_exists=False,
        user_public_exists=False,
        kill_switch_paused=True,
    )
    handler = AdminDeleteRequestHandler(db, enabled=True, dry_run=True)

    handler.handle_request_document(
        _snapshot("req-1", {"status": "pending", "targetUid": "u1"})
    )

    # Only system_config collection should be queried (for kill-switch)
    # Request should be skipped without processing
    db.collection.assert_any_call("system_config")
    # admin_delete_request_audit should NOT be called
    call_names = [call[0][0] for call in db.collection.call_args_list]
    assert "admin_delete_request_audit" not in call_names


def test_pending_request_missing_target_uid_fails_terminal() -> None:
    db = MagicMock()
    db.collection.side_effect = _collection_side_effect(
        users_exists=False,
        user_public_exists=False,
        request_doc_status="pending",
    )
    handler = AdminDeleteRequestHandler(db, enabled=True, dry_run=True)

    handler.handle_request_document(_snapshot("req-1", {"status": "pending"}))

    db.collection.assert_any_call("admin_delete_request_audit")
    db.collection.assert_any_call("admin_delete_requests")


def test_pending_request_with_unscrubbed_user_fails_precondition() -> None:
    db = MagicMock()
    db.collection.side_effect = _collection_side_effect(
        users_exists=True,
        user_public_exists=False,
        request_doc_status="pending",
    )
    handler = AdminDeleteRequestHandler(db, enabled=True, dry_run=True)

    handler.handle_request_document(
        _snapshot("req-2", {"status": "pending", "targetUid": "u2"})
    )

    db.collection.assert_any_call("admin_delete_request_audit")
    db.collection.assert_any_call("admin_delete_requests")


def test_pending_request_scrubbed_moves_to_dry_run_validated() -> None:
    db = MagicMock()
    db.collection.side_effect = _collection_side_effect(
        users_exists=False,
        user_public_exists=False,
        request_doc_status="pending",
    )
    handler = AdminDeleteRequestHandler(db, enabled=True, dry_run=True)

    handler.handle_request_document(
        _snapshot("req-3", {"status": "pending", "targetUid": "u3"})
    )

    db.collection.assert_any_call("admin_delete_request_audit")
    db.collection.assert_any_call("admin_delete_requests")


def test_valid_state_transition_pending_to_dry_run_validated() -> None:
    """Test that pending -> dry_run_validated is a valid transition."""
    db = MagicMock()
    handler = AdminDeleteRequestHandler(db, enabled=True)

    assert handler._is_valid_transition("pending", "dry_run_validated") is True


def test_valid_state_transition_pending_to_failed_precondition() -> None:
    """Test that pending -> failed_precondition is a valid transition."""
    db = MagicMock()
    handler = AdminDeleteRequestHandler(db, enabled=True)

    assert handler._is_valid_transition("pending", "failed_precondition") is True


def test_valid_state_transition_pending_to_failed_terminal() -> None:
    """Test that pending -> failed_terminal is a valid transition."""
    db = MagicMock()
    handler = AdminDeleteRequestHandler(db, enabled=True)

    assert handler._is_valid_transition("pending", "failed_terminal") is True


def test_invalid_state_transition_pending_to_pending() -> None:
    """Test that pending -> pending is invalid."""
    db = MagicMock()
    handler = AdminDeleteRequestHandler(db, enabled=True)

    assert handler._is_valid_transition("pending", "pending") is False


def test_invalid_state_transition_failed_precondition_to_dry_run_validated() -> None:
    """Test that failed_precondition -> dry_run_validated is invalid."""
    db = MagicMock()
    handler = AdminDeleteRequestHandler(db, enabled=True)

    assert (
        handler._is_valid_transition("failed_precondition", "dry_run_validated")
        is False
    )


def test_audit_writes_with_composite_key() -> None:
    """Test that audit records use composite keys for immutability."""
    db = MagicMock()
    handler = AdminDeleteRequestHandler(db, enabled=True)

    handler._write_audit("req-1", {"outcome": "dry_run_validated", "targetUid": "u1"})

    # Verify audit + metric collections were touched.
    call_names = [call[0][0] for call in db.collection.call_args_list]
    assert "admin_delete_request_audit" in call_names
    assert "admin_delete_request_metrics" in call_names


def test_non_dry_run_without_cli_gate_blocks_auth_delete() -> None:
    db = MagicMock()
    auth_deleter = MagicMock()
    handler = AdminDeleteRequestHandler(
        db,
        enabled=True,
        dry_run=False,
        enable_real_auth_delete=False,
        auth_deleter=auth_deleter,
    )
    with (
        patch.object(handler, "_check_kill_switch", return_value=False),
        patch.object(handler, "_user_doc_exists", return_value=False),
        patch.object(handler, "_write_audit") as mock_write_audit,
        patch.object(handler, "_mark_request") as mock_mark_request,
    ):
        handler.handle_request_document(
            _snapshot("req-10", {"status": "pending", "targetUid": "u10"})
        )

    auth_deleter.assert_not_called()
    mock_write_audit.assert_any_call(
        "req-10",
        {
            "outcome": "auth_delete_blocked",
            "reason": "real_auth_delete_not_enabled",
            "targetUid": "u10",
        },
    )
    mock_mark_request.assert_any_call(
        "req-10",
        "auth_delete_blocked",
        lastError="real_auth_delete_not_enabled",
    )


def test_non_dry_run_with_cli_gate_deletes_auth_user() -> None:
    db = MagicMock()
    auth_deleter = MagicMock()
    handler = AdminDeleteRequestHandler(
        db,
        enabled=True,
        dry_run=False,
        enable_real_auth_delete=True,
        auth_deleter=auth_deleter,
    )
    with (
        patch.object(handler, "_check_kill_switch", return_value=False),
        patch.object(handler, "_user_doc_exists", return_value=False),
        patch.object(handler, "_write_audit") as mock_write_audit,
        patch.object(handler, "_mark_request") as mock_mark_request,
    ):
        handler.handle_request_document(
            _snapshot("req-11", {"status": "pending", "targetUid": "u11"})
        )

    auth_deleter.assert_called_once_with("u11")
    mock_write_audit.assert_any_call(
        "req-11",
        {
            "outcome": "auth_deleted",
            "targetUid": "u11",
        },
    )
    mock_mark_request.assert_any_call(
        "req-11",
        "auth_deleted",
        authDeletedAt=ANY,
    )


def test_non_dry_run_auth_delete_failure_marks_failed() -> None:
    db = MagicMock()

    def _raise_error(uid: str) -> None:
        raise RuntimeError(f"boom:{uid}")

    handler = AdminDeleteRequestHandler(
        db,
        enabled=True,
        dry_run=False,
        enable_real_auth_delete=True,
        auth_deleter=_raise_error,
    )
    with (
        patch.object(handler, "_check_kill_switch", return_value=False),
        patch.object(handler, "_user_doc_exists", return_value=False),
        patch.object(handler, "_write_audit") as mock_write_audit,
        patch.object(handler, "_mark_request") as mock_mark_request,
    ):
        handler.handle_request_document(
            _snapshot("req-12", {"status": "pending", "targetUid": "u12"})
        )

    mock_write_audit.assert_any_call(
        "req-12",
        {
            "outcome": "auth_delete_failed",
            "targetUid": "u12",
            "reason": "RuntimeError",
            "error": "boom:u12",
        },
    )
    mock_mark_request.assert_any_call(
        "req-12",
        "auth_delete_failed",
        lastError="RuntimeError: boom:u12",
    )
