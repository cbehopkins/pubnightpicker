import os
from pathlib import Path

import firebase_admin
from firebase_admin import credentials

from firebase_sub.database.handlers import DbHandler

CWD = Path(__file__).resolve().parent

CRED_PATH = Path(".")
_FIREBASE_APP_INITIALIZED = False
_DB_HANDLER: DbHandler | None = None


def _resolve_cred_path() -> Path:
    env_path = os.getenv("FIREBASE_CRED_PATH")
    if env_path:
        return Path(env_path)

    cwd_path = Path.cwd() / "cred.json"
    if cwd_path.exists():
        return cwd_path

    source_tree_path = CWD.parent.parent / "cred.json"
    if source_tree_path.exists():
        return source_tree_path

    # Prefer a predictable default for packaged runtime environments.
    return cwd_path


def _ensure_firebase_app() -> None:
    global _FIREBASE_APP_INITIALIZED
    if _FIREBASE_APP_INITIALIZED:
        return
    try:
        firebase_admin.get_app()
    except ValueError:
        cred = credentials.Certificate(CRED_PATH)
        firebase_admin.initialize_app(cred)
    _FIREBASE_APP_INITIALIZED = True


def get_db_handler() -> DbHandler:
    global _DB_HANDLER
    if _DB_HANDLER is None:
        _ensure_firebase_app()
        _DB_HANDLER = DbHandler()
    return _DB_HANDLER


CRED_PATH = _resolve_cred_path()
