import { useState, useEffect, useCallback } from "react";
import { arrayRemove, arrayUnion, doc, onSnapshot, updateDoc } from "firebase/firestore";
import { db } from "../firebase";

function useAttendance(pollId) {
  const [attendance, setAttendance] = useState({});
  const docRef = doc(db, "attendance", pollId);

  useEffect(() => {
    return onSnapshot(docRef, (snapshot) => {
      setAttendance(snapshot.data() || {});
    });
  }, [docRef]);

  const setAttendanceStatus = useCallback(async (pubId, userId, status) => {
    const oppositeStatus = status === "canCome" ? "cannotCome" : "canCome";
    await updateDoc(docRef, {
      [`${pubId}.${status}`]: arrayUnion(userId),
      [`${pubId}.${oppositeStatus}`]: arrayRemove(userId),
    });
  }, [docRef]);

  const clearAttendance = useCallback(async (pubId, userId) => {
    await updateDoc(docRef, {
      [`${pubId}.canCome`]: arrayRemove(userId),
      [`${pubId}.cannotCome`]: arrayRemove(userId),
    });
  }, [docRef]);

  return [attendance, setAttendanceStatus, clearAttendance];
}

export default useAttendance;