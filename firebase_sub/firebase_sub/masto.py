import contextlib
import logging
from mastodon import Mastodon

_log = logging.getLogger(__name__)

# Create an instance of the Mastodon class
mastodon = Mastodon(
    access_token="iiVGuPtfetzt9XaKnrwepGwu7cQDwBdj3tnJMq4lrGg",
    api_base_url="https://botsin.space/",
)


def ext_msg(msg, addition, max_len=500):
    if len(msg) > max_len:
        raise RuntimeError("Some moron messed up the message length")
    if (len(msg) + len(addition)) > max_len:
        return msg
    return msg + addition


def toot_for_me(poll_dict, pub_dict, previously_actioned=False, dummy_run=False):
    selected_pub_id = poll_dict["selected"]
    pub_name = pub_dict[selected_pub_id]["name"]
    web_site = None
    with contextlib.suppress(KeyError):
        web_site = pub_dict[selected_pub_id]["web_site"]
    map = None
    with contextlib.suppress(KeyError):
        map = pub_dict[selected_pub_id]["map"]

    event_date = poll_dict["date"]
    pre_text = (
        "We should go to"
        if not previously_actioned
        else "The event has beeen rescheduled to"
    )
    msg = f"{pre_text} {pub_name} on {event_date}"
    if web_site and len(msg):
        msg = ext_msg(msg, f" Pub's Web Page {web_site}")
    if map:
        msg = ext_msg(msg, f" Map to pub {map}")
    _log.info(f"Tooting:::{msg}")
    if dummy_run:
        return
    mastodon.status_post(msg)


# if __name__ == "__main__":
#     while True:
#         logging.basicConfig(level=logging.INFO)
#         doc = q.get()
#         id = doc.id
#         _log.debug(f"Got a pub ID of {id=}")
#         docs_ref = db.collection("polls").document(id)
#         poll_dict = docs_ref.get().to_dict()
#         toot_for_me(docs_ref, poll_dict)
#         time.sleep(1)
