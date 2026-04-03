import logging
import threading
from dataclasses import dataclass
from datetime import datetime
from collections.abc import Callable, Sequence

from croniter import croniter

_log = logging.getLogger(__name__)


@dataclass(frozen=True)
class HousekeepingTask:
    """A named housekeeping callback."""

    name: str
    callback: Callable[[], None]


class IntervalSchedule:
    """Simple interval schedule used for development and periodic jobs."""

    def __init__(self, interval_seconds: int):
        if interval_seconds <= 0:
            raise ValueError("interval_seconds must be > 0")
        self.interval_seconds = interval_seconds

    def is_due(self, now: datetime, last_run: datetime | None) -> bool:
        if last_run is None:
            return True
        return (now - last_run).total_seconds() >= self.interval_seconds


class HousekeepingRunner:
    """Run housekeeping tasks whenever the schedule says a run is due."""

    def __init__(self, tasks: Sequence[HousekeepingTask], schedule: IntervalSchedule):
        self.tasks = list(tasks)
        self.schedule = schedule
        self.last_run: datetime | None = None

    def maybe_run(self, now: datetime | None = None) -> None:
        current_time = now or datetime.now()
        if not self.schedule.is_due(current_time, self.last_run):
            return

        _log.info("Housekeeping run started (%s tasks)", len(self.tasks))
        for task in self.tasks:
            try:
                task.callback()
                _log.info("Housekeeping task completed: %s", task.name)
            except Exception:
                _log.exception("Housekeeping task failed: %s", task.name)
        self.last_run = current_time
        _log.info("Housekeeping run finished")


class PeriodicTrigger:
    """Run a callback repeatedly on a fixed interval in a daemon thread."""

    def __init__(self, interval_seconds: int, callback: Callable[[], None]):
        if interval_seconds <= 0:
            raise ValueError("interval_seconds must be > 0")
        self.interval_seconds = interval_seconds
        self.callback = callback
        self._stop_event = threading.Event()
        self._thread: threading.Thread | None = None

    def _run(self) -> None:
        while not self._stop_event.wait(self.interval_seconds):
            self.callback()

    def __enter__(self) -> "PeriodicTrigger":
        self._stop_event.clear()
        self._thread = threading.Thread(target=self._run, daemon=True)
        self._thread.start()
        return self

    def __exit__(self, exc_type, exc_val, exc_tb) -> None:
        self._stop_event.set()
        if self._thread is not None:
            self._thread.join(timeout=1.0)
            self._thread = None


class CroniterTrigger:
    """Run a callback on a cron schedule in a daemon thread."""

    def __init__(self, cron_expression: str, callback: Callable[[], None]):
        self.cron_expression = cron_expression.strip()
        if not self.cron_expression:
            raise ValueError("cron_expression cannot be empty")
        self.callback = callback
        self._stop_event = threading.Event()
        self._thread: threading.Thread | None = None
        # Validate expression once at startup.
        croniter(self.cron_expression, datetime.now())

    def _run(self) -> None:
        while True:
            now = datetime.now()
            cron = croniter(self.cron_expression, now)
            next_run = cron.get_next(datetime)
            wait_seconds = max((next_run - now).total_seconds(), 0.0)
            if self._stop_event.wait(wait_seconds):
                break
            self.callback()

    def __enter__(self) -> "CroniterTrigger":
        self._stop_event.clear()
        self._thread = threading.Thread(target=self._run, daemon=True)
        self._thread.start()
        return self

    def __exit__(self, exc_type, exc_val, exc_tb) -> None:
        self._stop_event.set()
        if self._thread is not None:
            self._thread.join(timeout=1.0)
            self._thread = None
