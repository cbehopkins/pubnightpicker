// @ts-check

import { useEffect, useMemo, useState } from "react";
import { getTodaysDate } from "../utils/meetSorting";
import { ATTENDANCE_GLOBAL_KEY, getEffectiveAttendanceState } from "../utils/attendanceState";
import { requestLastOrders } from "../utils/lastOrdersApi";

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

    const [countsByVenueId, setCountsByVenueId] = useState({});
    const [lastDateByVenueId, setLastDateByVenueId] = useState({});
    const [isLoading, setIsLoading] = useState(true);
    const [errorMessage, setErrorMessage] = useState(null);

    useEffect(() => {
        const controller = new AbortController();
        setIsLoading(true);
        setCountsByVenueId({});
        setLastDateByVenueId({});
        setErrorMessage(null);

        requestLastOrders("api/attendance/statistics", {
            method: "GET",
            query: { start: startDate, end: endDate },
            signal: controller.signal,
        }).then((value) => {
            if (controller.signal.aborted) return;
            const payload = /** @type {{ counts_by_venue_id?: unknown, last_date_by_venue_id?: unknown }} */ (value);
            if (!payload || typeof payload !== "object"
                || !payload.counts_by_venue_id || typeof payload.counts_by_venue_id !== "object"
                || !payload.last_date_by_venue_id || typeof payload.last_date_by_venue_id !== "object") {
                throw new Error("Invalid attendance statistics response.");
            }
            setCountsByVenueId(/** @type {Record<string, number>} */(payload.counts_by_venue_id));
            setLastDateByVenueId(/** @type {Record<string, string | null>} */(payload.last_date_by_venue_id));
            setErrorMessage(null);
            setIsLoading(false);
        }).catch((error) => {
            if (controller.signal.aborted) return;
            console.error("Error loading attendance venue stats", error);
            setCountsByVenueId({});
            setLastDateByVenueId({});
            setErrorMessage("Unable to load attendance stats right now.");
            setIsLoading(false);
        });

        return () => controller.abort();
    }, [startDate, endDate]);

    return {
        countsByVenueId,
        lastDateByVenueId,
        isLoading,
        errorMessage,
        startDate,
        endDate,
    };
}
