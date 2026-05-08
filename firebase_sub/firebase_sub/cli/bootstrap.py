import os
from dataclasses import dataclass
from pathlib import Path
from typing import Any

import click
import firebase_admin
import google.oauth2.credentials
from firebase_admin import auth as firebase_auth
from firebase_admin import credentials, firestore
from google.cloud.firestore import SERVER_TIMESTAMP
from google.cloud.firestore_v1.client import Client

from firebase_sub.common.logging import configure_logging, log_level_to_int

CWD = Path(__file__).resolve().parent
CRED_PATH = CWD.parent.parent / "cred.json"
_FIREBASE_APP_INITIALIZED = False
_DB = None

ADMIN_DEFAULT_ROLES = [
    "canChat",
    "canAddPubToPoll",
    "canCreatePoll",
    "canCompletePoll",
    "canManagePubs",
    "canShowVoters",
    "canDeleteAnyMessage",
]

# Deterministic UIDs / credentials used by the smoke seed dataset and integration tests.
SMOKE_ADMIN_UID = "smoke-admin"
SMOKE_USER_A_UID = "smoke-user-a"  # chat message sender; no push endpoint
SMOKE_USER_B_UID = "smoke-user-b"  # chat message recipient; has active push endpoint
SMOKE_USER_B_ENDPOINT_ID = "smoke-endpoint-b"

# Auth credentials for the smoke users (emulator only).
SMOKE_ADMIN_EMAIL = "smoke-admin@test.local"
SMOKE_ADMIN_PASSWORD = "test-password-admin"
SMOKE_USER_A_EMAIL = "smoke-user-a@test.local"
SMOKE_USER_A_PASSWORD = "test-password-a"
SMOKE_USER_B_EMAIL = "smoke-user-b@test.local"
SMOKE_USER_B_PASSWORD = "test-password-b"


@dataclass(frozen=True)
class RoleGrantResult:
    role: str
    already_granted: bool


@dataclass(frozen=True)
class SeedResult:
    wrote_docs: list[str]
    skipped_docs: list[str]


def _in_emulator_mode() -> bool:
    return bool(os.getenv("FIRESTORE_EMULATOR_HOST"))


def _ensure_firebase_app() -> None:
    global _FIREBASE_APP_INITIALIZED
    if _FIREBASE_APP_INITIALIZED:
        return

    try:
        firebase_admin.get_app()
    except ValueError:
        if _in_emulator_mode():
            project_id = os.getenv("GOOGLE_CLOUD_PROJECT", "demo-bootstrap")
            firebase_admin.initialize_app(
                credential=google.oauth2.credentials.Credentials(token="owner"),
                options={"projectId": project_id},
            )
        else:
            cred = credentials.Certificate(CRED_PATH)
            firebase_admin.initialize_app(cred)

    _FIREBASE_APP_INITIALIZED = True


def _get_db():
    global _DB
    if _DB is None:
        _ensure_firebase_app()
        _DB = firestore.client()
    return _DB


def _upsert_missing_fields(doc_ref, defaults: dict, dry_run: bool) -> tuple[bool, dict]:
    existing = doc_ref.get().to_dict() or {}
    missing = {key: value for key, value in defaults.items() if key not in existing}
    if not missing:
        return False, {}
    if not dry_run:
        doc_ref.set(missing, merge=True)
    return True, missing


def _grant_role(db, role: str, uid: str, dry_run: bool) -> RoleGrantResult:
    role_ref = db.collection("roles").document(role)
    current = role_ref.get().to_dict() or {}
    already_granted = bool(current.get(uid))
    if not already_granted and not dry_run:
        role_ref.set({uid: True}, merge=True)
    return RoleGrantResult(role=role, already_granted=already_granted)


# ---------------------------------------------------------------------------
# Public seeding helpers – callable from integration tests without going
# through the Click CLI.
# ---------------------------------------------------------------------------


