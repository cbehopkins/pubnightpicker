from unittest.mock import MagicMock

from firebase_sub.event import EventType
from firebase_sub.runtime.event_producers import EventProducer


def _producer() -> EventProducer:
    producer = object.__new__(EventProducer)
    producer.event_queue = MagicMock()
    return producer


def _document(selected: object) -> MagicMock:
    document = MagicMock()
    document.id = "poll-1"
    document.to_dict.return_value = {"selected": selected}
    return document


def test_new_poll_callback_uses_selected_venue(caplog):
    producer = _producer()

    producer._new_poll_callback(_document(""))
    producer._new_poll_callback(_document("venue-1"))
    producer._new_poll_callback(_document("any"))

    producer.event_queue.put.assert_called_once()
    assert producer.event_queue.put.call_args.args[0].type == EventType.NEW_POLL
    assert 'unexpected selected Venue ID "any"' in caplog.text


def test_complete_poll_callback_uses_selected_venue(caplog):
    producer = _producer()

    producer._complete_poll_callback(_document(""))
    producer._complete_poll_callback(_document("venue-1"))
    producer._complete_poll_callback(_document("any"))

    assert producer.event_queue.put.call_count == 2
    assert all(
        call.args[0].type == EventType.COMP_POLL
        for call in producer.event_queue.put.call_args_list
    )
    assert 'unexpected selected Venue ID "any"' in caplog.text
