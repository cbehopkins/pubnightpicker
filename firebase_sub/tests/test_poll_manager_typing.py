from collections.abc import Sequence
from inspect import signature
from typing import get_args, get_origin

from google.cloud.firestore_v1.base_document import DocumentSnapshot

from firebase_sub.database.poll_manager import PollManager


def test_poll_updater_signature_has_sequence_snapshot_and_none_return():
    sig = signature(PollManager._poll_updater)
    doc_param = sig.parameters["doc_snapshot"]
    doc_annotation = doc_param.annotation

    assert get_origin(doc_annotation) is Sequence
    assert get_args(doc_annotation) == (DocumentSnapshot,)
    assert sig.return_annotation is None
