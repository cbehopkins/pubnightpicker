// @ts-check

import { useState, useEffect, useCallback, useMemo, useRef } from "react";
import { useSelector } from "react-redux";
import { arrayRemove, arrayUnion, deleteField, doc, runTransaction } from "firebase/firestore";
import { db } from "../firebase";
import { LastOrdersApiError, requestLastOrders, streamLastOrders } from "../utils/lastOrdersApi";
import { ATTENDANCE_GLOBAL_KEY } from "../utils/attendanceState";

/** @typedef {Record<string, { uid: string, name?: string, photoUrl?: string }>} AttendanceProfiles */

/** @typedef {{ attendance: AttendanceMap, profiles: AttendanceProfiles }} ParsedAttendance */

/** @typedef {{ onSnapshot: (value: unknown) => void, onReset: () => void, onError: (message: string) => void }} AttendanceListener */

/** @typedef {{ key: string, groupId: string, listeners: Map<string, Set<AttendanceListener>>, controller: AbortController | null, connectedMeetKey: string, scheduled: boolean }} GroupAttendanceStream */

const maxStreamedAttendanceMeets = 50;
const fatalStreamErrorCodes = ["signed_out", "unauthenticated", "forbidden", "not_found"];
const invalidSnapshotMessage = "Last Orders returned an invalid attendance snapshot.";
const unavailableMessage = "Attendance updates are temporarily unavailable.";

/** @type {Map<string, GroupAttendanceStream>} */
const groupAttendanceStreams = new Map();

/** @param {unknown} value @param {string} meetId @returns {ParsedAttendance | null} */
function parseAttendanceSnapshot(value, meetId) {
    if (!value || typeof value !== "object") return null;
    const payload = /** @type {{ meet_id?: unknown, cursor?: unknown, entries?: unknown, participants?: unknown }} */ (value);
    if (payload.meet_id !== meetId || typeof payload.cursor !== "string" || !payload.cursor
        || !payload.entries || typeof payload.entries !== "object" || Array.isArray(payload.entries)
        || !payload.participants || typeof payload.participants !== "object" || Array.isArray(payload.participants)) return null;

    /** @type {AttendanceMap} */
    const attendance = {};
    for (const [venueId, rawEntry] of Object.entries(payload.entries)) {
        if (!rawEntry || typeof rawEntry !== "object" || Array.isArray(rawEntry)) return null;
        const entry = /** @type {{ can_come?: unknown, cannot_come?: unknown, eta?: unknown }} */ (rawEntry);
        if (!Array.isArray(entry.can_come) || !entry.can_come.every((uid) => typeof uid === "string")
            || !Array.isArray(entry.cannot_come) || !entry.cannot_come.every((uid) => typeof uid === "string")
            || !entry.eta || typeof entry.eta !== "object" || Array.isArray(entry.eta)) return null;
        const eta = /** @type {Record<string, unknown>} */ (entry.eta);
        if (!Object.values(eta).every((time) => typeof time === "string")) return null;
        attendance[venueId] = { canCome: entry.can_come, cannotCome: entry.cannot_come, eta: /** @type {Record<string, string>} */ (eta) };
    }

    /** @type {AttendanceProfiles} */
    const profiles = {};
    for (const [uid, rawProfile] of Object.entries(payload.participants)) {
        if (!rawProfile || typeof rawProfile !== "object" || Array.isArray(rawProfile)) return null;
        const profile = /** @type {{ id?: unknown, name?: unknown, photo_url?: unknown }} */ (rawProfile);
        if (profile.id !== uid || (profile.name !== undefined && typeof profile.name !== "string")
            || (profile.photo_url !== undefined && typeof profile.photo_url !== "string")) return null;
        profiles[uid] = {
            uid,
            name: typeof profile.name === "string" ? profile.name : undefined,
            photoUrl: typeof profile.photo_url === "string" ? profile.photo_url : undefined,
        };
    }
    return { attendance, profiles };
}

