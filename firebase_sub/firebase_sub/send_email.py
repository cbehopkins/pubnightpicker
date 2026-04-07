import contextlib
import html
import logging
import os
import textwrap
import time
from os import getenv
from collections.abc import Callable, Iterable
from typing import Any, Protocol

import mailtrap
from pydantic import ValidationError

from firebase_sub.action_track import CallbackExceptionRetry
from firebase_sub.common.rate_limit import SkipCall, TokenBucket, rate_limited
from firebase_sub.models.notification_models import PollPayload, VenuePayload
from firebase_sub.my_types import (
    EmailAddr,
    MissingPubError,
    PollDocument,
    UserId,
    VenueDocument,
    VenueType,
)

_SELF_EMAIL_ADDR = "ampubnight@contable.co.uk"
_SELF_EMAIL_NAME = "ampubnight notification emails"
_ADMIN_EMAIL_ADDR = "cbehopkins@gmail.com"
_SELF_EMAIL = mailtrap.Address(email=_SELF_EMAIL_ADDR, name=_SELF_EMAIL_NAME)

SECONDS_IN_HOUR = 60 * 60
SECONDS_IN_DAY = SECONDS_IN_HOUR * 24

def skip_mail_send() -> None:
    raise SkipCall()

STALLED_MAIL_SEND_BUCKET = TokenBucket(
    refill_amount=int(1),
    max_tokens=int(1),
    refill_interval_seconds=float(SECONDS_IN_DAY),
    on_stall=skip_mail_send,
)


@rate_limited(STALLED_MAIL_SEND_BUCKET)
def _on_mail_send_stall() -> None:
    logging.warning(
        "Mail send rate limit stall: no tokens available in MAIL_SEND_BUCKET"
    )
    client = _mail_client(dummy_run=False)
    mail = mailtrap.Mail(
        sender=_SELF_EMAIL,
        to=[mailtrap.Address(email=_ADMIN_EMAIL_ADDR)],
        subject="Mail send rate limit stall",
        text="ampubnight mail send rate limit stall: no tokens available in MAIL_SEND_BUCKET",
        category="Pub notification",
    )
    client.send(mail)
    time.sleep(SECONDS_IN_HOUR * 6)


MAIL_SEND_BUCKET = TokenBucket(
    refill_amount=int(os.getenv("MAIL_SEND_REFILL_AMOUNT", "4")),
    max_tokens=int(os.getenv("MAIL_SEND_MAX_TOKENS", "4")),
    refill_interval_seconds=float(
        os.getenv("MAIL_SEND_REFILL_INTERVAL_SECONDS", str(SECONDS_IN_DAY))
    ),
    on_stall=_on_mail_send_stall,
)


class VenueLookup(Protocol):
    def __getitem__(self, key: str, /) -> VenueDocument: ...


_log = logging.getLogger("SendEmail")
GOOGLEGROUPS_ADDR = "ampubnight@googlegroups.com"
BASE_TEMPLATE = textwrap.dedent(
    """\
    Every week we have a pub night to which you are cordially invited.
    The venue changes each week, though we do tend to frequent a few favourites - suggestions for venues are always welcome.
    The earliest attendees get there between 6:30 & 7:30pm and we continue through to closing time.
    """
)
PUB_TEMPLATE = textwrap.dedent(
    """\
    This week on {event_date} we will be visiting {pub_name}"""
)

EVENT_TEMPLATE = textwrap.dedent(
    """\
    Every week we have a pub night to which you are cordially invited.
    This week the destination is an event venue.

    On {event_date} we will be attending {venue_name}."""
)
RESTAURANT_TEMPLATE = textwrap.dedent(
    """\
    Every week we have a pub night to which you are cordially invited.
    This week the destination is a restaurant.

    On {event_date} we will be visiting {venue_name}."""
)
RESTAURANT_BLOCK_TEMPLATE = textwrap.dedent(
    """\

    Before the pub we are meeting at {venue_name}.{restaurant_time_block}"""
)
OPEN_TEMPLATE = textwrap.dedent(
    """\
    Voting has opened for this week's pub night. Please visit https://pubnightpicker.web.app/active_polls to participate in the voting."""
)


