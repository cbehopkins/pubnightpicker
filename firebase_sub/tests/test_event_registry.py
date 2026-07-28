from firebase_sub.event import EventEnvelope, EventType
from firebase_sub.runtime.event_registry import EventRegistry, EventWritebackError


class _WritebackFailingPlugin:
    def name(self) -> str:
        return "writeback_failing"

    def filter(self, envelope: EventEnvelope) -> bool:
        return envelope.type == EventType.TICK

    def handle(self, envelope: EventEnvelope) -> None:
        del envelope
        return

    def mark_done(self, envelope: EventEnvelope) -> None:
        del envelope
        raise RuntimeError("firestore write failed")


class _SuccessPlugin:
    def __init__(self) -> None:
        self.handle_calls = 0
        self.mark_done_calls = 0

    def name(self) -> str:
        return "success"

    def filter(self, envelope: EventEnvelope) -> bool:
        return envelope.type == EventType.TICK

    def handle(self, envelope: EventEnvelope) -> None:
        del envelope
        self.handle_calls += 1

    def mark_done(self, envelope: EventEnvelope) -> None:
        del envelope
        self.mark_done_calls += 1


class _HandleFailingPlugin:
    def name(self) -> str:
        return "handle_failing"

    def filter(self, envelope: EventEnvelope) -> bool:
        return envelope.type == EventType.TICK

    def handle(self, envelope: EventEnvelope) -> None:
        del envelope
        raise RuntimeError("handler failed")

    def mark_done(self, envelope: EventEnvelope) -> None:
        del envelope
        raise AssertionError("mark_done should not run")


def test_dispatch_wraps_mark_done_failure_as_writeback_error():
    registry = EventRegistry()
    plugin = _WritebackFailingPlugin()
    registry.subscribe(EventType.TICK, plugin)

    try:
        registry.dispatch(EventEnvelope(type=EventType.TICK, doc=None))
        raise AssertionError("Expected EventWritebackError")
    except EventWritebackError as exc:
        assert "mark_done writeback failed" in str(exc)
        assert isinstance(exc.__cause__, RuntimeError)
        assert str(exc.__cause__) == "firestore write failed"


def test_dispatch_completes_successfully_when_writeback_succeeds():
    registry = EventRegistry()
    plugin = _SuccessPlugin()
    registry.subscribe(EventType.TICK, plugin)

    executed = registry.dispatch(EventEnvelope(type=EventType.TICK, doc=None))

    assert executed == 1
    assert plugin.handle_calls == 1
    assert plugin.mark_done_calls == 1


def test_dispatch_preserves_handler_failure_without_writeback_wrap():
    registry = EventRegistry()
    plugin = _HandleFailingPlugin()
    registry.subscribe(EventType.TICK, plugin)

    try:
        registry.dispatch(EventEnvelope(type=EventType.TICK, doc=None))
        raise AssertionError("Expected handler failure")
    except EventWritebackError as exc:
        raise AssertionError(f"Did not expect writeback error: {exc}")
    except RuntimeError as exc:
        assert str(exc) == "handler failed"
