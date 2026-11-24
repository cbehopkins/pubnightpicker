// @ts-check

import { useState, useEffect, useCallback, useMemo } from "react";
import { arrayRemove, arrayUnion, deleteField, doc, onSnapshot, runTransaction } from "firebase/firestore";
import { db } from "../firebase";
import { createFirestoreSnapshotErrorHandler } from "../utils/firestoreErrors";
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
 *  (venueId: string, userId: string, status: AttendanceStatus) => Promise<void>,
 *  (venueId: string, userId: string) => Promise<void>,
 *  (venueIds: string[] | null | undefined, userId: string, status: AttendanceStatus) => Promise<void>,
 *  (venueIds: string[] | null | undefined, userId: string, status: AttendanceStatus) => Promise<void>,
 *  (venueId: string, userId: string, eta: string) => Promise<void>,
 *  (venueId: string, userId: string) => Promise<void>
 * ]} UseAttendanceResult
 */

/**
 * @param {string} meetId
 * @param {boolean} [enabled=true]
 * @returns {UseAttendanceResult}
 */
function useAttendance(meetId, enabled = true) {
    /** @type {[AttendanceMap, import("react").Dispatch<import("react").SetStateAction<AttendanceMap>>]} */
    const [attendance, setAttendance] = useState({});
    const docRef = useMemo(() => doc(db, "attendance", meetId), [meetId]);

    /** @type {(payload: Record<string, unknown>) => Promise<void>} */
    const updateAttendanceDoc = useCallback(async (payload) => {
        await runTransaction(db, async (transaction) => {
            const meetRef = doc(db, "polls", meetId);
            const meetSnapshot = await transaction.get(meetRef);
            if (!meetSnapshot.exists()) {
                throw new Error("This Meet no longer exists.");
            }
            transaction.set(docRef, payload, { merge: true });
        });
    }, [docRef, meetId]);

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

    /** @type {(venueId: string, userId: string, status: AttendanceStatus) => Promise<void>} */
    const setAttendanceStatus = useCallback(async (venueId, userId, status) => {
        const oppositeStatus = status === "canCome" ? "cannotCome" : "canCome";
        /** @type {Record<string, unknown>} */
        const venueAttendance = {
            [status]: arrayUnion(userId),
            [oppositeStatus]: arrayRemove(userId),
        };
        // ETA only makes sense alongside canCome; clear it when switching to cannotCome
        if (status === "cannotCome") {
            venueAttendance.eta = { [userId]: deleteField() };
        }
        await updateAttendanceDoc({ [venueId]: venueAttendance });
    }, [updateAttendanceDoc]);

    /** @type {(venueId: string, userId: string) => Promise<void>} */
    const clearAttendance = useCallback(async (venueId, userId) => {
        await updateAttendanceDoc({
            [venueId]: {
                canCome: arrayRemove(userId),
                cannotCome: arrayRemove(userId),
                eta: { [userId]: deleteField() },
            },
        });
    }, [updateAttendanceDoc]);

    /** @type {(venueIds: string[] | null | undefined, userId: string, status: AttendanceStatus) => Promise<void>} */
    const setAttendanceForMultipleVenues = useCallback(async (venueIds, userId, status) => {
        if (!venueIds || venueIds.length === 0) {
            return;
        }

        const oppositeStatus = status === "canCome" ? "cannotCome" : "canCome";
        /** @type {Record<string, unknown>} */
        const payload = {};
        for (const venueId of venueIds) {
            payload[venueId] = {
                [status]: arrayUnion(userId),
                [oppositeStatus]: arrayRemove(userId),
            };
        }

        await updateAttendanceDoc(payload);
    }, [updateAttendanceDoc]);

    /** @type {(venueIds: string[] | null | undefined, userId: string, status: AttendanceStatus) => Promise<void>} */
    const setGlobalAttendanceStatus = useCallback(async (venueIds, userId, status) => {
        const oppositeStatus = status === "canCome" ? "cannotCome" : "canCome";
        /** @type {Record<string, unknown>} */
        const payload = {
            [ATTENDANCE_GLOBAL_KEY]: {
                [status]: arrayUnion(userId),
                [oppositeStatus]: arrayRemove(userId),
            },
        };

        for (const venueId of venueIds || []) {
            payload[venueId] = {
                [status]: arrayUnion(userId),
                [oppositeStatus]: arrayRemove(userId),
            };
        }

        await updateAttendanceDoc(payload);
    }, [updateAttendanceDoc]);

    /** @type {(venueId: string, userId: string, eta: string) => Promise<void>} */
    const setEta = useCallback(async (venueId, userId, eta) => {
        await updateAttendanceDoc({
            [venueId]: { eta: { [userId]: eta } },
        });
    }, [updateAttendanceDoc]);

    /** @type {(venueId: string, userId: string) => Promise<void>} */
    const clearEta = useCallback(async (venueId, userId) => {
        await updateAttendanceDoc({
            [venueId]: { eta: { [userId]: deleteField() } },
        });
    }, [updateAttendanceDoc]);

    return [
        attendance,
        setAttendanceStatus,
        clearAttendance,
        setAttendanceForMultipleVenues,
        setGlobalAttendanceStatus,
        setEta,
        clearEta,
    ];
}

export default useAttendance;