def _create_auth_user_if_missing(
    uid: str, email: str, password: str, display_name: str
) -> bool:
    """Create an Auth user in the emulator if they don't already exist.

    Only runs when ``FIREBASE_AUTH_EMULATOR_HOST`` is set — safe to call from
    ``seed_smoke_data`` without risk of touching real Auth in production.
    Returns True when the user was created, False when it already existed.
    """
    if not os.getenv("FIREBASE_AUTH_EMULATOR_HOST"):
        return False
    try:
        firebase_auth.create_user(
            uid=uid,
            email=email,
            password=password,
            display_name=display_name,
        )
        return True
    except (firebase_auth.UidAlreadyExistsError, firebase_auth.EmailAlreadyExistsError):
        return False


def _set_doc_if_missing(
    db: Client, collection: str, doc_id: str, data: dict[str, Any], dry_run: bool
) -> bool:
    """Write *data* only when the document does not exist yet. Returns True when a write occurred."""
    ref = db.collection(collection).document(doc_id)
    if ref.get().to_dict() is not None:
        return False
    if not dry_run:
        ref.set(data)
    return True


def seed_smoke_data(db: Client, *, dry_run: bool = False) -> SeedResult:
    """Seed a minimal, deterministic dataset for smoke integration tests.

    Creates three users (one admin, one plain user, one push-enabled recipient)
    and the matching role grants.  All writes are idempotent – if the documents
    already exist they are left unchanged.
    """
    wrote: list[str] = []
    skipped: list[str] = []

    def _record(path: str, written: bool) -> None:
        (wrote if written else skipped).append(path)

    # Admin user ---------------------------------------------------------------
    admin_private: dict[str, Any] = {
        "uid": SMOKE_ADMIN_UID,
        "name": "Smoke Admin",
        "notificationEmail": "",
        "notificationEmailEnabled": False,
        "openPollEmailEnabled": False,
        "votesVisible": True,
        "webPushEnabled": False,
        "pushPreferences": {
            "pollOpens": True,
            "pollCompletes": True,
            "globalChat": False,
            "eventChat": False,
        },
    }
    admin_public: dict[str, Any] = {
        "uid": SMOKE_ADMIN_UID,
        "name": "Smoke Admin",
        "photoUrl": None,
        "votesVisible": True,
    }
    _record(
        f"users/{SMOKE_ADMIN_UID}",
        _set_doc_if_missing(db, "users", SMOKE_ADMIN_UID, admin_private, dry_run),
    )
    _record(
        f"user-public/{SMOKE_ADMIN_UID}",
        _set_doc_if_missing(db, "user-public", SMOKE_ADMIN_UID, admin_public, dry_run),
    )

    for role in ["admin", *ADMIN_DEFAULT_ROLES]:
        result = _grant_role(db, role=role, uid=SMOKE_ADMIN_UID, dry_run=dry_run)
        _record(f"roles/{role} → {SMOKE_ADMIN_UID}", not result.already_granted)

    # User A (sender, has canChat, no push endpoint) ---------------------------
    user_a_private: dict[str, Any] = {
        "uid": SMOKE_USER_A_UID,
        "name": "Smoke User A",
        "webPushEnabled": False,
        "pushPreferences": {
            "pollOpens": False,
            "pollCompletes": False,
            "globalChat": False,
            "eventChat": False,
        },
    }
    user_a_public: dict[str, Any] = {
        "uid": SMOKE_USER_A_UID,
        "name": "Smoke User A",
        "photoUrl": None,
        "votesVisible": False,
    }
    _record(
        f"users/{SMOKE_USER_A_UID}",
        _set_doc_if_missing(db, "users", SMOKE_USER_A_UID, user_a_private, dry_run),
    )
    _record(
        f"user-public/{SMOKE_USER_A_UID}",
        _set_doc_if_missing(
            db, "user-public", SMOKE_USER_A_UID, user_a_public, dry_run
        ),
    )
    _record(
        f"roles/canChat → {SMOKE_USER_A_UID}",
        not _grant_role(db, "canChat", SMOKE_USER_A_UID, dry_run).already_granted,
    )

    # User B (recipient, push-enabled, global chat opted in) -------------------
    user_b_private: dict[str, Any] = {
        "uid": SMOKE_USER_B_UID,
        "name": "Smoke User B",
        "webPushEnabled": True,
        "pushPreferences": {
            "pollOpens": False,
            "pollCompletes": False,
            "globalChat": True,
            "eventChat": False,
        },
    }
    user_b_public: dict[str, Any] = {
        "uid": SMOKE_USER_B_UID,
        "name": "Smoke User B",
        "photoUrl": None,
        "votesVisible": False,
    }
    _record(
        f"users/{SMOKE_USER_B_UID}",
        _set_doc_if_missing(db, "users", SMOKE_USER_B_UID, user_b_private, dry_run),
    )
    _record(
        f"user-public/{SMOKE_USER_B_UID}",
        _set_doc_if_missing(
            db, "user-public", SMOKE_USER_B_UID, user_b_public, dry_run
        ),
    )
    _record(
        f"roles/canChat → {SMOKE_USER_B_UID}",
        not _grant_role(db, "canChat", SMOKE_USER_B_UID, dry_run).already_granted,
    )

    # User B push endpoint (stub values; real push not used in tests) ----------
    endpoint_ref = (
        db.collection("users")
        .document(SMOKE_USER_B_UID)
        .collection("push_endpoints")
        .document(SMOKE_USER_B_ENDPOINT_ID)
    )
    ep_key = f"users/{SMOKE_USER_B_UID}/push_endpoints/{SMOKE_USER_B_ENDPOINT_ID}"
    if endpoint_ref.get().to_dict() is not None:
        skipped.append(ep_key)
    else:
        if not dry_run:
            endpoint_ref.set(
                {
                    "endpoint": "https://test-push.example.com/endpoint-b",
                    "p256dh": "test-p256dh-key",
                    "auth": "test-auth-key",
                    "active": True,
                }
            )
        wrote.append(ep_key)

    # Auth users (emulator only) --------------------------------------------
    # Creates the Firebase Auth record so test clients can sign in with
    # email/password.  Only runs when FIREBASE_AUTH_EMULATOR_HOST is set so
    # this is a no-op in unit tests and against real projects.
    if not dry_run and os.getenv("FIREBASE_AUTH_EMULATOR_HOST"):
        for uid, email, password, display_name in [
            (SMOKE_ADMIN_UID, SMOKE_ADMIN_EMAIL, SMOKE_ADMIN_PASSWORD, "Smoke Admin"),
            (
                SMOKE_USER_A_UID,
                SMOKE_USER_A_EMAIL,
                SMOKE_USER_A_PASSWORD,
                "Smoke User A",
            ),
            (
                SMOKE_USER_B_UID,
                SMOKE_USER_B_EMAIL,
                SMOKE_USER_B_PASSWORD,
                "Smoke User B",
            ),
        ]:
            created = _create_auth_user_if_missing(uid, email, password, display_name)
            _record(f"auth/{uid}", created)

    return SeedResult(wrote_docs=wrote, skipped_docs=skipped)