/** @param {string} groupId @param {string} userId @param {string} meetId @param {AttendanceListener} listener */
function subscribeGroupAttendance(groupId, userId, meetId, listener) {
    const key = `${groupId}\u0000${userId}`;
    let stream = groupAttendanceStreams.get(key);
    if (!stream) {
        stream = { key, groupId, listeners: new Map(), controller: null, connectedMeetKey: "", scheduled: false };
        groupAttendanceStreams.set(key, stream);
    }
    const owner = stream;
    let meetListeners = owner.listeners.get(meetId);
    if (!meetListeners) {
        meetListeners = new Set();
        owner.listeners.set(meetId, meetListeners);
    }
    const listeners = meetListeners;
    listeners.add(listener);
    scheduleGroupAttendance(owner);
    return () => {
        listeners.delete(listener);
        if (listeners.size === 0 && owner.listeners.get(meetId) === listeners) owner.listeners.delete(meetId);
        scheduleGroupAttendance(owner);
    };
}

/** @param {GroupAttendanceStream} stream */
function scheduleGroupAttendance(stream) {
    if (stream.scheduled) return;
    stream.scheduled = true;
    queueMicrotask(() => {
        stream.scheduled = false;
        const meetIds = [...stream.listeners.keys()].sort();
        const meetKey = meetIds.join("\u0000");
        if (stream.controller && meetKey === stream.connectedMeetKey) return;
        stream.controller?.abort();
        stream.controller = null;
        stream.connectedMeetKey = "";
        if (meetIds.length === 0) {
            if (groupAttendanceStreams.get(stream.key) === stream) groupAttendanceStreams.delete(stream.key);
            return;
        }
        startGroupAttendance(stream, meetIds, meetKey);
    });
}

/** @param {GroupAttendanceStream} stream @param {string[]} meetIds @param {(listener: AttendanceListener) => void} notify */
function notifyAttendanceListeners(stream, meetIds, notify) {
    for (const meetId of meetIds) {
        for (const listener of stream.listeners.get(meetId) ?? []) notify(listener);
    }
}

/** @param {GroupAttendanceStream} stream @param {string[]} meetIds @param {string} meetKey */
function startGroupAttendance(stream, meetIds, meetKey) {
    const controller = new AbortController();
    stream.controller = controller;
    stream.connectedMeetKey = meetKey;
    const isCurrent = () => stream.controller === controller && !controller.signal.aborted;
    const fail = (message) => notifyAttendanceListeners(stream, meetIds, (listener) => listener.onError(message));
    const stop = () => {
        if (stream.controller !== controller) return;
        stream.controller = null;
        stream.connectedMeetKey = "";
    };
    if (meetIds.length > maxStreamedAttendanceMeets) {
        fail("Too many Meets are open for attendance updates.");
        stop();
        return;
    }
    try {
        streamLastOrders(`api/meets/${encodeURIComponent(stream.groupId)}/attendance/stream`, {
            query: meetIds.map((meetId) => ["meet_id", meetId]),
            signal: controller.signal,
            onopen: async (response) => {
                const contentType = response.headers.get("content-type")?.split(";")[0].trim().toLowerCase();
                if (response.ok && contentType === "text/event-stream") return;
                const code = response.status === 401 ? "unauthenticated" : response.status === 403 ? "forbidden" :
                    response.status === 404 ? "not_found" : "stream_unavailable";
                throw new LastOrdersApiError(code, unavailableMessage, { status: response.status });
            },
            onmessage: (event) => {
                if (!isCurrent()) return;
                if (event.event === "view_reset") {
                    stop();
                    controller.abort();
                    notifyAttendanceListeners(stream, meetIds, (listener) => listener.onReset());
                    scheduleGroupAttendance(stream);
                    return;
                }
                if (event.event !== "attendance_snapshots") return;
                let payload;
                try {
                    payload = JSON.parse(event.data);
                } catch {
                    fail(invalidSnapshotMessage);
                    return;
                }
                if (!payload || typeof payload.cursor !== "string" || !payload.cursor || !Array.isArray(payload.meets)
                    || !payload.meets.every((meet) => meet && typeof meet.meet_id === "string")) {
                    fail(invalidSnapshotMessage);
                    return;
                }
                for (const meet of payload.meets) {
                    for (const listener of stream.listeners.get(meet.meet_id) ?? []) listener.onSnapshot(meet);
                }
            },
            onclose: () => {
                if (!controller.signal.aborted) throw new Error("Attendance updates closed unexpectedly.");
            },
            onerror: (error) => {
                if (!isCurrent()) return;
                fail(error?.message || unavailableMessage);
                if (error instanceof LastOrdersApiError && fatalStreamErrorCodes.includes(error.code)) throw error;
                return 5000;
            },
        }).catch((error) => {
            if (!isCurrent()) return;
            stop();
            fail(error?.message || unavailableMessage);
        });
    } catch (error) {
        stop();
        fail(error?.message || unavailableMessage);
    }
}

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
 *  (venueId: string, userId: string) => Promise<void>,
 *  AttendanceProfiles,
 *  boolean,
 *  string
 * ]} UseAttendanceResult
 */

