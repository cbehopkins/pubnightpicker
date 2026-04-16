import threading
import time

from firebase_sub.common.rate_limit import SkipCall, TokenBucket, rate_limited


def test_rate_limited_decorator_uses_shared_bucket_across_functions():
    bucket = TokenBucket(
        refill_amount=1,
        max_tokens=2,
        refill_interval_seconds=0.05,
    )
    call_order: list[str] = []

    @rate_limited(bucket)
    def first(label: str) -> str:
        call_order.append(label)
        return label

    @rate_limited(bucket)
    def second(label: str) -> str:
        call_order.append(label)
        return label

    released = threading.Event()
    third_result: list[str] = []

    def invoke_third_call() -> None:
        third_result.append(first("third"))
        released.set()

    worker = threading.Thread(target=invoke_third_call)
    started_at = time.monotonic()

    try:
        assert first("first") == "first"
        assert second("second") == "second"

        worker.start()

        assert not released.wait(0.02)
        assert released.wait(0.25)

        elapsed = time.monotonic() - started_at
        assert elapsed >= 0.04
        assert third_result == ["third"]
        assert call_order == ["first", "second", "third"]
    finally:
        worker.join(timeout=0.25)
        bucket.close()


def test_token_bucket_refill_saturates_at_maximum() -> None:
    bucket = TokenBucket(
        refill_amount=2,
        max_tokens=3,
        refill_interval_seconds=0.03,
        initial_tokens=0,
    )

    try:
        time.sleep(0.12)
        assert bucket.tokens == 3
    finally:
        bucket.close()


def test_on_stall_callback_called_once_when_stalling() -> None:
    stall_calls: list[int] = []

    def on_stall() -> None:
        stall_calls.append(1)

    bucket = TokenBucket(
        refill_amount=1,
        max_tokens=1,
        refill_interval_seconds=0.05,
        on_stall=on_stall,
    )

    released = threading.Event()

    def exhaust_and_stall() -> None:
        bucket.acquire()
        released.set()
        bucket.acquire()

    worker = threading.Thread(target=exhaust_and_stall)
    worker.start()

    try:
        assert released.wait(0.15)
        assert len(stall_calls) == 1
    finally:
        worker.join(timeout=0.2)
        bucket.close()


def test_skip_call_exception_skips_decorated_function() -> None:
    call_count = 0

    def on_stall() -> None:
        raise SkipCall(return_value="email_sent")

    bucket = TokenBucket(
        refill_amount=100,
        max_tokens=1,
        refill_interval_seconds=1.0,
        on_stall=on_stall,
    )

    @rate_limited(bucket)
    def important_function() -> str:
        nonlocal call_count
        call_count += 1
        return "executed"

    try:
        assert important_function() == "executed"
        assert call_count == 1

        result = important_function()
        assert result == "email_sent"
        assert call_count == 1
    finally:
        bucket.close()
