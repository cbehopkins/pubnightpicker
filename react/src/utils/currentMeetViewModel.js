// @ts-check

import { getEffectiveAttendanceState } from "./attendanceState";

/** @typedef {{ total: number, voterUids: string[] }} VotingOption */
/** @typedef {Record<string, VotingOption | undefined>} VotesMap */
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
 * @typedef {Object} MeetVenueViewModel
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
 * @typedef {MeetVenueViewModel & {
 *   restaurantTime?: string | null | undefined
 * }} RestaurantVenueViewModel
 */

/**
 * @typedef {Object} CurrentMeetViewModel
 * @property {MeetVenueViewModel} mainVenue
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
        allVotes.push(...votes[venueId].voterUids);
    }
    if (venueId && votes.any) {
        allVotes.push(...votes.any.voterUids);
    }

    return [...new Set(allVotes)];
}

/**
 * @param {{
 *   currentVenueId: string,
 *   restaurantId: string | null | undefined,
 *   restaurantTime: string | null | undefined,
 *   venues: VenueMap,
 *   votes: VotesMap,
 *   attendance: AttendanceMap | null | undefined,
 *   currUserId: string | null | undefined,
 *   showVoters: boolean,
 * }} input
 * @returns {CurrentMeetViewModel | null}
 */
export function buildCurrentMeetViewModel({
    currentVenueId,
    restaurantId,
    restaurantTime,
    venues,
    votes,
    attendance,
    currUserId,
    showVoters,
}) {
    const currentVenue = venues[currentVenueId];
    if (!currentVenue) {
        return null;
    }

    const mainAttendance = getEffectiveAttendanceState(attendance, currentVenueId, currUserId);
    const mainVotes = getDedupedVotesForVenue(votes, currentVenueId);
    const venueWasVotedFor = currentVenueId in votes || Boolean(votes.any);
    const mainEtaMap = (attendance && attendance[currentVenueId]?.eta) || {};

    /** @type {MeetVenueViewModel} */
    const mainVenue = {
        id: currentVenueId,
        name: currentVenue.name,
        website: currentVenue.website,
        image: currentVenue.image,
        address: currentVenue.address,
        dedupedVotes: mainVotes,
        allowShowVoters: showVoters && (venueWasVotedFor || mainAttendance.hasAttendanceData),
        etaMap: mainEtaMap,
        ...mainAttendance,
    };

    const restaurantSource = restaurantId ? venues[restaurantId] : null;
    /** @type {RestaurantVenueViewModel | null} */
    let restaurantVenue = null;
    if (restaurantSource) {
        const restaurantAttendance = getEffectiveAttendanceState(attendance, restaurantId, currUserId);
        const restaurantVotes = getDedupedVotesForVenue(votes, restaurantId);
        const restaurantEtaMap = (attendance && restaurantId && attendance[restaurantId]?.eta) || {};
        restaurantVenue = {
            id: restaurantId,
            name: restaurantSource.name,
            restaurantTime,
            website: restaurantSource.website,
            image: restaurantSource.image,
            address: restaurantSource.address,
            dedupedVotes: restaurantVotes,
            allowShowVoters: showVoters && (restaurantVotes.length > 0 || restaurantAttendance.hasAttendanceData),
            etaMap: restaurantEtaMap,
            ...restaurantAttendance,
        };
    }

    return {
        mainVenue,
        restaurantVenue,
    };
}
