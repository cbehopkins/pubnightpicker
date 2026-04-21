import functools
import logging
import time

_log = logging.getLogger(__name__)


def retry[
    **P,
    R,
](
    *,
    retry_errors: tuple[type[Exception], ...],
    max_retries: int,
    delay_seconds: float,
    operation_name: str,
):
    """Decorator factory that retries the wrapped function on specified exceptions."""
    from collections.abc import Callable

    def decorator(callback: Callable[P, R]) -> Callable[P, R]:
        @functools.wraps(callback)
        def wrapped(*args: P.args, **kwargs: P.kwargs) -> R:
            for attempt in range(1, max_retries):
                try:
                    return callback(*args, **kwargs)
                except retry_errors:
                    _log.warning(
                        "Retrying %s (attempt %s/%s)",
                        operation_name,
                        attempt,
                        max_retries,
                    )
                    time.sleep(delay_seconds)
            return callback(*args, **kwargs)

        return wrapped

    return decorator
