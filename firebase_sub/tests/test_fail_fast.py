import threading
from types import SimpleNamespace

from firebase_sub.runtime.fail_fast import install_fail_fast_thread_excepthook


def test_install_fail_fast_thread_excepthook_exits_on_thread_exception() -> None:
    calls: list[int] = []

    def _fake_exit(code: int) -> None:
        calls.append(code)

    restore = install_fail_fast_thread_excepthook(exit_func=_fake_exit)
    try:
        args = SimpleNamespace(
            thread=SimpleNamespace(name="worker-1"),
            exc_type=RuntimeError,
            exc_value=RuntimeError("boom"),
            exc_traceback=None,
        )
        threading.excepthook(args)
    finally:
        restore()

    assert calls == [1]


def test_install_fail_fast_thread_excepthook_restore_reinstates_previous_hook() -> None:
    previous = threading.excepthook
    restore = install_fail_fast_thread_excepthook(exit_func=lambda _code: None)
    restore()

    assert threading.excepthook is previous
