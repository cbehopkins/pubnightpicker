// @ts-check

import { getEffectiveAttendanceState } from "./attendanceState";

/** @typedef {Record<string, string[]>} VotesMap */
/** @typedef {Record<string, { canCome?: string[], cannotCome?: string[] } | undefined>} AttendanceMap */

/**
 * @typedef {Object} PubParametersEntry
 * @property {string=} name
 * @property {string=} web_site
 * @property {string=} pubImage
 * @property {string=} address
 */

/** @typedef {Record<string, PubParametersEntry | undefined>} PubParametersMap */

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
 *   current_pub_id: string,
 *   restaurant_id: string | null | undefined,
 *   restaurant_time: string | null | undefined,
 *   pub_parameters: PubParametersMap,
 *   votes: VotesMap,
 *   attendance: AttendanceMap | null | undefined,
 *   currUserId: string | null | undefined,
 *   show_voters: boolean,
 * }} input
 * @returns {CurrentEventViewModel | null}
 */
export function buildCurrentEventViewModel({
    current_pub_id,
    restaurant_id,
    restaurant_time,
    pub_parameters,
    votes,
    attendance,
    currUserId,
    show_voters,
}) {
    const currentVenue = pub_parameters[current_pub_id];
    if (!currentVenue) {
        return null;
    }

    const mainAttendance = getEffectiveAttendanceState(attendance, current_pub_id, currUserId);
    const mainVotes = getDedupedVotesForVenue(votes, current_pub_id);
    const pubWasVotedFor = current_pub_id in votes || Boolean(votes.any);

    /** @type {EventVenueViewModel} */
    const mainVenue = {
        id: current_pub_id,
        name: currentVenue.name,
        website: currentVenue.web_site,
        image: currentVenue.pubImage,
        address: currentVenue.address,
        dedupedVotes: mainVotes,
        allowShowVoters: show_voters && (pubWasVotedFor || mainAttendance.hasAttendanceData),
        ...mainAttendance,
    };

    const restaurantSource = restaurant_id ? pub_parameters[restaurant_id] : null;
    /** @type {RestaurantVenueViewModel | null} */
    let restaurantVenue = null;
    if (restaurantSource) {
        const restaurantAttendance = getEffectiveAttendanceState(attendance, restaurant_id, currUserId);
        const restaurantVotes = getDedupedVotesForVenue(votes, restaurant_id);
        restaurantVenue = {
            id: restaurant_id,
            name: restaurantSource.name,
            restaurantTime: restaurant_time,
            website: restaurantSource.web_site,
            image: restaurantSource.pubImage,
            address: restaurantSource.address,
            dedupedVotes: restaurantVotes,
            allowShowVoters: show_voters && (restaurantVotes.length > 0 || restaurantAttendance.hasAttendanceData),
            ...restaurantAttendance,
        };
    }

    return {
        mainVenue,
        restaurantVenue,
    };
}
