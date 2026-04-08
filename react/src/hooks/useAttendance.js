// @ts-check

import { useState, useEffect, useCallback, useMemo } from "react";
import { arrayRemove, arrayUnion, doc, onSnapshot } from "firebase/firestore";
import { db } from "../firebase";
import { createFirestoreSnapshotErrorHandler } from "../utils/firestoreErrors";
import { updateDocWithInitialization } from "../utils/firestoreDocOps";
import { ATTENDANCE_GLOBAL_KEY } from "../utils/attendanceState";

/**
 * @typedef {Object} AttendanceEntry
 * @property {string[]=} canCome
 * @property {string[]=} cannotCome
 */

/** @typedef {Record<string, AttendanceEntry | undefined>} AttendanceMap */
/** @typedef {"canCome" | "cannotCome"} AttendanceStatus */

/**
 * @typedef {[ 
 *  AttendanceMap,
 *  (pubId: string, userId: string, status: AttendanceStatus) => Promise<void>,
 *  (pubId: string, userId: string) => Promise<void>,
 *  (pubIds: string[] | null | undefined, userId: string, status: AttendanceStatus) => Promise<void>,
 *  (pubIds: string[] | null | undefined, userId: string, status: AttendanceStatus) => Promise<void>
 * ]} UseAttendanceResult
 */

/**
 * @param {string} pollId
 * @param {boolean} [enabled=true]
 * @returns {UseAttendanceResult}
 */
function useAttendance(pollId, enabled = true) {
    /** @type {[AttendanceMap, import("react").Dispatch<import("react").SetStateAction<AttendanceMap>>]} */
    const [attendance, setAttendance] = useState({});
    const docRef = useMemo(() => doc(db, "attendance", pollId), [pollId]);

    /** @type {(payload: Record<string, unknown>) => Promise<void>} */
    const updateAttendanceDoc = useCallback(async (payload) => {
        await updateDocWithInitialization(docRef, payload);
    }, [docRef]);

    useEffect(() => {
        if (!enabled) {
            setAttendance({});
            return undefined;
        }

        const snapshotErrorHandler = createFirestoreSnapshotErrorHandler("Attendance");
        return onSnapshot(docRef, (snapshot) => {
            setAttendance(snapshot.data() || {});
        }, snapshotErrorHandler);
    }, [docRef, enabled]);

    /** @type {(pubId: string, userId: string, status: AttendanceStatus) => Promise<void>} */
    const setAttendanceStatus = useCallback(async (pubId, userId, status) => {
        const oppositeStatus = status === "canCome" ? "cannotCome" : "canCome";
        await updateAttendanceDoc({
            [`${pubId}.${status}`]: arrayUnion(userId),
            [`${pubId}.${oppositeStatus}`]: arrayRemove(userId),
        });
    }, [updateAttendanceDoc]);

    /** @type {(pubId: string, userId: string) => Promise<void>} */
    const clearAttendance = useCallback(async (pubId, userId) => {
        await updateAttendanceDoc({
            [`${pubId}.canCome`]: arrayRemove(userId),
            [`${pubId}.cannotCome`]: arrayRemove(userId),
        });
    }, [updateAttendanceDoc]);

    /** @type {(pubIds: string[] | null | undefined, userId: string, status: AttendanceStatus) => Promise<void>} */
    const setAttendanceForMultiplePubs = useCallback(async (pubIds, userId, status) => {
        if (!pubIds || pubIds.length === 0) {
            return;
        }

        const oppositeStatus = status === "canCome" ? "cannotCome" : "canCome";
        /** @type {Record<string, unknown>} */
        const payload = {};
        for (const pubId of pubIds) {
            payload[`${pubId}.${status}`] = arrayUnion(userId);
            payload[`${pubId}.${oppositeStatus}`] = arrayRemove(userId);
        }

        await updateAttendanceDoc(payload);
    }, [updateAttendanceDoc]);

    /** @type {(pubIds: string[] | null | undefined, userId: string, status: AttendanceStatus) => Promise<void>} */
    const setGlobalAttendanceStatus = useCallback(async (pubIds, userId, status) => {
        const oppositeStatus = status === "canCome" ? "cannotCome" : "canCome";
        /** @type {Record<string, unknown>} */
        const payload = {
            [`${ATTENDANCE_GLOBAL_KEY}.${status}`]: arrayUnion(userId),
            [`${ATTENDANCE_GLOBAL_KEY}.${oppositeStatus}`]: arrayRemove(userId),
        };

        for (const pubId of pubIds || []) {
            payload[`${pubId}.${status}`] = arrayUnion(userId);
            payload[`${pubId}.${oppositeStatus}`] = arrayRemove(userId);
        }

        await updateAttendanceDoc(payload);
    }, [updateAttendanceDoc]);

    return [
        attendance,
        setAttendanceStatus,
        clearAttendance,
        setAttendanceForMultiplePubs,
        setGlobalAttendanceStatus,
    ];
}

export default useAttendance;
