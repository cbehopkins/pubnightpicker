
// @ts-check

import { useState, useEffect, useCallback } from "react";
import { onSnapshot } from "firebase/firestore";
import { createFirestoreSnapshotErrorHandler } from "../utils/firestoreErrors";

/** @typedef {{ code?: string, message?: string }} FirestoreLikeError */

/**
 * Subscribe to a Firestore query and expose a mutable id->value map.
 *
 * @template {Record<string, unknown>} TDoc
 * @template TValue
 * @param {import("firebase/firestore").Query<import("firebase/firestore").DocumentData>} q
 * @param {((error: FirestoreLikeError) => void) | null} [error_handler]
 * @param {((doc: TDoc) => TValue) | null} [precondition]
 * @param {boolean} [enabled]
 * @returns {Record<string, TValue | TDoc>}
 */
export default function useQueryDb(q, error_handler = null, precondition = null, enabled = true) {
    /** @type {[Record<string, TValue | TDoc>, import("react").Dispatch<import("react").SetStateAction<Record<string, TValue | TDoc>>>]} */
    const [polls, setPolls] = useState({});
    /** @type {(id: string, poll: TDoc) => void} */
    const addCallback = useCallback((id, poll) => {
        setPolls((prevPolls) => {
            const newValue = precondition ? precondition(poll) : poll
            return { ...prevPolls, [id]: newValue };
        });
    }, [precondition]);
    /** @type {(id: string, poll: TDoc) => void} */
    const modCallback = useCallback((id, poll) => {
        setPolls((prevPolls) => {
            const newValue = precondition ? precondition(poll) : poll
            const nextPolls = { ...prevPolls, [id]: newValue };
            // nextPolls[id] = poll;
            return nextPolls;
        });
    }, [precondition]);
    /** @type {(id: string) => void} */
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
                const docData = /** @type {TDoc} */ (change.doc.data());
                if (change.type === "added") {
                    addCallback(change.doc.id, docData);
                }
                if (change.type === "modified") {
                    modCallback(change.doc.id, docData);
                }
                if (change.type === "removed") {
                    rmCallback(change.doc.id);
                }
            });
        }, snapshotErrorHandler);
        return () => {
            unsubscribe()
        };
    }, [enabled, q, error_handler, addCallback, modCallback, rmCallback]);
    return polls
}