# ---------------------------------------------------------------------------
# Click commands
# ---------------------------------------------------------------------------


@click.group()
def cli() -> None:
    """Database bootstrap helpers for local setup and test seeding."""


@cli.command("create-admin")
@click.option("--uid", required=True, help="Firebase Auth UID for the admin user")
@click.option("--name", default="Admin User", show_default=True)
@click.option(
    "--email", default="", show_default=False, help="Optional notification email"
)
@click.option("--dry-run", is_flag=True, help="Print changes without writing them")
@click.option(
    "--loglevel",
    default="INFO",
    callback=lambda ctx, param, value: log_level_to_int(value),
    show_default=True,
)
def create_admin(uid: str, name: str, email: str, dry_run: bool, loglevel: int) -> None:
    """Create or update baseline admin user documents and grant admin roles."""
    configure_logging(loglevel, None)
    db = _get_db()

    private_ref = db.collection("users").document(uid)
    public_ref = db.collection("user-public").document(uid)

    private_defaults = {
        "uid": uid,
        "name": name,
        "notificationEmail": email,
        "notificationEmailEnabled": False,
        "openPollEmailEnabled": False,
        "votesVisible": True,
        "webPushEnabled": False,
        "pushPreferences": {
            "pollOpens": True,
            "pollCompletes": True,
            "globalChat": False,
            "eventChat": False,
        },
        "bootstrapUpdatedAt": SERVER_TIMESTAMP,
    }
    public_defaults = {
        "uid": uid,
        "name": name,
        "photoUrl": None,
        "votesVisible": True,
    }

    private_changed, private_payload = _upsert_missing_fields(
        private_ref, private_defaults, dry_run
    )
    public_changed, public_payload = _upsert_missing_fields(
        public_ref, public_defaults, dry_run
    )

    if private_changed:
        click.echo(
            f"users/{uid}: {'would set' if dry_run else 'set'} missing fields {sorted(private_payload.keys())}"
        )
    else:
        click.echo(f"users/{uid}: already has required baseline fields")

    if public_changed:
        click.echo(
            f"user-public/{uid}: {'would set' if dry_run else 'set'} missing fields {sorted(public_payload.keys())}"
        )
    else:
        click.echo(f"user-public/{uid}: already has required baseline fields")

    roles_to_grant = ["admin", *ADMIN_DEFAULT_ROLES]
    for role in roles_to_grant:
        result = _grant_role(db, role=role, uid=uid, dry_run=dry_run)
        if result.already_granted:
            click.echo(f"roles/{role}: {uid} already granted")
        else:
            click.echo(f"roles/{role}: {'would grant' if dry_run else 'granted'} {uid}")


