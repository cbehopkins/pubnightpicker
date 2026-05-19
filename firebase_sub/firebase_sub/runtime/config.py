from dataclasses import dataclass
from datetime import date, timedelta
from enum import StrEnum


class EmailMode(StrEnum):
    LIVE = "live"
    DRY_RUN = "dry_run"
    DISABLED = "disabled"


class PushMode(StrEnum):
    LIVE = "live"
    DRY_RUN = "dry_run"
    DISABLED = "disabled"


class AuthDeleteMode(StrEnum):
    DISABLED = "disabled"
    DRY_RUN = "dry_run"
    LIVE = "live"


class PollHistoryMode(StrEnum):
    ALL = "all"
    RECENT = "recent"


@dataclass(frozen=True)
class HousekeepingSchedule:
    interval_seconds: int
    cron_expression: str | None = None

    @property
    def uses_cron(self) -> bool:
        return self.cron_expression is not None


@dataclass(frozen=True)
class PollHistory:
    mode: PollHistoryMode
    lookback_days: int

    def min_date(self, *, today: date | None = None) -> str | None:
        if self.mode is PollHistoryMode.ALL:
            return None
        today = today or date.today()
        return (today - timedelta(days=self.lookback_days)).isoformat()


@dataclass(frozen=True)
class RuntimeConfig:
    email_mode: EmailMode
    push_mode: PushMode
    auth_delete_mode: AuthDeleteMode
    admin_delete_enabled: bool
    housekeeping: HousekeepingSchedule
    poll_history: PollHistory
    comp_poll_max_retries: int = 10
    comp_poll_retry_delay_seconds: float = 1.0
    healthcheck_interval_seconds: float = 10.0

    @property
    def dummy_email(self) -> bool:
        return self.email_mode is EmailMode.DRY_RUN

    @property
    def dummy_push(self) -> bool:
        return self.push_mode is PushMode.DRY_RUN

    @property
    def enable_real_auth_delete(self) -> bool:
        return self.auth_delete_mode is AuthDeleteMode.LIVE

    @classmethod
    def from_legacy_options(
        cls,
        *,
        dummy_email: bool,
        dummy_push: bool,
        housekeeping_interval_seconds: int,
        housekeeping_cron: str | None,
        all_history: bool,
        poll_lookback_days: int,
        enable_real_auth_delete: bool,
        admin_delete_enabled: bool,
    ) -> "RuntimeConfig":
        return cls(
            email_mode=EmailMode.DRY_RUN if dummy_email else EmailMode.LIVE,
            push_mode=PushMode.DRY_RUN if dummy_push else PushMode.LIVE,
            auth_delete_mode=(
                AuthDeleteMode.LIVE
                if enable_real_auth_delete
                else AuthDeleteMode.DRY_RUN
            ),
            admin_delete_enabled=admin_delete_enabled,
            housekeeping=HousekeepingSchedule(
                interval_seconds=housekeeping_interval_seconds,
                cron_expression=housekeeping_cron,
            ),
            poll_history=PollHistory(
                mode=PollHistoryMode.ALL if all_history else PollHistoryMode.RECENT,
                lookback_days=poll_lookback_days,
            ),
        )
