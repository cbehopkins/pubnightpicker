from datetime import date

from firebase_sub.runtime.config import (
    AuthDeleteMode,
    EmailMode,
    PollHistoryMode,
    PushMode,
    RuntimeConfig,
)


def test_runtime_config_from_legacy_options_maps_dummy_flags_to_modes() -> None:
    config = RuntimeConfig.from_legacy_options(
        dummy_email=True,
        dummy_push=False,
        housekeeping_interval_seconds=60,
        housekeeping_cron=None,
        all_history=False,
        poll_lookback_days=14,
        enable_real_auth_delete=False,
        admin_delete_enabled=True,
    )

    assert config.email_mode is EmailMode.DRY_RUN
    assert config.push_mode is PushMode.LIVE
    assert config.auth_delete_mode is AuthDeleteMode.DRY_RUN
    assert config.admin_delete_enabled is True
    assert config.dummy_email is True
    assert config.dummy_push is False
    assert config.enable_real_auth_delete is False
    assert config.poll_history.mode is PollHistoryMode.RECENT
    assert config.poll_history.lookback_days == 14
    assert config.comp_poll_max_retries == 10
    assert config.comp_poll_retry_delay_seconds == 1.0
    assert config.healthcheck_interval_seconds == 10.0


def test_runtime_config_poll_history_min_date_respects_all_history() -> None:
    config = RuntimeConfig.from_legacy_options(
        dummy_email=False,
        dummy_push=True,
        housekeeping_interval_seconds=300,
        housekeeping_cron="0 12 * * 4",
        all_history=True,
        poll_lookback_days=7,
        enable_real_auth_delete=True,
        admin_delete_enabled=False,
    )

    assert config.poll_history.mode is PollHistoryMode.ALL
    assert config.poll_history.min_date(today=date(2026, 5, 18)) is None
    assert config.auth_delete_mode is AuthDeleteMode.LIVE
    assert config.enable_real_auth_delete is True


def test_runtime_config_poll_history_min_date_uses_lookback_days() -> None:
    config = RuntimeConfig.from_legacy_options(
        dummy_email=False,
        dummy_push=False,
        housekeeping_interval_seconds=120,
        housekeeping_cron=None,
        all_history=False,
        poll_lookback_days=5,
        enable_real_auth_delete=False,
        admin_delete_enabled=False,
    )

    assert config.poll_history.min_date(today=date(2026, 5, 18)) == "2026-05-13"
