import importlib
import sys
from unittest.mock import Mock


def _import_fresh(module_name: str):
    sys.modules.pop(module_name, None)
    return importlib.import_module(module_name)


def test_sub_events_import_has_no_firebase_side_effects(monkeypatch):
    init_mock = Mock()
    cert_mock = Mock()

    import firebase_admin
    import firebase_admin.credentials

    monkeypatch.setattr(firebase_admin, "initialize_app", init_mock)
    monkeypatch.setattr(firebase_admin.credentials, "Certificate", cert_mock)

    _import_fresh("firebase_sub.cli.sub_events")

    init_mock.assert_not_called()
    cert_mock.assert_not_called()


def test_backup_import_has_no_firebase_side_effects(monkeypatch):
    init_mock = Mock()
    cert_mock = Mock()

    import firebase_admin
    import firebase_admin.credentials

    monkeypatch.setattr(firebase_admin, "initialize_app", init_mock)
    monkeypatch.setattr(firebase_admin.credentials, "Certificate", cert_mock)

    _import_fresh("firebase_sub.cli.backup")

    init_mock.assert_not_called()
    cert_mock.assert_not_called()


def test_restore_import_has_no_firebase_side_effects(monkeypatch):
    init_mock = Mock()
    cert_mock = Mock()

    import firebase_admin
    import firebase_admin.credentials

    monkeypatch.setattr(firebase_admin, "initialize_app", init_mock)
    monkeypatch.setattr(firebase_admin.credentials, "Certificate", cert_mock)

    _import_fresh("firebase_sub.cli.restore")

    init_mock.assert_not_called()
    cert_mock.assert_not_called()


def test_bootstrap_import_has_no_firebase_side_effects(monkeypatch):
    init_mock = Mock()
    cert_mock = Mock()

    import firebase_admin
    import firebase_admin.credentials

    monkeypatch.setattr(firebase_admin, "initialize_app", init_mock)
    monkeypatch.setattr(firebase_admin.credentials, "Certificate", cert_mock)

    _import_fresh("firebase_sub.cli.bootstrap")

    init_mock.assert_not_called()
    cert_mock.assert_not_called()


def test_sub_events_get_db_handler_initializes_once(monkeypatch):
    module = _import_fresh("firebase_sub.cli.sub_events")

    cert_mock = Mock(return_value=object())
    init_mock = Mock(return_value=object())
    get_app_mock = Mock(side_effect=ValueError("no app"))

    monkeypatch.setattr(module.credentials, "Certificate", cert_mock)
    monkeypatch.setattr(module.firebase_admin, "initialize_app", init_mock)
    monkeypatch.setattr(module.firebase_admin, "get_app", get_app_mock)

    class FakeDbHandler:
        def __init__(self):
            self.db = object()

    monkeypatch.setattr(module, "DbHandler", FakeDbHandler)

    first = module._get_db_handler()
    second = module._get_db_handler()

    assert isinstance(first, FakeDbHandler)
    assert first is second
    cert_mock.assert_called_once()
    init_mock.assert_called_once()


def test_restore_get_db_initializes_once(monkeypatch):
    module = _import_fresh("firebase_sub.cli.restore")

    cert_mock = Mock(return_value=object())
    init_mock = Mock(return_value=object())
    get_app_mock = Mock(side_effect=ValueError("no app"))
    client_mock = Mock(return_value=object())

    monkeypatch.setattr(module.credentials, "Certificate", cert_mock)
    monkeypatch.setattr(module.firebase_admin, "initialize_app", init_mock)
    monkeypatch.setattr(module.firebase_admin, "get_app", get_app_mock)
    monkeypatch.setattr(module.firestore, "client", client_mock)

    first = module._get_db()
    second = module._get_db()

    assert first is second
    cert_mock.assert_called_once()
    init_mock.assert_called_once()
    client_mock.assert_called_once()
