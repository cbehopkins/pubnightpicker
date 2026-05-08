// @ts-check

export const DEFAULT_ARRIVAL_TIME = "19:30";

/**
 * @param {unknown} value
 * @returns {value is string}
 */
function isTimeString(value) {
    return typeof value === "string" && /^([01]\d|2[0-3]):[0-5]\d$/.test(value);
}

/**
 * Normalize user-configurable arrival time values to HH:mm.
 * Falls back to the provided fallback when input is invalid or missing.
 *
 * @param {unknown} value
 * @param {string=} fallback
 * @returns {string}
 */
export function normalizeArrivalTime(value, fallback = DEFAULT_ARRIVAL_TIME) {
    return isTimeString(value) ? value : fallback;
}
