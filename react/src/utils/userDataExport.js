import { collection, doc, getDoc, getDocs, query, where } from "firebase/firestore";
import { db } from "../firebase";
import getUserDoc from "../dbtools/getUserDoc";

/**
 * @param {string} userId
 * @param {Record<string, string[]>} votesData
 */
function extractVotesForUser(userId, votesData) {
    return Object.entries(votesData || {}).reduce((acc, [venueId, voterIds]) => {
        if (Array.isArray(voterIds) && voterIds.includes(userId)) {
            acc[venueId] = true;
        }
        return acc;
    }, {});
}

/**
 * @param {string} userId
 * @param {Record<string, { canCome?: string[], cannotCome?: string[] } | undefined>} attendanceData
 */
function extractAttendanceForUser(userId, attendanceData) {
    return Object.entries(attendanceData || {}).reduce((acc, [venueId, entry]) => {
        if (!entry) {
            return acc;
        }

        const canCome = Array.isArray(entry.canCome) && entry.canCome.includes(userId);
        const cannotCome = Array.isArray(entry.cannotCome) && entry.cannotCome.includes(userId);

        if (canCome || cannotCome) {
            acc[venueId] = {
                canCome,
                cannotCome,
            };
        }
        return acc;
    }, {});
}

/**
 * @param {string} filename
 * @param {unknown} payload
 */
export function downloadJson(filename, payload) {
    const blob = new Blob([JSON.stringify(payload, null, 2)], { type: "application/json" });
    const objectUrl = URL.createObjectURL(blob);
    const link = document.createElement("a");
    link.href = objectUrl;
    link.download = filename;
    document.body.appendChild(link);
    link.click();
    link.remove();
    URL.revokeObjectURL(objectUrl);
}

/**
 * @param {unknown} value
 */
function toTimestampIso(value) {
    const typedValue = /** @type {{ toDate?: () => Date } | string | null | undefined} */ (value);
    if (!value) {
        return null;
    }
    if (typedValue && typeof typedValue === "object" && typeof typedValue.toDate === "function") {
        return typedValue.toDate().toISOString();
    }
    if (typeof typedValue === "string") {
        return typedValue;
    }
    return null;
}

/**
 * @param {string | null | undefined} venueId
 * @param {Record<string, unknown> | null | undefined} pollVenueData
 * @param {Record<string, Record<string, unknown>>} pubsById
 */
function buildVenueDetails(venueId, pollVenueData, pubsById) {
    if (!venueId) {
        return null;
    }

    if (venueId === "any") {
        return {
            venueId,
            name: "Any venue",
            pollEntry: null,
            venueRecord: null,
        };
    }

    return {
        venueId,
        name: pollVenueData?.name || pubsById[venueId]?.name || null,
        pollEntry: pollVenueData || null,
        venueRecord: pubsById[venueId] || null,
    };
}

/**
 * @param {string} pollId
 * @param {Record<string, Record<string, unknown>>} pollsById
 * @param {Record<string, Record<string, unknown>>} pubsById
 */
function buildPollDetails(pollId, pollsById, pubsById) {
    const pollData = /** @type {{ date?: string, completed?: boolean, selected?: string, restaurant?: string, restaurant_time?: string, pubs?: Record<string, Record<string, unknown>> } | null} */ (pollsById[pollId] || null);
    const pollPubs = /** @type {Record<string, Record<string, unknown>>} */ (pollData?.pubs || {});
    const selectedVenueId = typeof pollData?.selected === "string" ? pollData.selected : "";
    const restaurantVenueId = typeof pollData?.restaurant === "string" ? pollData.restaurant : "";
    const venueEntries = Object.entries(pollPubs)
        .filter(([venueId]) => venueId !== "any")
        .map(([venueId, entry]) => buildVenueDetails(venueId, entry, pubsById));

    return {
        pollId,
        date: pollData?.date || null,
        completed: Boolean(pollData?.completed),
        selectedVenue: buildVenueDetails(selectedVenueId || null, pollPubs[selectedVenueId], pubsById),
        restaurantVenue: buildVenueDetails(restaurantVenueId || null, pollPubs[restaurantVenueId], pubsById),
        restaurantTime: pollData?.restaurant_time || null,
        venuesOnPoll: venueEntries,
        rawPoll: pollData,
    };
}

/**
 * @param {Record<string, Record<string, true>>} votesByPoll
 * @param {Record<string, Record<string, unknown>>} pollsById
 * @param {Record<string, Record<string, unknown>>} pubsById
 */
function buildExpandedVotes(votesByPoll, pollsById, pubsById) {
    return Object.entries(votesByPoll).reduce((acc, [pollId, pollVotes]) => {
        acc[pollId] = {
            event: buildPollDetails(pollId, pollsById, pubsById),
            votes: Object.keys(pollVotes).map((venueId) => ({
                venue: buildVenueDetails(
                    venueId,
                    pollsById[pollId]?.pubs?.[venueId],
                    pubsById
                ),
                selected: true,
            })),
        };
        return acc;
    }, {});
}

/**
 * @param {Record<string, Record<string, { canCome: boolean, cannotCome: boolean }>>} attendanceByPoll
 * @param {Record<string, Record<string, unknown>>} pollsById
 * @param {Record<string, Record<string, unknown>>} pubsById
 */
function buildExpandedAttendance(attendanceByPoll, pollsById, pubsById) {
    return Object.entries(attendanceByPoll).reduce((acc, [pollId, pollAttendance]) => {
        acc[pollId] = {
            event: buildPollDetails(pollId, pollsById, pubsById),
            attendance: Object.entries(pollAttendance).map(([venueId, status]) => ({
                venue: buildVenueDetails(
                    venueId,
                    pollsById[pollId]?.pubs?.[venueId],
                    pubsById
                ),
                status,
            })),
        };
        return acc;
    }, {});
}

