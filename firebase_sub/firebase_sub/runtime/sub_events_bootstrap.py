import os
from functools import cache
from pathlib import Path

import firebase_admin
from firebase_admin import credentials

from firebase_sub.database.handlers import DbHandler

CWD = Path(__file__).resolve().parent


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


CRED_PATH = _resolve_cred_path()


def _ensure_firebase_app() -> None:
    try:
        firebase_admin.get_app()
    except ValueError:
        cred = credentials.Certificate(str(CRED_PATH))
        firebase_admin.initialize_app(cred)


@cache
def _get_cached_db_handler() -> DbHandler:
    _ensure_firebase_app()
    return DbHandler()


def get_db_handler() -> DbHandler:
    return _get_cached_db_handler()
