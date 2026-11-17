import os

import click
import firebase_admin
import google.oauth2.credentials
from firebase_admin import firestore


def _get_db():
    try:
        app = firebase_admin.get_app()
    except ValueError:
        if os.getenv("FIRESTORE_EMULATOR_HOST"):
            project_id = os.getenv("GOOGLE_CLOUD_PROJECT") or os.getenv(
                "GCLOUD_PROJECT"
            )
            if not project_id:
                raise click.ClickException(
                    "Set GOOGLE_CLOUD_PROJECT when using the Firestore emulator."
                )
            app = firebase_admin.initialize_app(
                credential=google.oauth2.credentials.Credentials(token="owner"),
                options={"projectId": project_id},
            )
        else:
            app = firebase_admin.initialize_app()
    return firestore.client(app=app)


@click.command()
@click.option("--name", required=True, help="Name for the initial public Group.")
def main(name: str) -> None:
    """Create the initial Group and associate all existing Meets with it."""
    if not name.strip():
        raise click.BadParameter("must not be empty", param_hint="--name")

    try:
        db = _get_db()
        existing_groups = list(db.collection("groups").stream())
        if existing_groups:
            raise click.ClickException(
                "Migration refused: the groups collection already contains "
                f"{len(existing_groups)} Group(s); no writes made."
            )
        meets = list(db.collection("polls").stream())
    except click.ClickException:
        raise
    except Exception as exc:
        raise click.ClickException(
            f"Preflight failed; no data was written: {exc}"
        ) from exc

    group_ref = db.collection("groups").document()
    try:
        group_ref.set({"name": name, "visibility": "public"})
    except Exception as exc:
        raise click.ClickException(
            f"Migration failed while creating Group {group_ref.id}; "
            f"0 of {len(meets)} Meets migrated: {exc}"
        ) from exc

    total_meets = len(meets)
    migrated_count = 0
    click.echo(f"Group ID: {group_ref.id}")
    click.echo(f"Created public Group; migrating {total_meets} Meet(s).")
    for meet in meets:
        try:
            meet.reference.update({"groupId": group_ref.id})
        except Exception as exc:
            raise click.ClickException(
                f"Migration failed for Group {group_ref.id}: "
                f"{migrated_count} of {total_meets} Meets migrated; "
                f"failed to update {meet.reference.path}: {exc}"
            ) from exc
        migrated_count += 1
        click.echo(f"Progress: {migrated_count}/{total_meets} Meets migrated.")

    click.echo(
        f"Migration complete: {migrated_count} Meet(s) migrated "
        f"to Group {group_ref.id}."
    )


if __name__ == "__main__":
    main()
