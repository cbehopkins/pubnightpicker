import threading
from collections.abc import Callable
from functools import wraps
from typing import ParamSpec, TypeVar

Params = ParamSpec("Params")
ReturnT = TypeVar("ReturnT")


class SkipCall(Exception):
    """Raised by on_stall callback to skip the decorated function call.

    Optionally carries a return value to use for the skipped call.
    """

    def __init__(self, return_value: object = None) -> None:
        self.return_value = return_value
        super().__init__()


class TokenBucket:
    def __init__(
        self,
        *,
        refill_amount: int,
        max_tokens: int,
        refill_interval_seconds: float,
        initial_tokens: int | None = None,
        on_stall: Callable[[], None] | None = None,
    ) -> None:
        if refill_amount <= 0:
            raise ValueError("refill_amount must be greater than 0")
        if max_tokens <= 0:
            raise ValueError("max_tokens must be greater than 0")
        if refill_interval_seconds <= 0:
            raise ValueError("refill_interval_seconds must be greater than 0")

        starting_tokens = max_tokens if initial_tokens is None else initial_tokens
        if starting_tokens < 0:
            raise ValueError("initial_tokens must be greater than or equal to 0")

        self.refill_amount = refill_amount
        self.max_tokens = max_tokens
        self.refill_interval_seconds = refill_interval_seconds
        self._on_stall = on_stall
        self._tokens = min(starting_tokens, max_tokens)
        self._stop_event = threading.Event()
        self._condition = threading.Condition()
        self._refill_thread = threading.Thread(
            target=self._run_refill_loop,
            name="token-bucket-refill",
            daemon=True,
        )
        self._refill_thread.start()

    @property
    def tokens(self) -> int:
        with self._condition:
            return self._tokens

    def acquire(self) -> None:
        with self._condition:
            stall_notified = False
            while self._tokens <= 0:
                if self._stop_event.is_set():
                    raise RuntimeError("TokenBucket is closed")
                if not stall_notified and self._on_stall:
                    stall_notified = True
                    self._on_stall()
                self._condition.wait()
            self._tokens -= 1

    def close(self) -> None:
        self._stop_event.set()
        with self._condition:
            self._condition.notify_all()
        self._refill_thread.join(timeout=self.refill_interval_seconds * 2)

    def _run_refill_loop(self) -> None:
        while not self._stop_event.wait(self.refill_interval_seconds):
            with self._condition:
                next_token_count = min(
                    self.max_tokens,
                    self._tokens + self.refill_amount,
                )
                if next_token_count != self._tokens:
                    self._tokens = next_token_count
                    self._condition.notify_all()


def rate_limited(
    token_bucket: TokenBucket,
) -> Callable[[Callable[Params, ReturnT]], Callable[Params, ReturnT]]:
    def decorator(func: Callable[Params, ReturnT]) -> Callable[Params, ReturnT]:
        @wraps(func)
        def wrapped(*args: Params.args, **kwargs: Params.kwargs) -> ReturnT:
            try:
                token_bucket.acquire()
            except SkipCall as e:
                return e.return_value  # type: ignore[return-value]
            return func(*args, **kwargs)

        return wrapped

    return decorator
