// @ts-check

import { useEffect, useMemo, useState } from "react";
import { useSelector } from "react-redux";
import { requestLastOrders } from "../utils/lastOrdersApi";

function yearsAgo(yearCount, endDate) {
    const [year, month, day] = endDate.split("-").map(Number);
    const date = new Date(Date.UTC(year - yearCount, month - 1, day));
    return date.toISOString().slice(0, 10);
}

function getLondonToday() {
    const parts = Object.fromEntries(new Intl.DateTimeFormat("en-GB", {
        timeZone: "Europe/London", year: "numeric", month: "2-digit", day: "2-digit",
    }).formatToParts(new Date()).map(({ type, value }) => [type, value]));
    return `${parts.year}-${parts.month}-${parts.day}`;
}

/** @param {number} yearCount */
export default function useWinningVenueStats(yearCount) {
    const groupId = useSelector(/** @param {{ group?: { groupId?: string | null } }} state */
        (state) => state.group?.groupId ?? null);
    const [report, setReport] = useState(/** @type {{ meets: Record<string, { selectedVenueId: string, date: string }>, startDate: string, endDate: string, requestKey: string } | null} */(null));
    const [isLoading, setIsLoading] = useState(Boolean(groupId));
    const [errorMessage, setErrorMessage] = useState(/** @type {string | null} */(null));
    const requestKey = `${groupId || ""}:${yearCount}`;
    const endDate = useMemo(() => getLondonToday(), []);
    const startDate = yearsAgo(yearCount, endDate);

    useEffect(() => {
        let cancelled = false;
        setReport(null);
        setErrorMessage(null);
        if (!groupId) {
            setIsLoading(false);
            setErrorMessage("The current Group is unavailable.");
            return () => { cancelled = true; };
        }
        setIsLoading(true);
        requestLastOrders(`api/groups/${encodeURIComponent(groupId)}/winning-statistics`, {
            method: "GET", allowAnonymous: true, query: { years: String(yearCount) },
        }).then((data) => {
            if (cancelled) return;
            if (!data || typeof data.counts_by_venue_id !== "object" || typeof data.last_date_by_venue_id !== "object") {
                throw new Error("The statistics response was invalid.");
            }
            const meets = /** @type {Record<string, { selectedVenueId: string, date: string }>} */ ({});
            for (const [venueId, count] of Object.entries(data.counts_by_venue_id)) {
                if (!Number.isInteger(count) || count < 0) throw new Error("The statistics response was invalid.");
                const date = data.last_date_by_venue_id[venueId];
                if (typeof date !== "string") throw new Error("The statistics response was invalid.");
                for (let index = 0; index < count; index++) meets[`${venueId}-${index}`] = { selectedVenueId: venueId, date };
            }
            setReport({ meets, startDate: data.start_date, endDate: data.end_date, requestKey });
            setIsLoading(false);
        }).catch((error) => {
            if (cancelled) return;
            console.error("Error loading winning venue stats", error);
            setReport(null);
            setErrorMessage("Unable to load winning venue stats right now.");
            setIsLoading(false);
        });
        return () => { cancelled = true; };
    }, [groupId, yearCount, requestKey]);

    const currentReport = report?.requestKey === requestKey ? report : null;
    return { meets: currentReport?.meets ?? {}, isLoading: isLoading || (!currentReport && !errorMessage), errorMessage, startDate: currentReport?.startDate ?? startDate, endDate: currentReport?.endDate ?? endDate, isEmpty: Boolean(currentReport && Object.keys(currentReport.meets).length === 0) };
}
