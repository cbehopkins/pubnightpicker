"""Integration tests for admin delete request processing.

Tests the full flow: request enqueue → kill-switch check → precondition validation → audit trail.
"""

import pytest
from firebase_admin import auth as firebase_auth
from firebase_admin import firestore
from google.cloud.firestore_v1.document import DocumentReference

from firebase_sub.database.admin_delete_requests import AdminDeleteRequestHandler


def _delete_doc_tree(doc_ref: DocumentReference) -> None:
    for subcollection in doc_ref.collections():
        for subdoc in subcollection.stream():
            _delete_doc_tree(subdoc.reference)
    doc_ref.delete()


def _clear_firestore(client) -> None:
    for collection in client.collections():
        for doc in collection.stream():
            _delete_doc_tree(doc.reference)


@pytest.mark.integration
def test_admin_delete_request_pending_to_dry_run_validated(firestore_client):
    """Test full flow: request → kill-switch → precondition → dry_run_validated."""
    # Setup: Create scrubbed user (no users/uid or user-public/uid docs)
    target_uid = "test-user-1"
    request_id = "req-1"
    
    # Create admin delete request in pending state
    firestore_client.collection("admin_delete_requests").document(request_id).set({
        "schemaVersion": 1,
        "targetUid": target_uid,
        "targetEmail": "user@example.com",
        "requestedByUid": "admin-uid",
        "reason": "testing",
        "scrubbedAppData": True,
        "status": "pending",
        "createdAt": firestore.SERVER_TIMESTAMP,
    })
    
    # Verify request is in pending state
    request_doc = firestore_client.collection("admin_delete_requests").document(request_id).get()
    assert request_doc.exists
    assert request_doc.to_dict()["status"] == "pending"
    
    # Process the request with dry-run enabled
    handler = AdminDeleteRequestHandler(firestore_client, enabled=True, dry_run=True)
    request_snapshot = firestore_client.collection("admin_delete_requests").document(request_id).get()
    handler.handle_request_document(request_snapshot)
    
    # Verify request status changed to dry_run_validated
    updated_doc = firestore_client.collection("admin_delete_requests").document(request_id).get()
    assert updated_doc.exists
    assert updated_doc.to_dict()["status"] == "dry_run_validated"
    
    # Verify audit record was created
    audit_docs = list(firestore_client.collection("admin_delete_request_audit").stream())
    assert len(audit_docs) == 1
    audit_data = audit_docs[0].to_dict()
    assert audit_data["requestId"] == request_id
    assert audit_data["outcome"] == "dry_run_validated"
    assert audit_data["targetUid"] == target_uid
    
    _clear_firestore(firestore_client)


@pytest.mark.integration
def test_admin_delete_request_unscrubbed_fails_precondition(firestore_client):
    """Test that request fails if user data not scrubbed."""
    target_uid = "test-user-2"
    request_id = "req-2"
    
    # Create user document (not scrubbed)
    firestore_client.collection("users").document(target_uid).set({"name": "Test User"})
    
    # Create admin delete request
    firestore_client.collection("admin_delete_requests").document(request_id).set({
        "schemaVersion": 1,
        "targetUid": target_uid,
        "targetEmail": "user@example.com",
        "requestedByUid": "admin-uid",
        "reason": "testing",
        "scrubbedAppData": True,
        "status": "pending",
        "createdAt": firestore.SERVER_TIMESTAMP,
    })
    
    # Process the request
    handler = AdminDeleteRequestHandler(firestore_client, enabled=True, dry_run=True)
    request_snapshot = firestore_client.collection("admin_delete_requests").document(request_id).get()
    handler.handle_request_document(request_snapshot)
    
    # Verify request status changed to failed_precondition
    updated_doc = firestore_client.collection("admin_delete_requests").document(request_id).get()
    assert updated_doc.exists
    assert updated_doc.to_dict()["status"] == "failed_precondition"
    
    # Verify audit record shows the failure reason
    audit_docs = list(firestore_client.collection("admin_delete_request_audit").stream())
    assert len(audit_docs) == 1
    audit_data = audit_docs[0].to_dict()
    assert audit_data["outcome"] == "failed_precondition"
    assert audit_data["reason"] == "user_docs_still_exist"
    
    _clear_firestore(firestore_client)


@pytest.mark.integration
def test_admin_delete_request_with_kill_switch_paused(firestore_client):
    """Test that kill-switch pause prevents processing."""
    target_uid = "test-user-3"
    request_id = "req-3"
    
    # Create kill-switch pause document
    firestore_client.collection("system_config").document("admin_delete").set({
        "paused": True,
        "reason": "testing pause",
        "pausedAt": firestore.SERVER_TIMESTAMP,
    })
    
    # Create admin delete request
    firestore_client.collection("admin_delete_requests").document(request_id).set({
        "schemaVersion": 1,
        "targetUid": target_uid,
        "targetEmail": "user@example.com",
        "requestedByUid": "admin-uid",
        "reason": "testing",
        "scrubbedAppData": True,
        "status": "pending",
        "createdAt": firestore.SERVER_TIMESTAMP,
    })
    
    # Process the request
    handler = AdminDeleteRequestHandler(firestore_client, enabled=True, dry_run=True)
    request_snapshot = firestore_client.collection("admin_delete_requests").document(request_id).get()
    handler.handle_request_document(request_snapshot)
    
    # Verify request status remained pending (processing was skipped)
    updated_doc = firestore_client.collection("admin_delete_requests").document(request_id).get()
    assert updated_doc.exists
    assert updated_doc.to_dict()["status"] == "pending"
    
    # Verify no audit record was created (processing was skipped)
    audit_docs = list(firestore_client.collection("admin_delete_request_audit").stream())
    assert len(audit_docs) == 0
    
    _clear_firestore(firestore_client)


