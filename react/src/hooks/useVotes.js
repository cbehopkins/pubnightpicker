import { useState, useEffect, useCallback, useMemo } from "react";
import { arrayUnion, arrayRemove, doc, onSnapshot, updateDoc } from "firebase/firestore";
import { db } from "../firebase";
import { createFirestoreSnapshotErrorHandler } from "../utils/firestoreErrors";

function useVotes(pollId) {
  // Votes is a dict from pub ID -> List of users who voted for it
  const [votes, setVotes] = useState({});
  const docRef = useMemo(() => doc(db, "votes", pollId), [pollId]);

  useEffect(() => {
    const snapshotErrorHandler = createFirestoreSnapshotErrorHandler("Votes");
    return onSnapshot(docRef, (snapshot) => {
      setVotes(snapshot.data() || {});
    }, snapshotErrorHandler);
  }, [docRef]);

  const makeVote = useCallback(async (pubId, userId) => {
    await updateDoc(docRef, {
      [pubId]: arrayUnion(userId)
    })
  }, [docRef]);
  const clearVote = useCallback(async (pubId, userId) => {
    await updateDoc(docRef, {
      [pubId]: arrayRemove(userId)
    })
  }, [docRef]);

  return [votes, makeVote, clearVote];
}
export default useVotes;
