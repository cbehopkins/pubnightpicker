import contextlib
import html
import logging
import textwrap
from os import getenv

import mailtrap

MAILTRAP_TOKEN: str = getenv("MAILTRAP_TOKEN", "")
assert MAILTRAP_TOKEN, "MAILTRAP_TOKEN environment variable not set"


_log = logging.getLogger("SendEmail")
GOOGLEGROUPS_ADDR = "ampubnight@googlegroups.com"
BASE_TEMPLATE = textwrap.dedent(
    """\
    Every week we have a pub night to which you are cordially invited.
    The venue changes each week, though we do tend to frequent a few favourites - suggestions for venues are always welcome.
    The earliest attendees get there between 6:30 & 7:30pm and we continue through to closing time.
    
    
    This week on {event_date} we will be visiting {pub_name}"""
)
OPEN_TEMPLATE = textwrap.dedent(
    """\
    Voting has opened for this week's pub night. Please visit https://pubnightpicker.web.app/active_polls to participate in the voting."""
)


def render_template(*_, **kwargs):
    for k, v in kwargs.items():
        if v:
            kwargs[k] = html.escape(v)
    result = BASE_TEMPLATE.format(
        pub_name=kwargs["pub_name"], event_date=kwargs["event_date"]
    )
    if ("web_site" in kwargs) and kwargs["web_site"]:
        result += f"\n\n\nPub Web Site {kwargs['web_site']}\n"
    if ("address" in kwargs) and kwargs["address"]:
        result += f"\n{kwargs['address']}\n"
    if ("map" in kwargs) and kwargs["map"]:
        result += f"\n\nMap to pub {kwargs['map']}"
    if ("uid" in kwargs) and kwargs["uid"]:
        result += f"\n\nUnsubscribe at https://pubnightpicker.web.app/preferences/{kwargs['uid']}"
    return result


def send_poll_open_email(previously_actioned: bool, emails_src, dummy_run=False):
    for email, _ in emails_src():
        _log.info(f"Poll open email to send to {email}")
        if dummy_run:
            continue
        mail = mailtrap.Mail(
            sender=mailtrap.Address(
                email="ampubnight@contable.co.uk", name="ampubnight notification emails"
            ),
            to=[mailtrap.Address(email=email)],
            subject=f"Pub Night voting opened",
            text=OPEN_TEMPLATE,
            category="Pub notification",
        )
        client = mailtrap.MailtrapClient(token=MAILTRAP_TOKEN)
        client.send(mail)


def send_ampub_email(
    poll_dict, pub_dict, *, previously_actioned=False, emails_src=None, dummy_run=False
):
    selected_pub_id = poll_dict["selected"]
    pub_name = pub_dict[selected_pub_id]["name"]
    web_site = None
    with contextlib.suppress(KeyError):
        web_site = pub_dict[selected_pub_id]["web_site"]
    map = None
    with contextlib.suppress(KeyError):
        map = pub_dict[selected_pub_id]["map"]
    address = None
    with contextlib.suppress(KeyError):
        address = pub_dict[selected_pub_id]["address"]

    event_date = poll_dict["date"]
    _log.info(
        "Generating ampub email: pub_name=%r, event_date=%r, web_site=%r, address=%r, map=%r",
        pub_name,
        event_date,
        web_site,
        address,
        map,
    )

    if emails_src is None:
        src = [(GOOGLEGROUPS_ADDR, None)]
    else:
        src = list(emails_src())
    subject = (
        f"Pub Night @ RESCHEDULED::{pub_name}"
        if previously_actioned
        else f"Pub Night @ {pub_name}"
    )
    pre_text = (
        "This week's event has been rescheduled\n\n" if previously_actioned else ""
    )
    for email, uid in src:
        _log.info(f"Notification email to send to {email}")
        if dummy_run:
            continue
        mail = mailtrap.Mail(
            sender=mailtrap.Address(
                email="ampubnight@contable.co.uk", name="ampubnight notification emails"
            ),
            to=[mailtrap.Address(email=email)],
            subject=subject,
            text=pre_text
            + render_template(
                pub_name=pub_name,
                event_date=event_date,
                web_site=web_site,
                address=address,
                map=map,
                uid=uid,
            ),
            category="Pub notification",
        )
        client = mailtrap.MailtrapClient(token=MAILTRAP_TOKEN)
        client.send(mail)


if __name__ == "__main__":
    print(render_template(pub_name="Red Lion", web_site=""))
