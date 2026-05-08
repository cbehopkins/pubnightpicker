// @ts-check

import { useState, useEffect, useCallback, useMemo } from "react";
import { arrayRemove, arrayUnion, deleteField, doc, onSnapshot } from "firebase/firestore";
import { db } from "../firebase";
import { createFirestoreSnapshotErrorHandler } from "../utils/firestoreErrors";
import { updateDocWithInitialization } from "../utils/firestoreDocOps";
import { ATTENDANCE_GLOBAL_KEY } from "../utils/attendanceState";

/**
 * @typedef {Object} AttendanceEntry
 * @property {string[]=} canCome
 * @property {string[]=} cannotCome
 * @property {Record<string, string>=} eta
 */

/** @typedef {Record<string, AttendanceEntry | undefined>} AttendanceMap */
/** @typedef {"canCome" | "cannotCome"} AttendanceStatus */

/**
 * @typedef {[ 
 *  AttendanceMap,
 *  (pubId: string, userId: string, status: AttendanceStatus) => Promise<void>,
 *  (pubId: string, userId: string) => Promise<void>,
 *  (pubIds: string[] | null | undefined, userId: string, status: AttendanceStatus) => Promise<void>,
 *  (pubIds: string[] | null | undefined, userId: string, status: AttendanceStatus) => Promise<void>,
 *  (pubId: string, userId: string, eta: string) => Promise<void>,
 *  (pubId: string, userId: string) => Promise<void>
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
        /** @type {Record<string, unknown>} */
        const payload = {
            [`${pubId}.${status}`]: arrayUnion(userId),
            [`${pubId}.${oppositeStatus}`]: arrayRemove(userId),
        };
        // ETA only makes sense alongside canCome; clear it when switching to cannotCome
        if (status === "cannotCome") {
            payload[`${pubId}.eta.${userId}`] = deleteField();
        }
        await updateAttendanceDoc(payload);
    }, [updateAttendanceDoc]);

    /** @type {(pubId: string, userId: string) => Promise<void>} */
    const clearAttendance = useCallback(async (pubId, userId) => {
        await updateAttendanceDoc({
            [`${pubId}.canCome`]: arrayRemove(userId),
            [`${pubId}.cannotCome`]: arrayRemove(userId),
            [`${pubId}.eta.${userId}`]: deleteField(),
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

    /** @type {(pubId: string, userId: string, eta: string) => Promise<void>} */
    const setEta = useCallback(async (pubId, userId, eta) => {
        await updateAttendanceDoc({
            [`${pubId}.eta.${userId}`]: eta,
        });
    }, [updateAttendanceDoc]);

    /** @type {(pubId: string, userId: string) => Promise<void>} */
    const clearEta = useCallback(async (pubId, userId) => {
        await updateAttendanceDoc({
            [`${pubId}.eta.${userId}`]: deleteField(),
        });
    }, [updateAttendanceDoc]);

    return [
        attendance,
        setAttendanceStatus,
        clearAttendance,
        setAttendanceForMultiplePubs,
        setGlobalAttendanceStatus,
        setEta,
        clearEta,
    ];
}

export default useAttendance;
