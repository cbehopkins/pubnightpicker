// @ts-check

import { useEffect, useMemo, useState } from "react";
import useWinningVenueStats from "./useWinningVenueStats";
import useVenues from "./useVenues";
import { buildWinningVenueRows, splitRankedStatRows } from "../utils/statsRanking";
import { getDateWeeksAgo } from "../utils/autopoplateFiltering";
import { DEFAULT_VENUE_TYPE } from "../constants/venueTypes";

/**
 * @typedef {{ id: string, label: string, count: number, lastWonDate: string | null, venueType?: string }} RankedStatRow
 *
 * @typedef {{
 *   mostVisited: RankedStatRow[],
 *   leastVisited: RankedStatRow[],
 *   random: RankedStatRow[],
 *   isLoading: boolean,
 *   error: string | null
 * }} AutopopulateVenues
 */

/**
 * Query winning-venue stats to find viable venues for autopopulate. Returns three categories:
 * mostVisited, leastVisited, and random (all not visited in the past 4 weeks, excluding venues
 * already on the Meet). The ranking sample window for most/least expands progressively until those
 * two categories expose at least three unique venues, or the full ranked list is exhausted.
 *
 * @param {string} meetId - Meet document ID
 * @param {Record<string, unknown> | undefined} currentVenueIds - Venues already on the Meet (to exclude)
 * @returns {AutopopulateVenues}
 */
export default function useAutopopulateVenueSelector(meetId, currentVenueIds) {
    const allVenues = useVenues();

    // Base window: 4 weeks
    const baselineWeeks = 4;
    const baselineCutoffDate = useMemo(() => getDateWeeksAgo(baselineWeeks), []);

    // Query last 52 weeks to have historical data for expansion
    const { meets, isLoading: pollsLoading, errorMessage: pollsError } = useWinningVenueStats(1);

    const [mostVisited, setMostVisited] = useState(/** @type {RankedStatRow[]} */([]));
    const [leastVisited, setLeastVisited] = useState(/** @type {RankedStatRow[]} */([]));
    const [randomVenues, setRandomVenues] = useState(/** @type {RankedStatRow[]} */([]));
    const [error, setError] = useState(/** @type {string | null} */(null));

    // Build ranked rows and compute autopopulate options.
    useEffect(() => {
        if (pollsLoading || !meets || Object.keys(meets).length === 0) {
            setMostVisited([]);
            setLeastVisited([]);
            setRandomVenues([]);
            setError(pollsError || null);
            return;
        }

        try {
            // Build initial ranked rows from all Meets
            const allRankedRows = buildWinningVenueRows({ meets, venues: allVenues });
            const pubOnlyRankedRows = allRankedRows.filter((row) => {
                const venueType = allVenues[row.id]?.venueType || row.venueType || DEFAULT_VENUE_TYPE;
                return venueType === DEFAULT_VENUE_TYPE;
            });

            // Build a map of lastWonDate by venueId for filtering
            /** @type {Record<string, string | null>} */
            const lastDateByVenueId = {};
            for (const row of pubOnlyRankedRows) {
                lastDateByVenueId[row.id] = row.lastWonDate;
            }

            // Convert currentVenueIds to Set for fast lookup
            const currentVenueIdSet = new Set(Object.keys(currentVenueIds || {}));

            // Keep the recent-visit exclusion fixed at 4 weeks.
            const isVenueViable = (row) => {
                const lastDate = lastDateByVenueId[row.id];
                const isRecentlyVisited = Boolean(lastDate && lastDate >= baselineCutoffDate);
                const isBanned = Boolean(allVenues[row.id]?.banned);
                return !isRecentlyVisited && !currentVenueIdSet.has(row.id) && !isBanned;
            };

            const finalRandom = pubOnlyRankedRows.filter(isVenueViable);

            let finalMostVisited = [];
            let finalLeastVisited = [];
            let windowLimit = 5;
            const targetUniqueVenueCount = 3;

            while (windowLimit <= pubOnlyRankedRows.length) {
                const { most, least } = splitRankedStatRows(pubOnlyRankedRows, windowLimit);
                const viableMost = most.filter(isVenueViable);
                const viableLeast = least.filter(isVenueViable);
                const uniqueTopBottomIds = new Set([
                    ...viableMost.map((row) => row.id),
                    ...viableLeast.map((row) => row.id),
                ]);

                finalMostVisited = viableMost;
                finalLeastVisited = viableLeast;

                if (uniqueTopBottomIds.size >= targetUniqueVenueCount) {
                    break;
                }

                windowLimit += 5;
            }

            // Final fallback to the viable pool if a category remains empty.
            if (finalMostVisited.length === 0) {
                finalMostVisited = finalRandom;
            }
            if (finalLeastVisited.length === 0) {
                finalLeastVisited = finalRandom;
            }

            setMostVisited(finalMostVisited);
            setLeastVisited(finalLeastVisited);
            setRandomVenues(finalRandom);
            setError(null);
        } catch (err) {
            console.error("Error in useAutopopulateVenueSelector:", err);
            setMostVisited([]);
            setLeastVisited([]);
            setRandomVenues([]);
            setError("Unable to compute autopopulate venues right now.");
        }
    }, [meets, pollsLoading, pollsError, allVenues, currentVenueIds, baselineCutoffDate]);

    return {
        mostVisited,
        leastVisited,
        random: randomVenues,
        isLoading: pollsLoading,
        error,
    };
}