def _escape(value: str | None) -> str | None:
    if not value:
        return value
    return html.escape(value)


def _append_venue_details(
    message: str, *, venue: VenuePayload, uid: UserId | None, pub_wording: bool
) -> str:
    match venue.venue_type:
        case VenueType.PUB:
            site_label = "Pub Web Site"
            map_label = "Map to pub"
        case VenueType.EVENT:
            site_label = "Event Web Site"
            map_label = "Map to event"
        case VenueType.RESTAURANT:
            site_label = "Restaurant Web Site"
            map_label = "Map to restaurant"
        case _:
            # For now no type == pub.
            # At some point we'll fix this in the database
            # site_label = "Venue Web Site"
            site_label = "Pub Web Site"
            map_label = "Map to pub"
    result = message
    if venue.web_site:
        result += f"\n{site_label} {_escape(venue.web_site)}\n"
    if venue.address:
        result += f"\n{_escape(venue.address)}\n"
    if venue.map:
        result += f"\n\n{map_label} {_escape(venue.map)}"
    if uid:
        result += (
            "\n\nUnsubscribe at https://pubnightpicker.web.app/preferences/"
            f"{_escape(uid)}"
        )
    return result


class MailSender(Protocol):
    def send(self, mail: mailtrap.Mail) -> dict[str, Any]: ...


class DummyClient:
    def send(self, mail: mailtrap.Mail) -> dict[str, Any]:
        _log.info(f"DummyClient sending email: {mail}")
        return {"status": "dummy_sent"}


def _mail_client(dummy_run: bool = True) -> MailSender:
    if dummy_run:
        return DummyClient()
    token = getenv("MAILTRAP_TOKEN", "")
    if not token:
        raise CallbackExceptionRetry("MAILTRAP_TOKEN environment variable not set")
    return mailtrap.MailtrapClient(token=token)


def _restaurant_block(
    restaurant_venue: VenuePayload | None, restaurant_time: str | None
) -> str:
    if restaurant_venue is None:
        return ""
    if restaurant_time:
        restaurant_time_block = f" We will be meeting there at {restaurant_time}."
    else:
        restaurant_time_block = ""
    base = RESTAURANT_BLOCK_TEMPLATE.format(
        venue_name=_escape(restaurant_venue.name),
        restaurant_time_block=restaurant_time_block,
    )

    return (
        _append_venue_details(base, venue=restaurant_venue, uid=None, pub_wording=False)
        + "\n\n"
    )


def _render_pub_template(
    *,
    selected_venue: VenuePayload,
    restaurant_venue: VenuePayload | None,
    event_date: str,
    restaurant_time: str | None,
    uid: UserId | None,
) -> str:
    base = BASE_TEMPLATE.format(
        pub_name=_escape(selected_venue.name),
        event_date=_escape(event_date),
    )
    base += _restaurant_block(restaurant_venue, restaurant_time)
    base += PUB_TEMPLATE.format(
        pub_name=_escape(selected_venue.name),
        event_date=_escape(event_date),
    )
    return _append_venue_details(base, venue=selected_venue, uid=uid, pub_wording=True)


def _render_non_pub_template(
    *,
    selected_venue: VenuePayload,
    restaurant_venue: VenuePayload | None,
    event_date: str,
    restaurant_time: str | None,
    uid: UserId | None,
) -> str:
    if selected_venue.venue_type == VenueType.EVENT:
        message = EVENT_TEMPLATE.format(
            venue_name=_escape(selected_venue.name),
            event_date=_escape(event_date),
        )
    else:
        message = RESTAURANT_TEMPLATE.format(
            venue_name=_escape(selected_venue.name),
            event_date=_escape(event_date),
        )
    message += _restaurant_block(restaurant_venue, restaurant_time)
    return _append_venue_details(
        message, venue=selected_venue, uid=uid, pub_wording=False
    )


