"""Shared helpers for tests that run against Firebase emulators."""

from __future__ import annotations

import firebase_admin
import google.oauth2.credentials


class EmulatorCredentials(firebase_admin.credentials.Base):
    """Stub credential for emulator-only (demo project) usage.

    Avoids ADC look-up so tests can be skipped cleanly rather than
    erroring when the emulator isn't running.
    """

    def get_credential(self) -> google.oauth2.credentials.Credentials:
        return google.oauth2.credentials.Credentials(token="owner")
