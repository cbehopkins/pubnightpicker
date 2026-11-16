// @ts-check

import { getEffectiveAttendanceState } from "./attendanceState";

/** @typedef {Record<string, string[]>} VotesMap */
/** @typedef {Record<string, { canCome?: string[], cannotCome?: string[], eta?: Record<string, string> } | undefined>} AttendanceMap */

/**
 * @typedef {Object} VenueEntry
 * @property {string=} name
 * @property {string=} website
 * @property {string=} image
 * @property {string=} address
 */

/** @typedef {Record<string, VenueEntry | undefined>} VenueMap */

/**
 * @typedef {Object} EventVenueViewModel
 * @property {string} id
 * @property {string | undefined} name
 * @property {string | undefined} website
 * @property {string | undefined} image
 * @property {string | undefined} address
 * @property {string[]} dedupedVotes
 * @property {boolean} allowShowVoters
 * @property {string[]} canCome
 * @property {string[]} cannotCome
 * @property {boolean} userCanCome
 * @property {boolean} userCannotCome
 * @property {boolean} hasAttendanceData
 * @property {string | undefined} userEta
 * @property {Record<string, string>} etaMap
 */

/**
 * @typedef {EventVenueViewModel & {
 *   restaurantTime?: string | null | undefined
 * }} RestaurantVenueViewModel
 */

/**
 * @typedef {Object} CurrentEventViewModel
 * @property {EventVenueViewModel} mainVenue
 * @property {RestaurantVenueViewModel | null} restaurantVenue
 */

/**
 * @param {VotesMap} votes
 * @param {string | null | undefined} venueId
 * @returns {string[]}
 */
export function getDedupedVotesForVenue(votes, venueId) {
    const allVotes = [];

    if (venueId && votes[venueId]) {
        allVotes.push(...votes[venueId]);
    }
    if (venueId && votes.any) {
        allVotes.push(...votes.any);
    }

    return [...new Set(allVotes)];
}

/**
 * @param {{
 *   currentVenueId: string,
 *   restaurant_id: string | null | undefined,
 *   restaurant_time: string | null | undefined,
 *   venues: VenueMap,
 *   votes: VotesMap,
 *   attendance: AttendanceMap | null | undefined,
 *   currUserId: string | null | undefined,
 *   show_voters: boolean,
 * }} input
 * @returns {CurrentEventViewModel | null}
 */
export function buildCurrentMeetViewModel({
    currentVenueId,
    restaurant_id,
    restaurant_time,
    venues,
    votes,
    attendance,
    currUserId,
    show_voters,
}) {
    const currentVenue = venues[currentVenueId];
    if (!currentVenue) {
        return null;
    }

    const mainAttendance = getEffectiveAttendanceState(attendance, currentVenueId, currUserId);
    const mainVotes = getDedupedVotesForVenue(votes, currentVenueId);
    const venueWasVotedFor = currentVenueId in votes || Boolean(votes.any);
    const mainEtaMap = (attendance && attendance[currentVenueId]?.eta) || {};

    /** @type {EventVenueViewModel} */
    const mainVenue = {
        id: currentVenueId,
        name: currentVenue.name,
        website: currentVenue.website,
        image: currentVenue.image,
        address: currentVenue.address,
        dedupedVotes: mainVotes,
        allowShowVoters: show_voters && (venueWasVotedFor || mainAttendance.hasAttendanceData),
        etaMap: mainEtaMap,
        ...mainAttendance,
    };

    const restaurantSource = restaurant_id ? venues[restaurant_id] : null;
    /** @type {RestaurantVenueViewModel | null} */
    let restaurantVenue = null;
    if (restaurantSource) {
        const restaurantAttendance = getEffectiveAttendanceState(attendance, restaurant_id, currUserId);
        const restaurantVotes = getDedupedVotesForVenue(votes, restaurant_id);
        const restaurantEtaMap = (attendance && restaurant_id && attendance[restaurant_id]?.eta) || {};
        restaurantVenue = {
            id: restaurant_id,
            name: restaurantSource.name,
            restaurantTime: restaurant_time,
            website: restaurantSource.website,
            image: restaurantSource.image,
            address: restaurantSource.address,
            dedupedVotes: restaurantVotes,
            allowShowVoters: show_voters && (restaurantVotes.length > 0 || restaurantAttendance.hasAttendanceData),
            etaMap: restaurantEtaMap,
            ...restaurantAttendance,
        };
    }

    return {
        mainVenue,
        restaurantVenue,
    };
}
