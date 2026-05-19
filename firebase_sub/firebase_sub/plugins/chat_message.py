from contextlib import AbstractContextManager

from firebase_sub.database.handlers import DbHandler
from firebase_sub.event import EventEnvelope, EventType
from firebase_sub.plugins.protocols import EventPlugin


class ChatMessageListenerPlugin(EventPlugin):
    """Listener plugin for chat message push processing."""

    def __init__(
        self,
        *,
        db_handler: DbHandler,
        dummy_run: bool,
    ) -> None:
        self._db_handler = db_handler
        self._dummy_run = dummy_run

    def name(self) -> str:
        return "chat_message_listener"

    def is_enabled(self) -> bool:
        return True

    def build_manager(self) -> AbstractContextManager[object]:
        """Events are now produced externally by event producers."""
        # No-op manager since Firestore watches are managed by event producers
        from contextlib import nullcontext

        return nullcontext()

    def filter(self, envelope: EventEnvelope) -> bool:
        return envelope.type == EventType.CHAT_MESSAGE and envelope.doc is not None

    def handle(self, envelope: EventEnvelope) -> None:
        if envelope.doc is None:
            return
        self._db_handler.chat_message_push_handler(
            envelope.doc.id,
            envelope.doc,
            dummy_run=self._dummy_run,
        )

    def mark_done(self, envelope: EventEnvelope) -> None:
        # Chat push delivery state is persisted inside the handler.
        del envelope
        return