def build_notification_text(
    *,
    selected_venue: VenuePayload,
    restaurant_venue: VenuePayload | None,
    event_date: str,
    restaurant_time: str | None = None,
    uid: UserId | None,
) -> str:
    if selected_venue.venue_type == VenueType.PUB:
        return _render_pub_template(
            selected_venue=selected_venue,
            restaurant_venue=restaurant_venue,
            event_date=event_date,
            restaurant_time=restaurant_time,
            uid=uid,
        )
    return _render_non_pub_template(
        selected_venue=selected_venue,
        restaurant_venue=None,
        event_date=event_date,
        restaurant_time=restaurant_time,
        uid=uid,
    )


def _resolve_payloads(
    *, poll_dict: PollDocument, pub_dict: VenueLookup
) -> tuple[PollPayload, VenuePayload, VenuePayload | None]:
    try:
        poll = PollPayload.model_validate(poll_dict)
        selected_venue = VenuePayload.model_validate(pub_dict[poll.selected])
    except (MissingPubError, KeyError, ValidationError) as exc:
        raise CallbackExceptionRetry(f"Invalid poll/venue payload: {exc}") from exc

    restaurant_venue: VenuePayload | None = None
    if poll.restaurant:
        with contextlib.suppress(MissingPubError, KeyError, ValidationError):
            restaurant_venue = VenuePayload.model_validate(pub_dict[poll.restaurant])
        if restaurant_venue is None:
            _log.warning(
                "Poll references restaurant id %s but no matching venue document was found",
                poll.restaurant,
            )

    return poll, selected_venue, restaurant_venue


@rate_limited(MAIL_SEND_BUCKET)
def send_poll_open_email(
    previously_actioned: bool,
    emails_src: Callable[[], Iterable[tuple[EmailAddr, UserId | None]]],
    dummy_run: bool = False,
):
    client = _mail_client(dummy_run)
    for email, _ in emails_src():
        _log.info(f"Poll open email to send to {email}")
        mail = mailtrap.Mail(
            sender=_SELF_EMAIL,
            to=[mailtrap.Address(email=email)],
            subject=f"Pub Night voting opened",
            text=OPEN_TEMPLATE,
            category="Pub notification",
        )
        result = client.send(mail)
        _log.info(f"Mail send result for {email}: {result}")


@rate_limited(MAIL_SEND_BUCKET)
def send_ampub_email(
    poll_dict: PollDocument,
    pub_dict: VenueLookup,
    *,
    previously_actioned: bool = False,
    emails_src: Callable[[], Iterable[tuple[EmailAddr, UserId | None]]] | None = None,
    dummy_run: bool = False,
):
    poll, selected_venue, restaurant_venue = _resolve_payloads(
        poll_dict=poll_dict,
        pub_dict=pub_dict,
    )

    _log.info(
        "Generating notification email: venue_name=%r, event_date=%r, venue_type=%r, has_restaurant=%r",
        selected_venue.name,
        poll.date,
        selected_venue.venue_type,
        restaurant_venue is not None,
    )

    if emails_src is None:
        src = [(GOOGLEGROUPS_ADDR, None)]
    else:
        src = list(emails_src())
    subject = (
        f"Pub Night @ RESCHEDULED::{selected_venue.name}"
        if previously_actioned
        else f"Pub Night @ {selected_venue.name}"
    )
    pre_text = (
        "This week's event has been rescheduled\n\n" if previously_actioned else ""
    )
    client = _mail_client(dummy_run)
    for email, uid in src:
        contents = pre_text + build_notification_text(
            selected_venue=selected_venue,
            restaurant_venue=restaurant_venue,
            event_date=poll.date,
            restaurant_time=poll.restaurant_time,
            uid=uid,
        )
        _log.info(f"Notification email to send to {email}::{contents}")
        mail = mailtrap.Mail(
            sender=_SELF_EMAIL,
            to=[mailtrap.Address(email=email)],
            subject=subject,
            text=contents,
            category="Pub notification",
        )
        result = client.send(mail)
        _log.info(f"Mail send result for {email}: {result}")


if __name__ == "__main__":
    print(
        build_notification_text(
            selected_venue=VenuePayload(name="Red Lion"),
            restaurant_venue=None,
            event_date="2026-03-30",
            uid=None,
        )
    )
