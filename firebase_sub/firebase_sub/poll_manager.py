import logging

_log = logging.getLogger("PollManager")


class PollManager:
    def __init__(self, query, add=None, modify=None, rm=None):
        self.add = add
        self.modify = modify
        self.rm = rm
        self.query = query

    def __enter__(self):
        self.unsubscribe = self.query.on_snapshot(self._poll_updater)

    def __exit__(self, exc_type, exc_val, exc_tb):
        self.unsubscribe()

    def _poll_updater(self, doc_snapshot, changes, read_time):
        for change in changes:
            if change.type.name == "ADDED":
                if self.add is None:
                    _log.info(f"New {change.document.id}")
                else:
                    self.add(change.document)
            elif change.type.name == "MODIFIED":
                if self.modify is None:
                    print(f"Modified : {change.document.id}")
                else:
                    self.modify(change.document)
            elif change.type.name == "REMOVED":
                if self.rm is None:
                    print(f"Removed : {change.document.id}")
                else:
                    self.rm(change.document)
