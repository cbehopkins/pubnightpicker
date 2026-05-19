from functools import partial
from typing import Any

from firebase_sub.action_track import ActionMan, ActionType
from firebase_sub.database.handlers import DbHandler
from firebase_sub.push_contract import (
    PUSH_EVENT_POLL_COMPLETED,
    PUSH_EVENT_POLL_OPENED,
    PUSH_PREFERENCE_FIELD,
)
from firebase_sub.send_email import send_ampub_email, send_poll_open_email
from firebase_sub.send_push import send_poll_complete_push, send_poll_open_push


def poll_open_actions(
    dummy_email: bool, dummy_push: bool, db_handler: DbHandler
) -> ActionMan:
    send_poll_open_email_i = partial(
        send_poll_open_email,
        emails_src=db_handler.query_open_emails,
    )
    open_am = ActionMan(dummy_email)
    open_am.bind(ActionType.EMAIL, send_poll_open_email_i)
    send_poll_open_push_impl = partial(
        send_poll_open_push,
        endpoints_src=partial(
            db_handler.query_active_push_endpoints,
            PUSH_PREFERENCE_FIELD[PUSH_EVENT_POLL_OPENED],
        ),
    )

    def send_poll_open_push_i(
        *args: Any,
        previously_actioned: bool,
        dummy_run: bool,
        **kwargs: Any,
    ) -> None:
        send_poll_open_push_impl(
            *args,
            previously_actioned=previously_actioned,
            dummy_run=dummy_run,
            **kwargs,
        )

    open_am.bind(
        ActionType.PUSH,
        send_poll_open_push_i,
        dummy_run=dummy_push,
    )
    return open_am


def poll_complete_actions(
    dummy_email: bool,
    dummy_push: bool,
    db_handler: DbHandler,
) -> ActionMan:
    send_mail_list_email = partial(
        send_ampub_email
    )  # defaults to Google Groups mailing list
    send_personal_email = partial(
        send_ampub_email,
        emails_src=db_handler.query_personal_emails,
    )
    complete_am = ActionMan(dummy_email)
    complete_am.bind(ActionType.EMAIL, send_mail_list_email)
    complete_am.bind(ActionType.PEMAIL, send_personal_email)
    send_push_impl = partial(
        send_poll_complete_push,
        endpoints_src=partial(
            db_handler.query_active_push_endpoints,
            PUSH_PREFERENCE_FIELD[PUSH_EVENT_POLL_COMPLETED],
        ),
    )

    def send_push_i(
        *args: Any,
        previously_actioned: bool,
        dummy_run: bool,
        **kwargs: Any,
    ) -> None:
        send_push_impl(
            *args,
            previously_actioned=previously_actioned,
            dummy_run=dummy_run,
            **kwargs,
        )

    complete_am.bind(
        ActionType.PUSH,
        send_push_i,
        dummy_run=dummy_push,
    )
    return complete_am
