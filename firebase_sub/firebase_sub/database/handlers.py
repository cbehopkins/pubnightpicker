import logging
from collections.abc import Callable, Generator, Mapping, Sequence
from datetime import datetime
from functools import partial
from typing import Any, cast

from firebase_admin import firestore
from google.cloud.firestore_v1.base_query import FieldFilter
from google.cloud.firestore_v1 import watch
from google.cloud.firestore_v1.base_document import DocumentSnapshot
from google.cloud.firestore_v1.client import Client
from google.cloud.firestore_v1.collection import CollectionReference
from google.cloud.firestore_v1.query import Query
from google.cloud.firestore_v1.watch import DocumentChange

from firebase_sub.action_track import ActionMan
from firebase_sub.push_contract import (
    PUSH_PREFERENCE_DEFAULTS,
    PUSH_PREFERENCE_FIELD,
    PUSH_EVENT_CHAT_MESSAGE_GLOBAL,
    PUSH_EVENT_CHAT_MESSAGE_EVENT,
    PushDedupeKeys,
)
from firebase_sub.send_push import (
    ValidPushEndpoint,
    build_chat_event_payload,
    build_chat_global_payload,
    endpoint_hash,
    push_endpoint_from_snapshot,
    send_chat_push,
)
from firebase_sub.database.repositories import (
    FirestorePollRepository,
    FirestoreUserRepository,
)
from firebase_sub.my_types import EmailAddr, PollDocument, PollId, UserId
from firebase_sub.database.pubs_list import PubsList

_log = logging.getLogger(__name__)


class RetryablePollDataNotReadyError(RuntimeError):
    """Raised when event handling should retry because dependent data is not ready."""


def _payload_dict(payload: object) -> dict[str, object]:
    if not isinstance(payload, Mapping):
        return {}

    normalized: dict[str, object] = {}
    payload_map = cast(Mapping[object, object], payload)
    for key, value in payload_map.items():
        if isinstance(key, str):
            normalized[key] = value
            continue
        normalized[str(key)] = value
    return normalized


def _snapshot_payload(document: DocumentSnapshot) -> dict[str, object]:
    return _payload_dict(document.to_dict())


def _string_list(value: object) -> list[str]:
    if not isinstance(value, list | tuple | set):
        return []

    result: list[str] = []
    iterable_value = cast(Sequence[object] | set[object], value)
    for entry in iterable_value:
        if isinstance(entry, str):
            result.append(entry)
    return result


def _snapshot_get(document_ref: object) -> DocumentSnapshot:
    raw_snapshot = cast(Any, document_ref).get()
    if isinstance(raw_snapshot, DocumentSnapshot):
        return raw_snapshot
    if raw_snapshot is not None and hasattr(raw_snapshot, "to_dict"):
        # Accept snapshot-like test doubles that expose Firestore-compatible surface.
        return cast(DocumentSnapshot, raw_snapshot)
    raise TypeError("Expected synchronous DocumentSnapshot from Firestore get()")


def _doc_set(
    document_ref: object, payload: dict[str, object], *, merge: bool = False
) -> None:
    cast(Any, document_ref).set(payload, merge=merge)


def _query_where(query: Query, *, field: str, op: str, value: object) -> Query:
    query_any: Any = query
    return cast(Query, query_any.where(filter=FieldFilter(field, op, value)))


def _endpoint_parent_user_id(document: DocumentSnapshot) -> str | None:
    # Expected path: users/<uid>/push_endpoints/<endpoint_id>
    path_parts = document.reference.path.split("/")
    if len(path_parts) < 4:
        return None
    if path_parts[-2] != "push_endpoints":
        return None
    user_id = path_parts[-3]
    return user_id if user_id else None


def _compute_action_key(poll_id: PollId, poll_dict: PollDocument, pub_id: str) -> str:
    """Build the canonical completion action key for email/push dedupe."""
    _ = poll_id
    return PushDedupeKeys.complete_key(
        pub_id=pub_id,
        restaurant_id=poll_dict.get("restaurant"),
        restaurant_time=poll_dict.get("restaurant_time"),
    )


