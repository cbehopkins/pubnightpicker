// @ts-check

import { useState, useEffect, useCallback, useMemo } from "react";
import { arrayUnion, arrayRemove, doc, onSnapshot, runTransaction } from "firebase/firestore";
import { db } from "../firebase";
import { createFirestoreSnapshotErrorHandler } from "../utils/firestoreErrors";

/** @typedef {Record<string, string[]>} VotesMap */

/**
 * @typedef {[
 *   VotesMap,
 *   (venueId: string, userId: string) => Promise<void>,
 *   (venueId: string, userId: string) => Promise<void>
 * ]} UseVotesResult
 */

/**
 * @param {string} pollId
 * @param {boolean} [enabled=true]
 * @returns {UseVotesResult}
 */
function useVotes(pollId, enabled = true) {
  // Votes is a dict from venue ID -> List of users who voted for it
  /** @type {[VotesMap, import("react").Dispatch<import("react").SetStateAction<VotesMap>>]} */
  const [votes, setVotes] = useState({});
  const docRef = useMemo(() => doc(db, "votes", pollId), [pollId]);

  useEffect(() => {
    if (!enabled) {
      setVotes({});
      return undefined;
    }

    const snapshotErrorHandler = createFirestoreSnapshotErrorHandler("Votes");
    return onSnapshot(docRef, (snapshot) => {
      setVotes(/** @type {VotesMap} */(snapshot.data() || {}));
    }, snapshotErrorHandler);
  }, [docRef, enabled]);

  const updateVoteDocument = useCallback(async (venueId, userId, operation) => {
    await runTransaction(db, async (transaction) => {
      const meetRef = doc(db, "polls", pollId);
      const meetSnapshot = await transaction.get(meetRef);
      if (!meetSnapshot.exists()) {
        throw new Error("This Meet no longer exists.");
      }
      transaction.set(docRef, {
        [venueId]: operation === "add" ? arrayUnion(userId) : arrayRemove(userId),
      }, { merge: true });
    });
  }, [docRef, pollId]);

  /** @type {(venueId: string, userId: string) => Promise<void>} */
  const makeVote = useCallback(async (venueId, userId) => {
    await updateVoteDocument(venueId, userId, "add");
  }, [updateVoteDocument]);
  /** @type {(venueId: string, userId: string) => Promise<void>} */
  const clearVote = useCallback(async (venueId, userId) => {
    await updateVoteDocument(venueId, userId, "remove");
  }, [updateVoteDocument]);

  return [votes, makeVote, clearVote];
}
export default useVotes;