/**
 * @param {Record<string, Record<string, true>>} votesByPoll
 * @param {Record<string, Record<string, { canCome: boolean, cannotCome: boolean }>>} attendanceByPoll
 * @param {Record<string, Record<string, unknown>>} pollsById
 * @param {Record<string, Record<string, unknown>>} pubsById
 */
function buildLookupSection(votesByPoll, attendanceByPoll, pollsById, pubsById) {
    const pollIds = [...new Set([
        ...Object.keys(votesByPoll || {}),
        ...Object.keys(attendanceByPoll || {}),
    ])];

    const venueIds = new Set();
    pollIds.forEach((pollId) => {
        const voteVenueIds = Object.keys(votesByPoll?.[pollId] || {});
        const attendanceVenueIds = Object.keys(attendanceByPoll?.[pollId] || {});
        const pollVenueIds = Object.keys(pollsById?.[pollId]?.pubs || {});

        [...voteVenueIds, ...attendanceVenueIds, ...pollVenueIds].forEach((venueId) => {
            if (venueId) {
                venueIds.add(venueId);
            }
        });
    });

    return {
        polls: pollIds.reduce((acc, pollId) => {
            acc[pollId] = buildPollDetails(pollId, pollsById, pubsById);
            return acc;
        }, {}),
        venues: [...venueIds].reduce((acc, venueId) => {
            if (venueId === "any") {
                acc[venueId] = {
                    venueId,
                    name: "Any venue",
                    venueRecord: null,
                };
                return acc;
            }

            acc[venueId] = {
                venueId,
                name: pubsById?.[venueId]?.name || null,
                venueRecord: pubsById?.[venueId] || null,
            };
            return acc;
        }, {}),
    };
}

/**
 * @param {string} uid
 */
export async function buildCurrentUserDataExport(uid) {
    const [privateUserDoc, publicUserSnapshot, votesSnapshot, attendanceSnapshot, pollsSnapshot, pubsSnapshot, messagesSnapshot] = await Promise.all([
        getUserDoc(uid),
        getDoc(doc(db, "user-public", uid)),
        getDocs(collection(db, "votes")),
        getDocs(collection(db, "attendance")),
        getDocs(collection(db, "polls")),
        getDocs(collection(db, "pubs")),
        getDocs(query(collection(db, "messages"), where("uid", "==", uid))),
    ]);

    const privateUserData = privateUserDoc ? privateUserDoc.data() : null;
    const publicUserData = publicUserSnapshot.exists() ? publicUserSnapshot.data() : null;
    const pollsById = pollsSnapshot.docs.reduce((acc, pollDoc) => {
        acc[pollDoc.id] = pollDoc.data();
        return acc;
    }, {});
    const pubsById = pubsSnapshot.docs.reduce((acc, pubDoc) => {
        acc[pubDoc.id] = pubDoc.data();
        return acc;
    }, {});

    const votesByPoll = votesSnapshot.docs.reduce((acc, voteDoc) => {
        const userVotes = extractVotesForUser(uid, voteDoc.data());
        if (Object.keys(userVotes).length > 0) {
            acc[voteDoc.id] = userVotes;
        }
        return acc;
    }, {});

    const attendanceByPoll = attendanceSnapshot.docs.reduce((acc, attendanceDoc) => {
        const userAttendance = extractAttendanceForUser(uid, attendanceDoc.data());
        if (Object.keys(userAttendance).length > 0) {
            acc[attendanceDoc.id] = userAttendance;
        }
        return acc;
    }, {});

    const rawMessages = messagesSnapshot.docs.map((messageDoc) => ({
        id: messageDoc.id,
        ...messageDoc.data(),
    }));

    const expandedMessages = rawMessages.map((message) => {
        const typedMessage = /** @type {{ id: string, text?: string, createdAt?: unknown, name?: string, uid?: string }} */ (message);
        return {
            id: typedMessage.id,
            text: typedMessage.text || "",
            sentAt: toTimestampIso(typedMessage.createdAt),
            nameAtSendTime: typedMessage.name || null,
            uid: typedMessage.uid || null,
        };
    });

    return {
        exportedAt: new Date().toISOString(),
        userId: uid,
        lookup: buildLookupSection(votesByPoll, attendanceByPoll, pollsById, pubsById),
        raw: {
            profile: {
                private: privateUserData,
                public: publicUserData,
            },
            votes: votesByPoll,
            attendance: attendanceByPoll,
            messages: rawMessages,
        },
        expanded: {
            profile: {
                preferredName: publicUserData?.name || privateUserData?.name || null,
                email: privateUserData?.email || null,
                notificationPreferences: {
                    notificationEmail: privateUserData?.notificationEmail || null,
                    notificationEmailEnabled: privateUserData?.notificationEmailEnabled || false,
                    openPollEmailEnabled: privateUserData?.openPollEmailEnabled || false,
                    votesVisible: publicUserData?.votesVisible ?? privateUserData?.votesVisible ?? true,
                },
                publicProfile: publicUserData,
            },
            votes: buildExpandedVotes(votesByPoll, pollsById, pubsById),
            attendance: buildExpandedAttendance(attendanceByPoll, pollsById, pubsById),
            messages: expandedMessages,
        },
    };
}

/**
 * @param {string} uid
 */
export async function downloadCurrentUserDataExport(uid) {
    const payload = await buildCurrentUserDataExport(uid);
    downloadJson(`pub-night-picker-data-${uid}.json`, payload);
    return payload;
}