def _with_legacy_alias_key(
    action_dict: dict[str, object] | None,
    legacy_key: str,
    canonical_key: str,
) -> dict[str, object] | None:
    """Add canonical key when legacy key exists to avoid replay resend after migration."""
    if action_dict is None or legacy_key == canonical_key:
        return action_dict

    normalized: dict[str, object] = dict(action_dict)
    for action_type, values in list(normalized.items()):
        if not isinstance(values, list | tuple | set):
            continue
        key_set: set[str] = set()
        for entry in cast(Sequence[object] | set[object], values):
            if isinstance(entry, str):
                key_set.add(entry)
        if legacy_key in key_set and canonical_key not in key_set:
            key_set.add(canonical_key)
            normalized[action_type] = list(key_set)
    return normalized


class DbHandler:
    def __init__(self):
        self.db: Client = firestore.client()
        # patch_watch_close(self.my_watch_close_callback)
        self.okay = True
        self.poll_repo = FirestorePollRepository(self.db)
        self.user_repo = FirestoreUserRepository(self.db)

    def my_watch_close_callback(self, reason: object | None) -> None:
        # This is no longer called as we often close a watch
        # At the moment we restart the watch regularly
        # to make sure we keep a live connection
        _log.error(f"Firestore Watch closed! Reason: {reason}")
        self.okay = False
        # This happens in a different thread - so we are blocked unable to exit
        # https://github.com/googleapis/python-firestore/issues/882
        raise SystemExit("Exiting due to watch close.")

    @property
    def pub_collection(self) -> CollectionReference:
        return self.db.collection("pubs")

    def query_personal_emails(self) -> Generator[tuple[EmailAddr, UserId], None, None]:
        """Query users who want personal email notifications (via personal email)."""
        yield from self.user_repo.query_users_by_email_preference(
            "notificationEmailEnabled"
        )

    def query_open_emails(self) -> Generator[tuple[EmailAddr, UserId], None, None]:
        """Query users who want poll-open notifications."""
        yield from self.user_repo.query_users_by_email_preference(
            "openPollEmailEnabled"
        )

    def query_active_push_endpoints(
        self, preference_field: str
    ) -> Generator[DocumentSnapshot, None, None]:
        """Query active web push endpoints across users with web push enabled.

        Filters by both the webPushEnabled master switch and the per-type
        pushPreferences field identified by ``preference_field`` (e.g.
        "pollOpens", "pollCompletes", "globalChat", "eventChat").
        """
        collection_group_any: Any = self.db.collection_group("push_endpoints")
        query = cast(
            Query,
            collection_group_any.where(filter=FieldFilter("active", "==", True)),
        )
        user_preference_cache: dict[str, bool] = {}
        for endpoint_doc in query.stream():
            user_id = _endpoint_parent_user_id(endpoint_doc)
            if user_id is None:
                continue
            if user_id not in user_preference_cache:
                user_snapshot = _snapshot_get(
                    self.db.collection("users").document(user_id)
                )
                user_payload = _snapshot_payload(user_snapshot)
                if "webPushEnabled" not in user_payload:
                    # Temporary rollout observability: missing preference now defaults off.
                    _log.warning(
                        "Skipping push endpoints for user %s because webPushEnabled is missing",
                        user_id,
                    )
                    user_preference_cache[user_id] = False
                elif not bool(user_payload.get("webPushEnabled")):
                    # Temporary rollout observability: explicit opt-out.
                    _log.info(
                        "Skipping push endpoints for user %s because webPushEnabled is false",
                        user_id,
                    )
                    user_preference_cache[user_id] = False
                else:
                    push_prefs = _payload_dict(user_payload.get("pushPreferences"))
                    default = PUSH_PREFERENCE_DEFAULTS.get(preference_field, True)
                    user_preference_cache[user_id] = bool(
                        push_prefs.get(preference_field, default)
                    )
                    if not user_preference_cache[user_id]:
                        _log.info(
                            "Skipping push endpoints for user %s because pushPreferences.%s is false",
                            user_id,
                            preference_field,
                        )
            if user_preference_cache[user_id]:
                yield endpoint_doc

    def query_active_push_endpoints_for_user(
        self, user_id: str
    ) -> Generator[DocumentSnapshot, None, None]:
        """Query active web push endpoints for one user when web push is enabled."""
        if not user_id:
            return
        user_snapshot = _snapshot_get(self.db.collection("users").document(user_id))
        user_payload = _snapshot_payload(user_snapshot)
        if "webPushEnabled" not in user_payload:
            _log.warning(
                "Skipping push endpoints for user %s because webPushEnabled is missing",
                user_id,
            )
            return
        if not bool(user_payload.get("webPushEnabled")):
            _log.info(
                "Skipping push endpoints for user %s because webPushEnabled is false",
                user_id,
            )
            return

        user_document_ref = self.db.collection("users").document(user_id)
        endpoint_collection = cast(Any, user_document_ref.collection("push_endpoints"))
        # TODO(revisit-indexed-query): switch back to .where(active == True)
        # once COLLECTION-scope index rollout for push_endpoints.active is
        # confirmed in all deployed projects.
        for endpoint_doc in endpoint_collection.stream():
            payload = _snapshot_payload(endpoint_doc)
            if not bool(payload.get("active")):
                continue
            yield endpoint_doc

    def _users_with_push_preference(self, preference_field: str) -> set[str]:
        """Return uids of users with webPushEnabled=True and the given pushPreferences field True."""
        uids: set[str] = set()
        users_collection: CollectionReference = self.db.collection("users")
        for user_doc in users_collection.stream():
            user_payload = _snapshot_payload(user_doc)
            if not bool(user_payload.get("webPushEnabled")):
                continue
            push_prefs = _payload_dict(user_payload.get("pushPreferences"))
            default = PUSH_PREFERENCE_DEFAULTS.get(preference_field, True)
            if bool(push_prefs.get(preference_field, default)):
                uids.add(user_doc.id)
        return uids

    def _attendee_uids(self, poll_id: str) -> set[str]:
        """Return uids of users attending the given poll (in any canCome array)."""
        attendance_doc = _snapshot_get(
            self.db.collection("attendance").document(poll_id)
        )
        attendance_data = _snapshot_payload(attendance_doc)
        uids: set[str] = set()
        for venue_data in attendance_data.values():
            if not isinstance(venue_data, Mapping):
                continue
            can_come = _string_list(
                cast(Mapping[str, object], venue_data).get("canCome")
            )
            uids.update(can_come)
        return uids

    def _event_chat_participant_uids(self, poll_id: str) -> set[str]:
        """Return uids that have already authored messages in the event chat."""
        if not poll_id:
            return set()

        query = cast(Query, self.db.collection("messages"))
        query = _query_where(query, field="scopeType", op="==", value="event")
        query = _query_where(query, field="scopeId", op="==", value=poll_id)

        participants: set[str] = set()
        for message_doc in query.stream():
            message_data = _snapshot_payload(message_doc)
            uid = message_data.get("uid")
            if isinstance(uid, str) and uid:
                participants.add(uid)
        return participants

    def _muted_event_chat_uids(
        self, poll_id: str, candidate_uids: set[str]
    ) -> set[str]:
        """Return candidate uids that muted notifications for a specific event."""
        if not poll_id or not candidate_uids:
            return set()

        muted: set[str] = set()
        for uid in candidate_uids:
            user_snapshot = _snapshot_get(self.db.collection("users").document(uid))
            user_payload = _snapshot_payload(user_snapshot)
            push_prefs = _payload_dict(user_payload.get("pushPreferences"))
            muted_poll_ids = _string_list(push_prefs.get("eventChatMutedPollIds"))
            if poll_id in muted_poll_ids:
                muted.add(uid)
        return muted

    def chat_message_push_handler(
        self,
        message_id: str,
        message_doc: DocumentSnapshot,
        *,
        dummy_run: bool = False,
    ) -> None:
        """Handle a new chat message and send push notifications to eligible users."""
        message_data = _snapshot_payload(message_doc)
        scope_type_raw = message_data.get("scopeType")
        if scope_type_raw != "event":
            scope_type = "global"
            scope_id = "main"
        else:
            scope_type = "event"
            scope_id_raw = message_data.get("scopeId")
            scope_id = (
                scope_id_raw
                if isinstance(scope_id_raw, str) and scope_id_raw
                else "main"
            )

        author_uid_raw = message_data.get("uid")
        author_uid = author_uid_raw if isinstance(author_uid_raw, str) else ""
        display_name = message_data.get("displayName")
        fallback_name = message_data.get("name")
        sender_name = (
            display_name
            if isinstance(display_name, str) and display_name
            else (
                fallback_name
                if isinstance(fallback_name, str) and fallback_name
                else "Someone"
            )
        )
        text_value = message_data.get("text")
        alt_text_value = message_data.get("message")
        raw_text = (
            text_value
            if isinstance(text_value, str)
            else alt_text_value if isinstance(alt_text_value, str) else ""
        )
        body_text = raw_text[:100]

        preference_field = PUSH_PREFERENCE_FIELD[
            (
                PUSH_EVENT_CHAT_MESSAGE_EVENT
                if scope_type == "event"
                else PUSH_EVENT_CHAT_MESSAGE_GLOBAL
            )
        ]
        eligible_uids = self._users_with_push_preference(preference_field)

        if scope_type == "event":
            attendees = self._attendee_uids(scope_id)
            participants = self._event_chat_participant_uids(scope_id)
            eligible_event_uids = attendees | participants
            eligible_uids &= eligible_event_uids
            muted_uids = self._muted_event_chat_uids(scope_id, eligible_uids)
            eligible_uids -= muted_uids

        # Exclude the message author.
        eligible_uids.discard(author_uid)

        if not eligible_uids:
            _log.info("No eligible recipients for chat push on message %s", message_id)
            return

        # Read per-endpoint delivery record for idempotency: endpoint hashes allow
        # partial retries without re-delivering to already-succeeded endpoints.
        actions_ref = self.db.collection("chat_push_actions").document(message_id)
        actions_snap = _snapshot_get(actions_ref)
        actions_data = _snapshot_payload(actions_snap)
        already_delivered_eps = set(
            _string_list(actions_data.get("delivered_endpoints"))
        )

        # Build payload.
        if scope_type == "event":
            payload = build_chat_event_payload(
                message_id=message_id,
                poll_id=scope_id,
                sender_name=sender_name,
                body_text=body_text,
            )
        else:
            payload = build_chat_global_payload(
                message_id=message_id,
                sender_name=sender_name,
                body_text=body_text,
            )

        # Collect active endpoints for eligible recipients, skipping any whose
        # delivery has already been recorded.
        endpoints: list[ValidPushEndpoint] = []
        for uid in eligible_uids:
            for endpoint_doc in self.query_active_push_endpoints_for_user(uid):
                raw_ep = push_endpoint_from_snapshot(endpoint_doc)
                if raw_ep is None:
                    continue
                valid_ep = raw_ep.validated()
                if valid_ep is not None:
                    if endpoint_hash(valid_ep) not in already_delivered_eps:
                        endpoints.append(valid_ep)
        if not endpoints:
            _log.info("No remaining undelivered endpoints for message %s", message_id)
            return

        # Send — writes idempotency record inline per successful endpoint delivery.
        send_chat_push(
            endpoints=endpoints,
            payload=payload,
            actions_ref=actions_ref,
            actions_doc_exists=actions_snap.exists,
            scope_type=scope_type,
            scope_id=scope_id,
            dummy_run=dummy_run,
        )

    def handle_chat_message(
        self,
        message_doc: DocumentSnapshot | None,
        pubs_list: PubsList,
        *,
        dummy_run: bool = False,
    ) -> None:
        del pubs_list
        if message_doc is None:
            return
        self.chat_message_push_handler(
            message_doc.id,
            message_doc,
            dummy_run=dummy_run,
        )

    @property
    def query_messages(self) -> Query:
        """Return a query for the messages collection (used for chat push listener)."""
        return cast(Query, self.db.collection("messages"))

    def new_poll_event_handler(self, am: ActionMan, poll_id: PollId) -> None:
        action_document = self.db.collection("open_actions").document(poll_id)
        action_snapshot = _snapshot_get(action_document)
        poll_dict_raw = self.poll_repo.get_poll(poll_id)
        try:
            if not isinstance(poll_dict_raw, dict):
                raise TypeError("poll payload is not a dict")
            raw_date = poll_dict_raw["date"]
            poll_date = raw_date
        except (KeyError, TypeError) as exc:
            _log.warning(
                "Poll %s has missing/invalid date for open push TTL; using default TTL path (%s)",
                poll_id,
                exc,
            )
            poll_date = ""
        canonical_open_key = PushDedupeKeys.open_key(poll_id)
        action_dict = _with_legacy_alias_key(
            action_snapshot.to_dict(),
            legacy_key=poll_id,
            canonical_key=canonical_open_key,
        )
        new_action_dict = am.action_event(
            action_dict=action_dict,
            action_key=canonical_open_key,
            poll_id=poll_id,
            poll_date=poll_date,
        )
        if new_action_dict:
            _doc_set(
                action_document, cast(dict[str, object], new_action_dict), merge=True
            )

    def complete_poll_event_handler(
        self, pubs_list: "PubsList", am: ActionMan, poll_id: PollId
    ) -> None:
        poll_dict_raw = self.poll_repo.get_poll(poll_id)
        action_document = self.db.collection("comp_actions").document(poll_id)
        if poll_dict_raw is None:
            return
        poll_dict = poll_dict_raw
        if "selected" not in poll_dict:
            _log.error("Poll document %s has no selected field", poll_id)
            return
        pub_id = poll_dict["selected"]
        if pub_id not in pubs_list:
            raise RetryablePollDataNotReadyError(
                "Poll "
                f"{poll_id} selected pub {pub_id} that is not in pubs_list. "
                "This usually indicates startup race while pubs list is warming."
            )
        action_snapshot = _snapshot_get(action_document)
        canonical_complete_key = _compute_action_key(poll_id, poll_dict, pub_id)
        action_dict = action_snapshot.to_dict()
        new_action_dict = am.action_event(
            action_dict=action_dict,
            action_key=canonical_complete_key,
            poll_id=poll_id,
            poll_dict=poll_dict,
            pub_dict=pubs_list,
        )
        if new_action_dict:
            _doc_set(
                action_document, cast(dict[str, object], new_action_dict), merge=True
            )

    def query_polls_by_status(
        self, *, completed: bool, min_date: str | None = None
    ) -> Query:
        """Query polls by completion state, optionally constrained by minimum date."""
        query = self.poll_repo.get_polls_by_status(completed=completed)
        if min_date is None:
            return query
        return _query_where(query, field="date", op=">=", value=min_date)

    @property
    def query_completed_true(self) -> Query:
        """Query completed polls."""
        return self.query_polls_by_status(completed=True)

    @property
    def query_completed_false(self) -> Query:
        """Query open (incomplete) polls."""
        return self.query_polls_by_status(completed=False)

    @property
    def query_all_polls(self) -> CollectionReference:
        """Return a query for all polls (no filters)."""
        return self.poll_repo.get_all_polls()

    @property
    def query_notification_requests(self) -> Query:
        """Return a query for notification request health-check documents."""
        return cast(Query, self.db.collection("notification_req"))

    @property
    def query_admin_delete_requests(self) -> Query:
        """Return a query for admin delete requests."""
        return cast(Query, self.db.collection("admin_delete_requests"))

    @staticmethod
    def wrapped_callback(
        doc_snapshot: Sequence[DocumentSnapshot],
        changes: Sequence[DocumentChange],
        read_time: datetime,
        callback: Callable[[str, DocumentSnapshot], None],
        collection: CollectionReference,
    ) -> None:
        collection_id = str(cast(Any, collection).id)
        if collection_id == "users":
            raise ValueError("Users collection should not be watched here.")
        for change in changes:
            change_type_name = str(getattr(cast(Any, change).type, "name", ""))
            if change_type_name == "ADDED":
                callback(collection_id, cast(Any, change).document)
            elif change_type_name == "MODIFIED":
                callback(collection_id, cast(Any, change).document)
            elif change_type_name == "REMOVED":
                pass

    def all_events_except_users(
        self, callback: Callable[[str, DocumentSnapshot], None]
    ) -> None:
        collections = self.db.collections()
        for raw_collection in collections:
            collection = cast(CollectionReference, raw_collection)
            collection_id = str(cast(Any, collection).id)
            if collection_id in ["users", "roles"]:
                continue
            bound_callback = partial(
                self.wrapped_callback, callback=callback, collection=collection
            )
            cast(Any, collection).on_snapshot(bound_callback)


def patch_watch_close(callback: Callable[[str | None], None]) -> None:
    orig_close = getattr(watch.Watch, "close")

    def new_close(self: watch.Watch, reason: str | None = None) -> None:
        callback(reason)
        # Call the original close
        orig_close(self, reason)

    setattr(watch.Watch, "close", new_close)
