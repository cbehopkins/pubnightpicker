// @ts-check

/**
 * Calculate a date N weeks in the past.
 * @param {number} weekCount - Number of weeks to go back
 * @returns {string} ISO date string (YYYY-MM-DD)
 */
export function getDateWeeksAgo(weekCount) {
    const normalizedWeeks = Number.isFinite(weekCount) && weekCount > 0 ? weekCount : 0;
    const date = new Date();
    date.setDate(date.getDate() - normalizedWeeks * 7);
    return date.toISOString().slice(0, 10);
}

/**
 * Filter ranked venue rows by excluding venues with lastDate on or after a cutoff date.
 * @param {Array<{ id: string, label: string, count: number, lastWonDate: string | null }>} rankedRows
 * @param {Record<string, string | null>} lastDateByVenueId - Map of venueId to last date visited
 * @param {string} cutoffDate - ISO date string (YYYY-MM-DD); venues with lastDate >= cutoffDate are excluded
 * @returns {Array<{ id: string, label: string, count: number, lastWonDate: string | null }>}
 */
export function filterVenuesByRecentVisits(rankedRows, lastDateByVenueId, cutoffDate) {
    return rankedRows.filter((row) => {
        const lastDate = lastDateByVenueId[row.id];
        // Include venue if it has no recorded date, or if its last date is before the cutoff
        if (!lastDate || lastDate < cutoffDate) {
            return true;
        }
        return false;
    });
}

/**
 * Select a random item from an array.
 * @template T
 * @param {T[]} array
 * @returns {T | null}
 */
export function selectRandomFromArray(array) {
    if (!array || array.length === 0) {
        return null;
    }
    const randomIndex = Math.floor(Math.random() * array.length);
    return array[randomIndex];
}

/**
 * Pick up to three unique venue IDs: one from mostVisited, one from leastVisited,
 * and one from random pool, excluding any IDs already selected.
 *
 * @param {{ id: string }[]} mostVisited
 * @param {{ id: string }[]} leastVisited
 * @param {{ id: string }[]} randomVenues
 * @param {Record<string, unknown> | undefined} existingPubs
 * @returns {string[]}
 */
export function chooseAutopopulateVenueIds(mostVisited, leastVisited, randomVenues, existingPubs) {
    const selectedIds = [];
    const pickedIds = new Set(Object.keys(existingPubs || {}));

    /**
     * @param {{ id: string }[]} rows
     */
    const pickFromGroup = (rows) => {
        const candidates = (rows || []).filter((row) => !pickedIds.has(row.id));
        const selected = selectRandomFromArray(candidates);
        if (!selected) {
            return;
        }
        pickedIds.add(selected.id);
        selectedIds.push(selected.id);
    };

    pickFromGroup(mostVisited);
    pickFromGroup(leastVisited);
    pickFromGroup(randomVenues);

    return selectedIds;
}
