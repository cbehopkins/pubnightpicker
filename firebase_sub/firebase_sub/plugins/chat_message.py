from firebase_sub.event import EventEnvelope, EventType
from firebase_sub.plugins.chat_push import ChatPushDbHandler, process_chat_message_push
from firebase_sub.runtime.service_adapter import (
    AdapterBackedEventPlugin,
    ServiceAdapter,
    ServiceContext,
    ServiceResult,
)


class _ChatMessageServiceAdapter(ServiceAdapter):
    """Adapter implementation for chat message push processing."""

    def __init__(
        self,
        *,
        db_handler: ChatPushDbHandler,
        dummy_run: bool,
    ) -> None:
        self._db_handler = db_handler
        self._dummy_run = dummy_run

    def name(self) -> str:
        return "chat_message_listener"

    def prepare(self, envelope: EventEnvelope) -> ServiceContext | None:
        if envelope.type != EventType.CHAT_MESSAGE or envelope.doc is None:
            return None
        message_id = envelope.document_id()
        if message_id is None:
            return None
        return ServiceContext(
            envelope=envelope,
            entity_id=message_id,
            dedupe_key=f"chat_message:{message_id}",
        )

    def execute(self, context: ServiceContext) -> ServiceResult:
        envelope = context.envelope
        if envelope.doc is None:
            return ServiceResult(success=True)
        # The chat push helper persists message delivery state inline.
        process_chat_message_push(
            self._db_handler,
            envelope.doc.id,
            envelope.doc,
            dummy_run=self._dummy_run,
        )
        return ServiceResult(success=True)

    def commit(self, context: ServiceContext, result: ServiceResult) -> None:
        # Chat push delivery state is persisted inline by the handler.
        del context, result
        return


class ChatMessageListenerPlugin(AdapterBackedEventPlugin):
    """Listener plugin for chat message push processing.

    Delivery state is written inline during chat push processing. ``mark_done()``
    is retained as the lifecycle acknowledgment hook and is a no-op.
    """

    def __init__(
        self,
        *,
        db_handler: ChatPushDbHandler,
        dummy_run: bool,
    ) -> None:
        self._chat_message_adapter = _ChatMessageServiceAdapter(
            db_handler=db_handler,
            dummy_run=dummy_run,
        )
        super().__init__(self._chat_message_adapter)
        self._db_handler = db_handler
        self._dummy_run = dummy_run

    def name(self) -> str:
        return "chat_message_listener"

    def is_enabled(self) -> bool:
        return True
