// @ts-check

import { useEffect, useMemo, useState } from "react";
import useWinningVenueStats from "./useWinningVenueStats";
import usePubs from "./usePubs";
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
 * already on the poll). If either most/least category is empty after filtering, the ranking window
 * expands progressively to include more candidates until viable venues are found.
 *
 * @param {string} pollId - Poll ID (for logging)
 * @param {Record<string, unknown> | undefined} currentPubIds - Venues already on the poll (to exclude)
 * @returns {AutopopulateVenues}
 */
export default function useAutopopulateVenueSelector(pollId, currentPubIds) {
    const allVenues = usePubs();

    // Base window: 4 weeks
    const baselineWeeks = 4;
    const baselineCutoffDate = useMemo(() => getDateWeeksAgo(baselineWeeks), []);

    // Query last 52 weeks to have historical data for expansion
    const { polls, isLoading: pollsLoading, errorMessage: pollsError } = useWinningVenueStats(1);

    const [mostVisited, setMostVisited] = useState(/** @type {RankedStatRow[]} */([]));
    const [leastVisited, setLeastVisited] = useState(/** @type {RankedStatRow[]} */([]));
    const [randomVenues, setRandomVenues] = useState(/** @type {RankedStatRow[]} */([]));
    const [error, setError] = useState(/** @type {string | null} */(null));

    // Build ranked rows and compute autopopulate options.
    useEffect(() => {
        if (pollsLoading || !polls || Object.keys(polls).length === 0) {
            setMostVisited([]);
            setLeastVisited([]);
            setRandomVenues([]);
            setError(pollsError || null);
            return;
        }

        try {
            // Build initial ranked rows from all polls
            const allRankedRows = buildWinningVenueRows({ polls, venues: allVenues });
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

            // Convert currentPubIds to Set for fast lookup
            const currentPubIdSet = new Set(Object.keys(currentPubIds || {}));

            // Keep the recent-visit exclusion fixed at 4 weeks.
            const isVenueViable = (row) => {
                const lastDate = lastDateByVenueId[row.id];
                const isRecentlyVisited = Boolean(lastDate && lastDate >= baselineCutoffDate);
                const isBanned = Boolean(allVenues[row.id]?.banned);
                return !isRecentlyVisited && !currentPubIdSet.has(row.id) && !isBanned;
            };

            const finalRandom = pubOnlyRankedRows.filter(isVenueViable);

            let finalMostVisited = [];
            let finalLeastVisited = [];
            let windowLimit = 5;

            while (windowLimit <= pubOnlyRankedRows.length) {
                const { most, least } = splitRankedStatRows(pubOnlyRankedRows, windowLimit);
                const viableMost = most.filter(isVenueViable);
                const viableLeast = least.filter(isVenueViable);

                if (viableMost.length > 0 && finalMostVisited.length === 0) {
                    finalMostVisited = viableMost;
                }
                if (viableLeast.length > 0 && finalLeastVisited.length === 0) {
                    finalLeastVisited = viableLeast;
                }

                if (finalMostVisited.length > 0 && finalLeastVisited.length > 0) {
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
    }, [polls, pollsLoading, pollsError, allVenues, currentPubIds, baselineCutoffDate]);

    return {
        mostVisited,
        leastVisited,
        random: randomVenues,
        isLoading: pollsLoading,
        error,
    };
}
