from firebase_sub.models.notification_models import VenuePayload
from firebase_sub.my_types import PollDocument, VenueDocument
from firebase_sub.send_email import build_notification_text, send_ampub_email


class _FakeAddress:
    def __init__(self, **kwargs):
        self.kwargs = kwargs


class _FakeMail:
    def __init__(self, **kwargs):
        self.kwargs = kwargs


class _FakeClient:
    def __init__(self):
        self.sent = []

    def send(self, mail):
        self.sent.append(mail)


def test_pub_template_keeps_pub_wording():
    text = build_notification_text(
        selected_venue=VenuePayload.model_validate(
            {
                "name": "The Red Lion",
                "venueType": "pub",
                "map": "https://maps.example/pub",
                "web_site": "https://pub.example",
            }
        ),
        restaurant_venue=None,
        event_date="2026-04-01",
        uid="user-1",
    )

    assert "we will be visiting The Red Lion" in text
    assert "Pub Web Site" in text
    assert "Map to pub" in text


def test_event_template_uses_event_wording_and_venue_map_label():
    text = build_notification_text(
        selected_venue=VenuePayload.model_validate(
            {
                "name": "Beer Festival",
                "venueType": "event",
                "map": "https://maps.example/event",
            }
        ),
        restaurant_venue=None,
        event_date="2026-04-02",
        uid=None,
    )

    assert "destination is an event venue" in text
    assert "we will be attending Beer Festival" in text
    assert "Map to venue" in text


def test_restaurant_template_uses_restaurant_wording():
    text = build_notification_text(
        selected_venue=VenuePayload.model_validate(
            {
                "name": "Bistro 19",
                "venueType": "restaurant",
            }
        ),
        restaurant_venue=None,
        event_date="2026-04-03",
        uid=None,
    )

    assert "destination is a restaurant" in text
    assert "we will be visiting Bistro 19" in text


def test_pub_template_includes_restaurant_pre_block_when_present():
    text = build_notification_text(
        selected_venue=VenuePayload.model_validate(
            {"name": "The Swan", "venueType": "pub"}
        ),
        restaurant_venue=VenuePayload.model_validate(
            {"name": "Nosh Place", "venueType": "restaurant"}
        ),
        event_date="2026-04-04",
        uid=None,
    )

    assert "Before the pub we are meeting at Nosh Place" in text


def test_missing_venue_type_defaults_to_pub():
    selected = VenuePayload.model_validate({"name": "Legacy Venue"})

    text = build_notification_text(
        selected_venue=selected,
        restaurant_venue=None,
        event_date="2026-04-05",
        uid=None,
    )

    assert selected.venue_type.value == "pub"
    assert "we will be visiting Legacy Venue" in text


def test_send_ampub_email_builds_pub_message_without_sending(monkeypatch):
    fake_client = _FakeClient()
    monkeypatch.setattr("firebase_sub.send_email.mailtrap.Mail", _FakeMail)
    monkeypatch.setattr("firebase_sub.send_email.mailtrap.Address", _FakeAddress)
    monkeypatch.setattr("firebase_sub.send_email._mailtrap_client", lambda: fake_client)

    poll_dict: PollDocument = {
        "selected": "pub-1",
        "date": "2026-04-06",
        "restaurant": "rest-1",
    }
    pub_dict: dict[str, VenueDocument] = {
        "pub-1": {
            "name": "The Castle",
            "venueType": "pub",
            "map": "https://maps.example/pub",
        },
        "rest-1": {
            "name": "Bistro Stop",
            "venueType": "restaurant",
        },
    }

    send_ampub_email(
        poll_dict,
        pub_dict,
        emails_src=lambda: [("test@example.com", "uid-1")],
        dummy_run=False,
    )

    assert len(fake_client.sent) == 1
    sent = fake_client.sent[0].kwargs
    assert sent["subject"] == "Pub Night @ The Castle"
    assert "This week on 2026-04-06 we will be visiting The Castle" in sent["text"]
    assert "Before the pub we are meeting at Bistro Stop" in sent["text"]


def test_send_ampub_email_uses_event_template(monkeypatch):
    fake_client = _FakeClient()
    monkeypatch.setattr("firebase_sub.send_email.mailtrap.Mail", _FakeMail)
    monkeypatch.setattr("firebase_sub.send_email.mailtrap.Address", _FakeAddress)
    monkeypatch.setattr("firebase_sub.send_email._mailtrap_client", lambda: fake_client)

    poll_dict: PollDocument = {"selected": "event-1", "date": "2026-04-07"}
    pub_dict: dict[str, VenueDocument] = {
        "event-1": {
            "name": "Beer Festival",
            "venueType": "event",
            "map": "https://maps.example/event",
        }
    }

    send_ampub_email(
        poll_dict,
        pub_dict,
        previously_actioned=True,
        emails_src=lambda: [("test@example.com", None)],
        dummy_run=False,
    )

    assert len(fake_client.sent) == 1
    sent = fake_client.sent[0].kwargs
    assert sent["subject"] == "Pub Night @ RESCHEDULED::Beer Festival"
    assert "destination is an event venue" in sent["text"]
    assert "we will be attending Beer Festival" in sent["text"]