@pytest.mark.integration
def test_admin_delete_request_missing_target_uid_fails_terminal(firestore_client):
    """Test that request with missing targetUid fails terminal."""
    request_id = "req-4"
    
    # Create admin delete request with missing targetUid
    firestore_client.collection("admin_delete_requests").document(request_id).set({
        "schemaVersion": 1,
        "targetEmail": "user@example.com",
        "requestedByUid": "admin-uid",
        "reason": "testing",
        "scrubbedAppData": True,
        "status": "pending",
        "createdAt": firestore.SERVER_TIMESTAMP,
    })
    
    # Process the request
    handler = AdminDeleteRequestHandler(firestore_client, enabled=True, dry_run=True)
    request_snapshot = firestore_client.collection("admin_delete_requests").document(request_id).get()
    handler.handle_request_document(request_snapshot)
    
    # Verify request status changed to failed_terminal
    updated_doc = firestore_client.collection("admin_delete_requests").document(request_id).get()
    assert updated_doc.exists
    assert updated_doc.to_dict()["status"] == "failed_terminal"
    
    # Verify audit record shows invalid_request
    audit_docs = list(firestore_client.collection("admin_delete_request_audit").stream())
    assert len(audit_docs) == 1
    audit_data = audit_docs[0].to_dict()
    assert audit_data["outcome"] == "invalid_request"
    assert audit_data["reason"] == "missing_target_uid"
    
    _clear_firestore(firestore_client)


@pytest.mark.integration
def test_audit_records_are_immutable_with_composite_keys(firestore_client):
    """Test that audit records use composite keys and are immutable."""
    target_uid = "test-user-5"
    request_id = "req-5"
    
    # Create admin delete request in pending state
    firestore_client.collection("admin_delete_requests").document(request_id).set({
        "schemaVersion": 1,
        "targetUid": target_uid,
        "targetEmail": "user@example.com",
        "requestedByUid": "admin-uid",
        "reason": "testing",
        "scrubbedAppData": True,
        "status": "pending",
        "createdAt": firestore.SERVER_TIMESTAMP,
    })
    
    # Process the request (should move to dry_run_validated)
    handler = AdminDeleteRequestHandler(firestore_client, enabled=True, dry_run=True)
    request_snapshot = firestore_client.collection("admin_delete_requests").document(request_id).get()
    handler.handle_request_document(request_snapshot)
    
    # Verify exactly one audit record was created with composite key format
    audit_docs = list(firestore_client.collection("admin_delete_request_audit").stream())
    assert len(audit_docs) == 1
    
    # Verify the document ID contains request ID and outcome
    doc_id = audit_docs[0].id
    assert request_id in doc_id
    assert "dry_run_validated" in doc_id
    
    # Verify the document contains the full audit data
    audit_data = audit_docs[0].to_dict()
    assert audit_data["requestId"] == request_id
    assert audit_data["outcome"] == "dry_run_validated"
    
    _clear_firestore(firestore_client)


@pytest.mark.integration
def test_admin_delete_request_real_auth_delete_success(
    firestore_client,
    firebase_test_app,
    clean_auth,
):
    target_uid = "test-user-real-delete"
    request_id = "req-real-delete"

    firebase_auth.create_user(uid=target_uid, email="real-delete@example.com", app=firebase_test_app)

    firestore_client.collection("admin_delete_requests").document(request_id).set(
        {
            "schemaVersion": 1,
            "targetUid": target_uid,
            "targetEmail": "real-delete@example.com",
            "requestedByUid": "admin-uid",
            "reason": "integration real auth delete",
            "scrubbedAppData": True,
            "status": "pending",
            "createdAt": firestore.SERVER_TIMESTAMP,
        }
    )

    handler = AdminDeleteRequestHandler(
        firestore_client,
        enabled=True,
        dry_run=False,
        enable_real_auth_delete=True,
    )
    request_snapshot = (
        firestore_client.collection("admin_delete_requests").document(request_id).get()
    )
    handler.handle_request_document(request_snapshot)

    updated_doc = firestore_client.collection("admin_delete_requests").document(request_id).get()
    assert updated_doc.exists
    assert updated_doc.to_dict()["status"] == "auth_deleted"

    with pytest.raises(firebase_auth.UserNotFoundError):
        firebase_auth.get_user(target_uid, app=firebase_test_app)

    outcomes = {
        doc.to_dict().get("outcome")
        for doc in firestore_client.collection("admin_delete_request_audit").stream()
    }
    assert "dry_run_validated" in outcomes
    assert "auth_deleting" in outcomes
    assert "auth_deleted" in outcomes
