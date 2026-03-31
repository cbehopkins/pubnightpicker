import { useState, useEffect, useCallback } from "react";
import { arrayRemove, arrayUnion, doc, onSnapshot } from "firebase/firestore";
import { db } from "../firebase";
import { createFirestoreSnapshotErrorHandler } from "../utils/firestoreErrors";
import { updateDocWithInitialization } from "../utils/firestoreDocOps";

function useAttendance(pollId) {
    const [attendance, setAttendance] = useState({});
    const docRef = doc(db, "attendance", pollId);

    const updateAttendanceDoc = useCallback(async (payload) => {
        await updateDocWithInitialization(docRef, payload);
    }, [docRef]);

    useEffect(() => {
        const snapshotErrorHandler = createFirestoreSnapshotErrorHandler("Attendance");
        return onSnapshot(docRef, (snapshot) => {
            setAttendance(snapshot.data() || {});
        }, snapshotErrorHandler);
    }, [docRef]);

    const setAttendanceStatus = useCallback(async (pubId, userId, status) => {
        const oppositeStatus = status === "canCome" ? "cannotCome" : "canCome";
        await updateAttendanceDoc({
            [`${pubId}.${status}`]: arrayUnion(userId),
            [`${pubId}.${oppositeStatus}`]: arrayRemove(userId),
        });
    }, [updateAttendanceDoc]);

    const clearAttendance = useCallback(async (pubId, userId) => {
        await updateAttendanceDoc({
            [`${pubId}.canCome`]: arrayRemove(userId),
            [`${pubId}.cannotCome`]: arrayRemove(userId),
        });
    }, [updateAttendanceDoc]);

    const setAttendanceForMultiplePubs = useCallback(async (pubIds, userId, status) => {
        if (!pubIds || pubIds.length === 0) {
            return;
        }

        const oppositeStatus = status === "canCome" ? "cannotCome" : "canCome";
        const payload = {};
        for (const pubId of pubIds) {
            payload[`${pubId}.${status}`] = arrayUnion(userId);
            payload[`${pubId}.${oppositeStatus}`] = arrayRemove(userId);
        }

        await updateAttendanceDoc(payload);
    }, [updateAttendanceDoc]);

    return [attendance, setAttendanceStatus, clearAttendance, setAttendanceForMultiplePubs];
}

export default useAttendance;
