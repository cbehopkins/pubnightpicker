import { useState, useEffect, useCallback } from "react";
import { arrayUnion, arrayRemove, doc, onSnapshot , updateDoc} from "firebase/firestore";
import { db } from "../firebase";
function isEqualSets(first, second) {
  const a = new Set(first)
  const b = new Set(second)
  if (a === b) return true;
  if (a.size !== b.size) return false;
  for (const value of a) if (!b.has(value)) return false;
  return true;
}

function useVotes(pollId) {
  // Votes is a dict from pub ID -> List of users who voted for it
  const [votes, setVotes] = useState({});
  const docRef = doc(db, "votes", pollId);

  useEffect(() => {
    return onSnapshot(docRef, (doc) => {
      if (!doc.data()){
        // We might not have set up doc data yet...
        return
      }
      // FIXME this gets called a little too frequently...
      for (const [key, value] of Object.entries(doc.data())) {
        if (Object.hasOwn(votes, key) && isEqualSets(votes[key], value)) {
        } else {
          // FIXME - thhis should be reworked to a single call to this function
          setVotes((prevVotes)=>{
            return {
              ...prevVotes,
              [key]:value
            }
          })
        }
      }
    });
  }, [votes, docRef, pollId]);

  const makeVote = useCallback(async (pubId, userId) => {
    await updateDoc(docRef, {
      [pubId]: arrayUnion(userId)
    })
  }, [docRef ]);
  const clearVote = useCallback(async (pubId, userId) => {
    await updateDoc(docRef, {
      [pubId]:arrayRemove(userId)
    })
  }, [docRef]);

  return [votes, makeVote, clearVote];
}
export default useVotes;
