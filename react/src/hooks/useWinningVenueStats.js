// @ts-check

import { useEffect, useMemo, useState } from "react";
import { collection, onSnapshot, orderBy, query, where } from "firebase/firestore";
import { db } from "../firebase";
import { getTodaysDate } from "../utils/pollSorting";

function getDateYearsAgo(yearCount) {
    const normalizedYears = Number.isFinite(yearCount) && yearCount > 0 ? yearCount : 1;
    const date = new Date();
    date.setFullYear(date.getFullYear() - normalizedYears);
    return date.toISOString().slice(0, 10);
}

/**
 * @param {number} yearCount
 * @returns {{ polls: Record<string, { selected?: string, date?: string }>, isLoading: boolean, startDate: string, endDate: string }}
 */
export default function useWinningVenueStats(yearCount) {
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

    const [polls, setPolls] = useState({});
    const [isLoading, setIsLoading] = useState(true);

    useEffect(() => {
        setIsLoading(true);
        setPolls({});

        const unsubscribe = onSnapshot(
            statsQuery,
            (snapshot) => {
                /** @type {Record<string, { selected?: string, date?: string }>} */
                const nextPolls = {};
                snapshot.forEach((docSnapshot) => {
                    nextPolls[docSnapshot.id] = docSnapshot.data();
                });
                setPolls(nextPolls);
                setIsLoading(false);
            },
            (error) => {
                console.error("Error loading winning venue stats", error);
                setPolls({});
                setIsLoading(false);
            }
        );

        return unsubscribe;
    }, [statsQuery]);

    return {
        polls,
        isLoading,
        startDate,
        endDate,
    };
}