@cli.command("grant-role")
@click.option("--uid", required=True, help="Firebase Auth UID")
@click.option("--role", "roles", required=True, multiple=True, help="Role doc name")
@click.option("--dry-run", is_flag=True, help="Print changes without writing them")
@click.option(
    "--loglevel",
    default="INFO",
    callback=lambda ctx, param, value: log_level_to_int(value),
    show_default=True,
)
def grant_role(uid: str, roles: tuple[str, ...], dry_run: bool, loglevel: int) -> None:
    """Grant one or more role memberships in roles/{role}."""
    configure_logging(loglevel, None)
    db = _get_db()

    deduped_roles = sorted({role.strip() for role in roles if role.strip()})
    if not deduped_roles:
        raise click.UsageError("At least one non-empty --role is required")

    for role in deduped_roles:
        result = _grant_role(db, role=role, uid=uid, dry_run=dry_run)
        if result.already_granted:
            click.echo(f"roles/{role}: {uid} already granted")
        else:
            click.echo(f"roles/{role}: {'would grant' if dry_run else 'granted'} {uid}")


@cli.command("seed-smoke")
@click.option("--dry-run", is_flag=True, help="Print changes without writing them")
@click.option(
    "--loglevel",
    default="INFO",
    callback=lambda ctx, param, value: log_level_to_int(value),
    show_default=True,
)
def seed_smoke(dry_run: bool, loglevel: int) -> None:
    """Seed deterministic test users for smoke and integration tests.

    Creates smoke-admin, smoke-user-a (sender), and smoke-user-b (push-enabled
    recipient). All writes are idempotent — existing documents are left unchanged.
    """
    configure_logging(loglevel, None)
    db = _get_db()

    result = seed_smoke_data(db, dry_run=dry_run)

    for path in result.wrote_docs:
        click.echo(f"{'[dry-run] would write' if dry_run else 'wrote'}: {path}")
    for path in result.skipped_docs:
        click.echo(f"skipped (already exists): {path}")

    click.echo(
        f"\nDone: {len(result.wrote_docs)} {'would be written' if dry_run else 'written'}, "
        f"{len(result.skipped_docs)} skipped."
    )


if __name__ == "__main__":
    cli()