/**
 * @param {string} meetId
 * @param {boolean} [enabled=true]
 * @returns {UseAttendanceResult}
 */
function useAttendance(meetId, enabled = true) {
    const groupId = useSelector(
        /** @param {{ group?: { groupId?: string | null } }} state */
        (state) => state.group?.groupId ?? null
    );
    const userId = useSelector(
        /** @param {{ auth?: { uid?: string | null } }} state */
        (state) => state.auth?.uid ?? null
    );
    /** @type {[AttendanceMap, import("react").Dispatch<import("react").SetStateAction<AttendanceMap>>]} */
    const [attendance, setAttendance] = useState({});
    const [attendanceProfiles, setAttendanceProfiles] = useState(/** @type {AttendanceProfiles} */({}));
    const [isLoading, setIsLoading] = useState(Boolean(enabled));
    const [errorMessage, setErrorMessage] = useState("");
    const docRef = useMemo(() => doc(db, "attendance", meetId), [meetId]);
    const streamSequenceRef = useRef(0);

    const applySnapshot = useCallback((value) => {
        const parsed = parseAttendanceSnapshot(value, meetId);
        if (!parsed) {
            setErrorMessage(invalidSnapshotMessage);
            setIsLoading(false);
            return false;
        }
        setAttendance(parsed.attendance);
        setAttendanceProfiles(parsed.profiles);
        setErrorMessage("");
        setIsLoading(false);
        return true;
    }, [meetId]);

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

        setAttendance({});
        setAttendanceProfiles({});
        setErrorMessage("");
        setIsLoading(true);
        streamSequenceRef.current = 0;
        let active = true;
        const snapshotController = new AbortController();
        const loadSnapshot = async () => {
            const observedSequence = streamSequenceRef.current;
            try {
                const snapshot = await requestLastOrders(`/api/meet/${encodeURIComponent(meetId)}/attendance`, {
                    method: "GET",
                    signal: snapshotController.signal,
                });
                if (active && streamSequenceRef.current === observedSequence) applySnapshot(snapshot);
            } catch (error) {
                if (active && !snapshotController.signal.aborted) {
                    setErrorMessage(error?.message || "Attendance is temporarily unavailable.");
                    setIsLoading(false);
                }
            }
        };
        void loadSnapshot();
        const unsubscribe = groupId && userId
            ? subscribeGroupAttendance(groupId, userId, meetId, {
                onSnapshot: (snapshot) => {
                    if (active && applySnapshot(snapshot)) streamSequenceRef.current += 1;
                },
                onReset: () => {
                    if (!active) return;
                    streamSequenceRef.current += 1;
                    setAttendance({});
                    setAttendanceProfiles({});
                    setIsLoading(true);
                    void loadSnapshot();
                },
                onError: (message) => {
                    if (active) {
                        setErrorMessage(message);
                        setIsLoading(false);
                    }
                },
            })
            : () => undefined;
        return () => {
            active = false;
            snapshotController.abort();
            unsubscribe();
        };
    }, [applySnapshot, enabled, groupId, meetId, userId]);

    /** @type {(venueId: string, userId: string, status: AttendanceStatus) => Promise<void>} */
    const setAttendanceStatus = useCallback(async (venueId, _userId, status) => {
        void _userId;
        await requestLastOrders(`/api/meet/${encodeURIComponent(meetId)}/attendance/${encodeURIComponent(venueId)}`, {
            method: "PUT",
            body: { status: status === "canCome" ? "attending" : "not_attending" },
        });
    }, [meetId]);

    /** @type {(venueId: string, userId: string) => Promise<void>} */
    const clearAttendance = useCallback(async (venueId, _userId) => {
        void _userId;
        await requestLastOrders(`/api/meet/${encodeURIComponent(meetId)}/attendance/${encodeURIComponent(venueId)}`, {
            method: "DELETE",
        });
    }, [meetId]);

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
        attendanceProfiles,
        isLoading,
        errorMessage,
    ];
}

export default useAttendance;
