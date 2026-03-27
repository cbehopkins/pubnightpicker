
import { useState, useEffect, useCallback } from "react";
import { onSnapshot } from "firebase/firestore";
import { createFirestoreSnapshotErrorHandler } from "../utils/firestoreErrors";

export default function useQueryDb(q, error_handler = null, precondition = null, enabled = true) {
    const [polls, setPolls] = useState({});
    const addCallback = useCallback((id, poll) => {
        setPolls((prevPolls) => {
            const newValue = precondition ? precondition(poll) : poll
            return { ...prevPolls, [id]: newValue };
        });
    }, [precondition]);
    const modCallback = useCallback((id, poll) => {
        setPolls((prevPolls) => {
            const newValue = precondition ? precondition(poll) : poll
            const nextPolls = { ...prevPolls, [id]: newValue };
            // nextPolls[id] = poll;
            return nextPolls;
        });
    }, [precondition]);
    const rmCallback = useCallback((id) => {
        setPolls((prevPolls) => {
            const nextPolls = { ...prevPolls };
            delete nextPolls[id];
            return nextPolls;
        });
    }, []);
    useEffect(() => {
        if (!enabled) {
            setPolls({});
            return;
        }
        const snapshotErrorHandler = error_handler || createFirestoreSnapshotErrorHandler("Polls data");
        const unsubscribe = onSnapshot(q, (snapshot) => {
            snapshot.docChanges().forEach((change) => {
                if (change.type === "added") {
                    addCallback(change.doc.id, change.doc.data());
                }
                if (change.type === "modified") {
                    modCallback(change.doc.id, change.doc.data());
                }
                if (change.type === "removed") {
                    rmCallback(change.doc.id, change.doc.data());
                }
            });
        }, snapshotErrorHandler);
        return () => {
            unsubscribe()
        };
    }, [enabled, q, error_handler, addCallback, modCallback, rmCallback]);
    return polls
}
