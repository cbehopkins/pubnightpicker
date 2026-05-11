// @ts-check

import { useEffect, useMemo, useState } from "react";
import { collection, doc, getDoc, onSnapshot, orderBy, query, where } from "firebase/firestore";
import { db } from "../firebase";
import { getTodaysDate } from "../utils/pollSorting";
import { ATTENDANCE_GLOBAL_KEY, getEffectiveAttendanceState } from "../utils/attendanceState";

function getDateYearsAgo(yearCount) {
    const normalizedYears = Number.isFinite(yearCount) && yearCount > 0 ? yearCount : 1;
    const date = new Date();
    date.setFullYear(date.getFullYear() - normalizedYears);
    return date.toISOString().slice(0, 10);
}

/**
 * @param {{ selected?: string, restaurant?: string | null, date?: string, pubs?: Record<string, unknown> }} poll
 * @param {Record<string, unknown>} attendanceMap
 * @returns {string[]}
 */
export function getVenueIdsForPoll(poll, attendanceMap) {
    const venueIdSet = new Set();

    for (const key of Object.keys(poll?.pubs || {})) {
        if (key !== ATTENDANCE_GLOBAL_KEY) {
            venueIdSet.add(key);
        }
    }

    if (poll?.selected) {
        venueIdSet.add(poll.selected);
    }
    if (poll?.restaurant) {
        venueIdSet.add(poll.restaurant);
    }

    for (const key of Object.keys(attendanceMap || {})) {
        if (key !== ATTENDANCE_GLOBAL_KEY) {
            venueIdSet.add(key);
        }
    }

    return [...venueIdSet];
}

/**
 * @param {Record<string, { selected?: string, restaurant?: string | null, date?: string, pubs?: Record<string, unknown> }>} polls
 * @param {Record<string, Record<string, unknown>>} attendanceByPoll
 * @returns {{ countsByVenueId: Record<string, number>, lastDateByVenueId: Record<string, string | null> }}
 */
export function aggregateVenueAttendance(polls, attendanceByPoll) {
    /** @type {Record<string, number>} */
    const countsByVenueId = {};
    /** @type {Record<string, string | null>} */
    const lastDateByVenueId = {};

    for (const [pollId, poll] of Object.entries(polls || {})) {
        const attendanceMap = attendanceByPoll[pollId] || {};
        const venueIds = getVenueIdsForPoll(poll, attendanceMap);
        for (const venueId of venueIds) {
            // We intentionally resolve effective attendance per venue so the global "any"
            // attendance state contributes to each venue in the shortlist for that poll.
            const effectiveAttendance = getEffectiveAttendanceState(attendanceMap, venueId, null);
            const canComeCount = effectiveAttendance.canCome.length;
            if (!countsByVenueId[venueId]) {
                countsByVenueId[venueId] = 0;
            }
            countsByVenueId[venueId] += canComeCount;

            if (poll.date && (!lastDateByVenueId[venueId] || poll.date > lastDateByVenueId[venueId])) {
                lastDateByVenueId[venueId] = poll.date;
            }
        }
    }

    return { countsByVenueId, lastDateByVenueId };
}

/**
 * @param {number} yearCount
 * @returns {{
 *  countsByVenueId: Record<string, number>,
 *  lastDateByVenueId: Record<string, string | null>,
 *  isLoading: boolean,
 *  errorMessage: string | null,
 *  startDate: string,
 *  endDate: string,
 * }}
 */
export default function useAttendanceVenueStats(yearCount) {
    const endDate = getTodaysDate();
    const startDate = useMemo(() => getDateYearsAgo(yearCount), [yearCount]);
    const statsQuery = useMemo(
        () => query(
            collection(db, "polls"),
            where("completed", "==", true),
            where("date", ">=", startDate),
            where("date", "<=", endDate),
            orderBy("date", "desc")
        ),
        [endDate, startDate]
    );

    const [countsByVenueId, setCountsByVenueId] = useState({});
    const [lastDateByVenueId, setLastDateByVenueId] = useState({});
    const [isLoading, setIsLoading] = useState(true);
    const [errorMessage, setErrorMessage] = useState(null);

    useEffect(() => {
        setIsLoading(true);
        setCountsByVenueId({});
        setLastDateByVenueId({});
        setErrorMessage(null);

        const unsubscribe = onSnapshot(
            statsQuery,
            async (snapshot) => {
                /** @type {Record<string, { selected?: string, restaurant?: string | null, date?: string, pubs?: Record<string, unknown> }>} */
                const pollsById = {};
                snapshot.forEach((docSnapshot) => {
                    pollsById[docSnapshot.id] = docSnapshot.data();
                });

                const pollIds = Object.keys(pollsById);
                if (pollIds.length === 0) {
                    setCountsByVenueId({});
                    setLastDateByVenueId({});
                    setErrorMessage(null);
                    setIsLoading(false);
                    return;
                }

                const attendanceEntries = await Promise.all(
                    pollIds.map(async (pollId) => {
                        const attendanceSnapshot = await getDoc(doc(db, "attendance", pollId));
                        return [pollId, /** @type {Record<string, unknown>} */ (attendanceSnapshot.data() || {})];
                    })
                );

                const attendanceByPoll = Object.fromEntries(attendanceEntries);
                const { countsByVenueId: counts, lastDateByVenueId: lastDates } = aggregateVenueAttendance(
                    pollsById,
                    attendanceByPoll
                );

                setCountsByVenueId(counts);
                setLastDateByVenueId(lastDates);
                setErrorMessage(null);
                setIsLoading(false);
            },
            (error) => {
                console.error("Error loading attendance venue stats", error);
                setCountsByVenueId({});
                setLastDateByVenueId({});
                setErrorMessage("Unable to load attendance stats right now.");
                setIsLoading(false);
            }
        );

        return unsubscribe;
    }, [statsQuery]);

    return {
        countsByVenueId,
        lastDateByVenueId,
        isLoading,
        errorMessage,
        startDate,
        endDate,
    };
}
