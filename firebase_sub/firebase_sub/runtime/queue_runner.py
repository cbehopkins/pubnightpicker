"""Queue runner: event dispatch loop with healthcheck integration."""

import logging
import queue as _queue
from collections.abc import Callable, Sequence

from firebase_sub.database.pubs_list import PubsList
from firebase_sub.event import Event
from firebase_sub.runtime.job_queue import JobQueue

_log = logging.getLogger(__name__)


class QueueRunner:
    """Processes events from a JobQueue, running healthchecks on idle timeout.

    Healthchecks are callables that return ``None`` when healthy or an error
    message string when a problem is detected.  The first non-None message
    triggers ``SystemExit``.
    """

    def __init__(
        self,
        *,
        event_queue: JobQueue[Event],
        pubs_list: PubsList,
        healthcheck_interval_seconds: float,
        healthchecks: Sequence[Callable[[], str | None]],
    ) -> None:
        self._queue = event_queue
        self._pubs_list = pubs_list
        self._healthcheck_interval_seconds = healthcheck_interval_seconds
        self._healthchecks = list(healthchecks)

    def run_forever(self) -> None:
        """Process events until a healthcheck fails or an unhandled error occurs."""
        while True:
            try:
                event = self._queue.get(timeout=self._healthcheck_interval_seconds)
            except _queue.Empty:
                for check in self._healthchecks:
                    if msg := check():
                        raise SystemExit(msg)
                continue

            event.handle_queue_item(self._pubs_list)
            _log_event(event)


def _log_event(event: Event) -> None:
    if not event.doc:
        _log.debug("Completed Event: Type:%s", event.type)
        return
    doc = event.doc.to_dict()
    if doc is None:
        _log.warning(
            "Received event %s for doc %s with no payload",
            event.type,
            event.doc.id,
        )
        return
    event_date = doc.get("date")
    completed = doc.get("completed", False)
    _log.info(
        "Completed Event: Type:%s, Date:%s, Completed:%s",
        event.type,
        event_date,
        completed,
    )
