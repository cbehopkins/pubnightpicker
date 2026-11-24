// @ts-check

import { useEffect, useMemo, useState } from "react";
import { collection, doc, getDoc, onSnapshot, orderBy, query, where } from "firebase/firestore";
import { db } from "../firebase";
import { getTodaysDate, MeetData } from "../utils/meetSorting";
import { ATTENDANCE_GLOBAL_KEY, getEffectiveAttendanceState } from "../utils/attendanceState";

/** @type {Record<string, { venueType?: string }>} */
const EMPTY_VENUE_TYPES = {};

function getDateYearsAgo(yearCount) {
    const normalizedYears = Number.isFinite(yearCount) && yearCount > 0 ? yearCount : 1;
    const date = new Date();
    date.setFullYear(date.getFullYear() - normalizedYears);
    return date.toISOString().slice(0, 10);
}

/**
 * @param {{ selectedVenueId?: string, restaurantId?: string | null, date?: string, venues?: Record<string, unknown> }} meet
 * @param {Record<string, unknown>} attendanceMap
 * @returns {string[]}
 */
export function getVenueIdsForMeet(meet, attendanceMap) {
    const venueIdSet = new Set();

    for (const key of Object.keys(meet?.venues || {})) {
        if (key !== ATTENDANCE_GLOBAL_KEY) {
            venueIdSet.add(key);
        }
    }

    if (meet?.selectedVenueId) {
        venueIdSet.add(meet.selectedVenueId);
    }
    if (meet?.restaurantId) {
        venueIdSet.add(meet.restaurantId);
    }

    for (const key of Object.keys(attendanceMap || {})) {
        if (key !== ATTENDANCE_GLOBAL_KEY) {
            venueIdSet.add(key);
        }
    }

    return [...venueIdSet];
}

/**
 * @param {Record<string, { selectedVenueId?: string, restaurantId?: string | null, date?: string, venues?: Record<string, unknown> }>} meets
 * @param {Record<string, Record<string, unknown>>} attendanceByMeet
 * @param {Record<string, { venueType?: string }>} venueTypesById
 * @returns {{ countsByVenueId: Record<string, number>, lastDateByVenueId: Record<string, string | null> }}
 */
export function aggregateVenueAttendance(meets, attendanceByMeet, venueTypesById = EMPTY_VENUE_TYPES) {
    /** @type {Record<string, number>} */
    const countsByVenueId = {};
    /** @type {Record<string, string | null>} */
    const lastDateByVenueId = {};

    for (const [meetId, meet] of Object.entries(meets || {})) {
        const attendanceMap = attendanceByMeet[meetId] || {};
        const venueIds = getVenueIdsForMeet(meet, attendanceMap);
        for (const venueId of venueIds) {
            const venueType = venueId === meet.restaurantId
                ? "restaurant"
                : venueTypesById[venueId]?.venueType || "pub";
            const effectiveAttendance = getEffectiveAttendanceState(attendanceMap, venueId, null, venueType);
            const canComeCount = effectiveAttendance.canCome.length;
            if (!countsByVenueId[venueId]) {
                countsByVenueId[venueId] = 0;
            }
            countsByVenueId[venueId] += canComeCount;

            if (meet.date && (!lastDateByVenueId[venueId] || meet.date > lastDateByVenueId[venueId])) {
                lastDateByVenueId[venueId] = meet.date;
            }
        }
    }

    return { countsByVenueId, lastDateByVenueId };
}

/**
 * @param {number} yearCount
 * @param {Record<string, { venueType?: string }>} [venueTypesById]
 * @returns {{
 *  countsByVenueId: Record<string, number>,
 *  lastDateByVenueId: Record<string, string | null>,
 *  isLoading: boolean,
 *  errorMessage: string | null,
 *  startDate: string,
 *  endDate: string,
 * }}
 */
export default function useAttendanceVenueStats(yearCount, venueTypesById = EMPTY_VENUE_TYPES) {
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
                const meetRecordsById = {};
                snapshot.forEach((docSnapshot) => {
                    meetRecordsById[docSnapshot.id] = docSnapshot.data();
                });

                const meetsById = new MeetData(meetRecordsById).meets;
                const meetIds = Object.keys(meetsById);
                if (meetIds.length === 0) {
                    setCountsByVenueId({});
                    setLastDateByVenueId({});
                    setErrorMessage(null);
                    setIsLoading(false);
                    return;
                }

                const attendanceEntries = await Promise.all(
                    meetIds.map(async (meetId) => {
                        const attendanceSnapshot = await getDoc(doc(db, "attendance", meetId));
                        return [meetId, /** @type {Record<string, unknown>} */ (attendanceSnapshot.data() || {})];
                    })
                );

                const attendanceByMeet = Object.fromEntries(attendanceEntries);
                const { countsByVenueId: counts, lastDateByVenueId: lastDates } = aggregateVenueAttendance(
                    meetsById,
                    attendanceByMeet,
                    venueTypesById
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
    }, [statsQuery, venueTypesById]);

    return {
        countsByVenueId,
        lastDateByVenueId,
        isLoading,
        errorMessage,
        startDate,
        endDate,
    };
}
