// @ts-check

export const DEFAULT_VENUE_TYPE = "pub";
export const VENUE_TYPE_OPTIONS = ["all", "pub", "restaurant", "event"];

/**
 * @param {string} venueType
 * @returns {string}
 */
export function getVenueTypeLabel(venueType) {
    return venueType === "all"
        ? "All Types"
        : venueType.charAt(0).toUpperCase() + venueType.slice(1);
}
