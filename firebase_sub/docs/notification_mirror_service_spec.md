# Notification Mirror Service Specification

## 1. Purpose

This document specifies notification mirror behaviour in three parts:
- housekeeping cleanup task: delete_notification_diagnostics
- housekeeping cleanup task: delete_notification_docs_for_past_polls
- runtime listener and mirror processing path in notification_request and notification_mirror

The goal is reimplementation-ready behaviour, not code-level parity.

## 2. Data model and collections

Primary collections:
- notification_req
- notification_ack

Special document IDs:
- diagnostics
- push_test

Related source collections:
- polls (used only by delete_notification_docs_for_past_polls to decide stale poll IDs)

## 3. Housekeeping task: delete_notification_diagnostics

Function:
- delete_notification_diagnostics(db)

Behaviour:
1. Delete notification_req/diagnostics.
2. Delete notification_ack/diagnostics.

Notes:
- This is unconditional delete-by-id.
- Re-running is idempotent in effect.

Operational intent:
- Clears manual health-check diagnostic documents from both request and ack stores.

Evidence:
- implementation in firebase_sub/database/housekeeping_tasks.py
- tested by tests/test_housekeeping_tasks.py function test_delete_notification_diagnostics_deletes_req_and_ack_docs

## 4. Housekeeping task: delete_notification_docs_for_past_polls

Function:
- delete_notification_docs_for_past_polls(db, today=None)

Behaviour:
1. Determine cutoff date string:
   - cutoff = (today if provided else date.today()).isoformat()
2. Query polls with date strictly before cutoff.
3. For each returned poll ID:
   - delete notification_req/{pollId}
   - delete notification_ack/{pollId}

Date predicate detail:
- poll.date < cutoff (strictly earlier than the current day)
- same-day poll IDs are not deleted by this task

Notes:
- Only request/ack docs keyed by poll ID are targeted.
- No explicit try/except in this function; failures bubble up to housekeeping runner.

Evidence:
- implementation in firebase_sub/database/housekeeping_tasks.py and firebase_sub/database/housekeeping_store.py poll_ids_before_date
- tested by:
  - test_delete_notification_docs_for_past_polls_deletes_req_and_ack_for_each_poll
  - test_delete_notification_docs_for_past_polls_no_past_polls_no_deletes

## 5. Mirror handler logic: NotificationAckMirrorHandler

Class:
- NotificationAckMirrorHandler

Default configuration:
- request collection name: notification_req
- ack collection name: notification_ack

Core operation:
- mirror_request_document(request_document)

Algorithm:
1. Validate input document is not None.
2. Read request payload; treat missing payload as empty map.
3. Read ack document with the same document ID from notification_ack; treat missing payload as empty map.
4. Build patch containing only keys where:
   - key not present in ack payload, or
   - value differs from ack payload.
5. If patch is empty:
   - no write (no-op).
6. If patch is non-empty:
   - set patch into ack doc with merge=true.

Semantics:
- Existing ack keys not present in request are not deleted.
- Only additive/changed fields are mirrored.
- Merge write preserves unrelated fields in ack docs.

Error handling:
- Any exception is wrapped and raised as RetryableServiceError.
- This marks mirror failures as retryable at runtime orchestration level.

Compatibility method:
- handle(request_document, pubs_list) delegates to mirror_request_document and ignores pubs_list.

Evidence:
- implementation in firebase_sub/database/notification_mirror.py
- tested by tests/test_notification_mirror.py

## 6. Notification listener: NotificationRequestListenerPlugin

Class:
- NotificationRequestListenerPlugin

What it listens for:
1. Source watch
   - Firestore query on notification_req collection.
   - Change types ADDED and MODIFIED (via PollManager add and modify callbacks).
2. Event routing
   - Registered for two event types in registry:
     - PUSH
     - PUSH_TEST

How event type is chosen from watched documents:
- event producer inspects each changed notification_req document.
- if document is recognised as push test request (doc ID push_test), event type is PUSH_TEST.
- otherwise event type is PUSH.

Plugin dispatch behaviour:
- PUSH_TEST events are handled by NotificationPushTestHandler.
- PUSH events are handled by NotificationAckMirrorHandler.
- non-PUSH and non-PUSH_TEST event types are rejected by prepare/filter logic.

Idempotency model:
- Metadata is stored in notification_ack/{docId} under root field _service_idempotency.
- Dedupe key format:
  - notification:{eventType}:{docId}:{payloadHash16}
- payload hash is SHA-256 of canonical JSON payload (sorted keys), truncated to 16 hex chars.

Idempotency states per dedupe key:
- done
- retryable_failed
- terminal_failed

Attempt tracking fields:
- attempt_count
- last_attempted_at
- last_error_code
- last_error_message

Execution and commit semantics:
1. execute:
   - skips if dedupe key already done
   - routes to push-test handler or mirror handler
   - records retryable or terminal failure metadata on exceptions
2. commit:
   - marks dedupe key done in _service_idempotency
   - request/ack business state is already persisted inline by the underlying handlers

Evidence:
- implementation in firebase_sub/plugins/notification_request.py
- producer wiring in firebase_sub/runtime/event_producers.py
- query source in firebase_sub/database/handlers.py
- registry subscriptions in firebase_sub/plugins/plugin_config.py
- tests in tests/test_notification_request_plugin.py

## 7. Relationship between housekeeping and listener

- Listener path keeps notification_ack aligned with incoming notification_req documents at runtime.
- Housekeeping tasks remove old or diagnostic request/ack documents so these stores do not accumulate stale entries.
- The two behaviours are complementary:
  - listener is reactive on writes/updates
  - housekeeping is periodic cleanup

## 8. Reimplementation checklist

A conforming implementation should:
1. Watch notification_req for added and modified documents.
2. Classify push_test versus normal request documents and route accordingly.
3. Mirror only changed/missing keys from request to ack using merge semantics.
4. Maintain per-dedupe idempotency metadata in notification_ack.
5. Provide periodic cleanup tasks that:
   - delete diagnostics docs from both notification_req and notification_ack
   - delete per-poll request/ack docs for polls with date strictly before today
