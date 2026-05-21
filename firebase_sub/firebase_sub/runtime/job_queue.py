import queue
from typing import Generic, TypeVar

T = TypeVar("T")


class JobQueue(Generic[T]):
    """Small wrapper around queue.Queue to make queue usage explicit in runtime code."""

    def __init__(self) -> None:
        self._queue: queue.Queue[T] = queue.Queue()

    def put(self, item: T) -> None:
        self._queue.put(item)

    def get(self, timeout: float | None = None) -> T:
        return self._queue.get(timeout=timeout)
