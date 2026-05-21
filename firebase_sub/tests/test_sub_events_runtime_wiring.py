from contextlib import nullcontext
from types import SimpleNamespace


class _FakeRuntimeConfig:
    def __init__(self) -> None:
        self.dummy_email = False
        self.dummy_push = False
        self.comp_poll_max_retries = 10
        self.comp_poll_retry_delay_seconds = 1.0
        self.healthcheck_interval_seconds = 10.0
        self.poll_history = SimpleNamespace(min_date=lambda: None)


class _FakeDbHandler:
    def __init__(self) -> None:
        self.db = object()
        self.okay = True
        self.pub_collection = object()
        self.query_active_push_endpoints_for_user = lambda _uid: []


class _FakeEventProducer:
    def build_chat_message_manager(self):
        return nullcontext()

    def build_notification_request_manager(self):
        return nullcontext()

    def build_admin_delete_request_manager(self):
        return nullcontext()

    def build_new_poll_manager(self):
        return nullcontext()

    def build_complete_poll_manager(self):
        return nullcontext()


class _FakePluginRuntime:
    def __init__(self, *, listener_plugins, housekeeping_plugins):
        del listener_plugins, housekeeping_plugins

    def __enter__(self):
        return SimpleNamespace(run_housekeeping=lambda: None)

    def __exit__(self, exc_type, exc_val, exc_tb):
        del exc_type, exc_val, exc_tb


class _FakeCanaryWatcher:
    instances: list["_FakeCanaryWatcher"] = []

    def __init__(self, db):
        del db
        self.send_count = 0
        _FakeCanaryWatcher.instances.append(self)

    def __enter__(self):
        return self

    def __exit__(self, exc_type, exc_val, exc_tb):
        del exc_type, exc_val, exc_tb

    def is_stale(self) -> bool:
        return False

    def send_canary(self) -> None:
        self.send_count += 1


class _FakePeriodicTrigger:
    calls: list[tuple[int, str]] = []

    def __init__(self, interval_seconds: int, callback):
        callback_name = getattr(callback, "__name__", callback.__class__.__name__)
        _FakePeriodicTrigger.calls.append((interval_seconds, callback_name))

    def __enter__(self):
        return self

    def __exit__(self, exc_type, exc_val, exc_tb):
        del exc_type, exc_val, exc_tb


class _FakePubsList:
    def __init__(self, pub_collection):
        del pub_collection

    def __enter__(self):
        return self

    def __exit__(self, exc_type, exc_val, exc_tb):
        del exc_type, exc_val, exc_tb


class _FakeQueueRunner:
    calls = 0

    def __init__(
        self,
        *,
        event_queue,
        healthcheck_interval_seconds,
        healthchecks,
        registry,
        scheduled_runner,
    ):
        del event_queue, healthcheck_interval_seconds, healthchecks, registry, scheduled_runner

    def run_forever(self) -> None:
        _FakeQueueRunner.calls += 1


def _patch_minimal_runtime(monkeypatch, module):
    monkeypatch.setattr(module, "get_db_handler", lambda: _FakeDbHandler())
    monkeypatch.setattr(module, "NotificationPushTestHandler", lambda *args, **kwargs: object())
    monkeypatch.setattr(module, "build_event_producer", lambda **kwargs: _FakeEventProducer())
    monkeypatch.setattr(module, "poll_open_actions", lambda *args, **kwargs: object())
    monkeypatch.setattr(module, "poll_complete_actions", lambda *args, **kwargs: object())
    monkeypatch.setattr(module, "build_listener_plugins", lambda **kwargs: [])
    monkeypatch.setattr(module, "build_housekeeping_plugins", lambda **kwargs: [])
    monkeypatch.setattr(module, "build_scheduled_housekeeping_plugins", lambda **kwargs: [])
    monkeypatch.setattr(module, "build_event_registry", lambda **kwargs: object())
    monkeypatch.setattr(module, "PluginRuntime", _FakePluginRuntime)
    monkeypatch.setattr(module, "CanaryWatcher", _FakeCanaryWatcher)
    monkeypatch.setattr(module, "PeriodicTrigger", _FakePeriodicTrigger)
    monkeypatch.setattr(module, "PubsList", _FakePubsList)
    monkeypatch.setattr(module, "QueueRunner", _FakeQueueRunner)
    monkeypatch.setattr(module, "ScheduledHousekeepingRunner", lambda plugins: object())


def test_sub_events_uses_env_gate_for_admin_delete(monkeypatch):
    import firebase_sub.cli.sub_events as module

    captured_calls: list[dict[str, object]] = []

    def _fake_from_legacy_options(**kwargs):
        captured_calls.append(kwargs)
        return _FakeRuntimeConfig()

    _patch_minimal_runtime(monkeypatch, module)
    monkeypatch.setattr(module.RuntimeConfig, "from_legacy_options", _fake_from_legacy_options)

    monkeypatch.delenv("ENABLE_ADMIN_DELETE_REQUESTS", raising=False)
    module.sub_events(
        dummy_email=False,
        dummy_push=False,
        loglevel=20,
        logfile=None,
        restart_interval=0,
        housekeeping_interval_seconds=60,
        housekeeping_cron=None,
        all_history=False,
        poll_lookback_days=7,
        canary_interval_seconds=300,
        enable_real_auth_delete=False,
    )

    monkeypatch.setenv("ENABLE_ADMIN_DELETE_REQUESTS", "true")
    module.sub_events(
        dummy_email=False,
        dummy_push=False,
        loglevel=20,
        logfile=None,
        restart_interval=0,
        housekeeping_interval_seconds=60,
        housekeeping_cron=None,
        all_history=False,
        poll_lookback_days=7,
        canary_interval_seconds=300,
        enable_real_auth_delete=True,
    )

    assert captured_calls[0]["admin_delete_enabled"] is False
    assert captured_calls[0]["enable_real_auth_delete"] is False
    assert captured_calls[1]["admin_delete_enabled"] is True
    assert captured_calls[1]["enable_real_auth_delete"] is True


def test_sub_events_registers_canary_trigger(monkeypatch):
    import firebase_sub.cli.sub_events as module

    _FakePeriodicTrigger.calls = []
    _FakeCanaryWatcher.instances = []

    _patch_minimal_runtime(monkeypatch, module)
    monkeypatch.setattr(
        module.RuntimeConfig,
        "from_legacy_options",
        lambda **kwargs: _FakeRuntimeConfig(),
    )

    module.sub_events(
        dummy_email=False,
        dummy_push=False,
        loglevel=20,
        logfile=None,
        restart_interval=0,
        housekeeping_interval_seconds=61,
        housekeeping_cron=None,
        all_history=False,
        poll_lookback_days=7,
        canary_interval_seconds=123,
        enable_real_auth_delete=False,
    )

    assert (61, "<lambda>") in _FakePeriodicTrigger.calls
    assert (123, "send_canary") in _FakePeriodicTrigger.calls
