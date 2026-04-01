// @ts-check

import { useState, useEffect, useCallback, useMemo } from "react";
import { arrayUnion, arrayRemove, doc, onSnapshot, updateDoc } from "firebase/firestore";
import { db } from "../firebase";
import { createFirestoreSnapshotErrorHandler } from "../utils/firestoreErrors";

/** @typedef {Record<string, string[]>} VotesMap */

/**
 * @typedef {[
 *   VotesMap,
 *   (pubId: string, userId: string) => Promise<void>,
 *   (pubId: string, userId: string) => Promise<void>
 * ]} UseVotesResult
 */

/**
 * @param {string} pollId
 * @returns {UseVotesResult}
 */
function useVotes(pollId) {
  // Votes is a dict from pub ID -> List of users who voted for it
  /** @type {[VotesMap, import("react").Dispatch<import("react").SetStateAction<VotesMap>>]} */
  const [votes, setVotes] = useState({});
  const docRef = useMemo(() => doc(db, "votes", pollId), [pollId]);

  useEffect(() => {
    const snapshotErrorHandler = createFirestoreSnapshotErrorHandler("Votes");
    return onSnapshot(docRef, (snapshot) => {
      setVotes(/** @type {VotesMap} */(snapshot.data() || {}));
    }, snapshotErrorHandler);
  }, [docRef]);

  /** @type {(pubId: string, userId: string) => Promise<void>} */
  const makeVote = useCallback(async (pubId, userId) => {
    await updateDoc(docRef, {
      [pubId]: arrayUnion(userId)
    })
  }, [docRef]);
  /** @type {(pubId: string, userId: string) => Promise<void>} */
  const clearVote = useCallback(async (pubId, userId) => {
    await updateDoc(docRef, {
      [pubId]: arrayRemove(userId)
    })
  }, [docRef]);

  return [votes, makeVote, clearVote];
}
export default useVotes;
