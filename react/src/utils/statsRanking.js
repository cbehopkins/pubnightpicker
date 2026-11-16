// @ts-check

/** @typedef {{ name?: string, venueType?: string }} VenueLike */

/** @typedef {{ id: string, label: string, count: number, lastWonDate: string | null, venueType: string }} RankedStatRow */

/**
 * @param {RankedStatRow} left
 * @param {RankedStatRow} right
 */
export function compareRankedStatRowsDescending(left, right) {
    if (left.count !== right.count) {
        return right.count - left.count;
    }

    const byLabel = left.label.localeCompare(right.label, undefined, { sensitivity: "base", numeric: true });
    if (byLabel !== 0) {
        return byLabel;
    }

    return left.id.localeCompare(right.id, undefined, { sensitivity: "base", numeric: true });
}

/**
 * @param {RankedStatRow} left
 * @param {RankedStatRow} right
 */
export function compareRankedStatRowsAscending(left, right) {
    if (left.count !== right.count) {
        return left.count - right.count;
    }

    const byLabel = left.label.localeCompare(right.label, undefined, { sensitivity: "base", numeric: true });
    if (byLabel !== 0) {
        return byLabel;
    }

    return left.id.localeCompare(right.id, undefined, { sensitivity: "base", numeric: true });
}

/**
 * Build ranked venue stats from completed Meets.
 *
 * @param {{ meets?: Record<string, { selectedVenueId?: string, date?: string }>, venues?: Record<string, VenueLike | undefined> }} input
 * @returns {RankedStatRow[]}
 */
export function buildWinningVenueRows({ meets = {}, venues = {} }) {
    /** @type {Record<string, RankedStatRow>} */
    const rowsById = {};

    for (const [venueId, venue] of Object.entries(venues || {})) {
        rowsById[venueId] = {
            id: venueId,
            label: venue?.name || venueId,
            count: 0,
            lastWonDate: null,
            venueType: venue?.venueType || "pub",
        };
    }

    for (const meet of Object.values(meets || {})) {
        const venueId = meet?.selectedVenueId;
        if (!venueId) {
            continue;
        }

        if (!rowsById[venueId]) {
            rowsById[venueId] = {
                id: venueId,
                label: venueId,
                count: 0,
                lastWonDate: null,
                venueType: "pub",
            };
        }

        const row = rowsById[venueId];
        row.count += 1;

        if (meet.date && (!row.lastWonDate || meet.date > row.lastWonDate)) {
            row.lastWonDate = meet.date;
        }
    }

    return Object.values(rowsById).sort(compareRankedStatRowsDescending);
}

/**
 * @param {RankedStatRow[]} rows
 * @param {number} limit
 * @returns {{ most: RankedStatRow[], least: RankedStatRow[] }}
 */
export function splitRankedStatRows(rows, limit) {
    const safeLimit = Number.isFinite(limit) && limit > 0 ? Math.floor(limit) : 5;

    return {
        most: rows.slice(0, safeLimit),
        least: [...rows].sort(compareRankedStatRowsAscending).slice(0, safeLimit),
    };
}

/**
 * Build ranked venue rows from pre-aggregated counts.
 *
 * @param {{
 *  countsByVenueId?: Record<string, number>,
 *  lastDateByVenueId?: Record<string, string | null>,
 *  venues?: Record<string, VenueLike | undefined>,
 * }} input
 * @returns {RankedStatRow[]}
 */
export function buildVenueCountRows({ countsByVenueId = {}, lastDateByVenueId = {}, venues = {} }) {
    /** @type {Record<string, RankedStatRow>} */
    const rowsById = {};

    for (const [venueId, venue] of Object.entries(venues || {})) {
        rowsById[venueId] = {
            id: venueId,
            label: venue?.name || venueId,
            count: countsByVenueId[venueId] || 0,
            lastWonDate: lastDateByVenueId[venueId] || null,
            venueType: venue?.venueType || "pub",
        };
    }

    for (const [venueId, count] of Object.entries(countsByVenueId || {})) {
        if (!rowsById[venueId]) {
            rowsById[venueId] = {
                id: venueId,
                label: venueId,
                count,
                lastWonDate: lastDateByVenueId[venueId] || null,
                venueType: "pub",
            };
        }
    }

    return Object.values(rowsById).sort(compareRankedStatRowsDescending);
}
