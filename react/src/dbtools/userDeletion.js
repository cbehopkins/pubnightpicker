import {
    arrayRemove,
    collection,
    deleteDoc,
    deleteField,
    doc,
    getDocs,
    query,
    serverTimestamp,
    setDoc,
    updateDoc,
    where,
} from "firebase/firestore";
import { db } from "../firebase";

export const DELETED_USER_UID = "__deleted_user__";
export const DELETED_USER_NAME = "Deleted User";
export const DELETED_USER_MESSAGE = "{user deleted}";

/**
 * @typedef {Object} DeletionSummary
 * @property {number} votesDocsUpdated
 * @property {number} attendanceDocsUpdated
 * @property {number} messagesAnonymized
 * @property {number} pushEndpointsDeleted
 * @property {number} roleDocsUpdated
 * @property {number} profileDocsDeleted
 */

/**
 * @param {{
 *  votes?: { updatedDocs?: number },
 *  attendance?: { updatedDocs?: number },
 *  messages?: { updatedDocs?: number },
 *  pushEndpoints?: { deletedDocs?: number },
 *  roles?: { updatedRoleDocs?: number },
 *  profiles?: { deleted?: boolean },
 * }} result
 * @returns {DeletionSummary}
 */
export function summarizeDeletionResult(result) {
    return {
        votesDocsUpdated: Number(result?.votes?.updatedDocs || 0),
        attendanceDocsUpdated: Number(result?.attendance?.updatedDocs || 0),
        messagesAnonymized: Number(result?.messages?.updatedDocs || 0),
        pushEndpointsDeleted: Number(result?.pushEndpoints?.deletedDocs || 0),
        roleDocsUpdated: Number(result?.roles?.updatedRoleDocs || 0),
        profileDocsDeleted: result?.profiles?.deleted ? 2 : 0,
    };
}

/**
 * @param {DeletionSummary} summary
 */
export function deletionSummaryLines(summary) {
    return [
        `Votes docs updated: ${summary.votesDocsUpdated}`,
        `Attendance docs updated: ${summary.attendanceDocsUpdated}`,
        `Messages anonymized: ${summary.messagesAnonymized}`,
        `Push endpoints deleted: ${summary.pushEndpointsDeleted}`,
        `Role docs updated: ${summary.roleDocsUpdated}`,
        `Profile docs deleted: ${summary.profileDocsDeleted}`,
    ];
}

/**
 * @param {Record<string, unknown>} votesDocData
 * @param {string} targetUid
 */
function buildVotesUpdate(votesDocData, targetUid) {
    return Object.entries(votesDocData || {}).reduce((payload, [venueId, voterIds]) => {
        if (Array.isArray(voterIds) && voterIds.includes(targetUid)) {
            payload[venueId] = arrayRemove(targetUid);
        }
        return payload;
    }, {});
}

/**
 * @param {Record<string, unknown>} attendanceDocData
 * @param {string} targetUid
 */
function buildAttendanceUpdate(attendanceDocData, targetUid) {
    return Object.entries(attendanceDocData || {}).reduce((payload, [venueId, entry]) => {
        if (!entry || typeof entry !== "object") {
            return payload;
        }

        const typedEntry = /** @type {{ canCome?: string[], cannotCome?: string[], eta?: Record<string, unknown> }} */ (entry);

        if (Array.isArray(typedEntry.canCome) && typedEntry.canCome.includes(targetUid)) {
            payload[`${venueId}.canCome`] = arrayRemove(targetUid);
        }
        if (Array.isArray(typedEntry.cannotCome) && typedEntry.cannotCome.includes(targetUid)) {
            payload[`${venueId}.cannotCome`] = arrayRemove(targetUid);
        }
        if (typedEntry.eta && typeof typedEntry.eta === "object" && targetUid in typedEntry.eta) {
            payload[`${venueId}.eta.${targetUid}`] = deleteField();
        }
        return payload;
    }, {});
}

/**
 * @param {string} targetUid
 */
export async function removeUserFromVotes(targetUid) {
    const votesSnapshot = await getDocs(collection(db, "votes"));
    let updatedDocs = 0;

    for (const voteDoc of votesSnapshot.docs) {
        const payload = buildVotesUpdate(voteDoc.data(), targetUid);
        if (Object.keys(payload).length === 0) {
            continue;
        }
        await updateDoc(voteDoc.ref, payload);
        updatedDocs += 1;
    }

    return { updatedDocs };
}

/**
 * @param {string} targetUid
 */
export async function removeUserFromAttendance(targetUid) {
    const attendanceSnapshot = await getDocs(collection(db, "attendance"));
    let updatedDocs = 0;

    for (const attendanceDoc of attendanceSnapshot.docs) {
        const payload = buildAttendanceUpdate(attendanceDoc.data(), targetUid);
        if (Object.keys(payload).length === 0) {
            continue;
        }
        await updateDoc(attendanceDoc.ref, payload);
        updatedDocs += 1;
    }

    return { updatedDocs };
}

/**
 * @param {string} targetUid
 */
export async function anonymizeUserMessages(targetUid) {
    const messagesSnapshot = await getDocs(query(collection(db, "messages"), where("uid", "==", targetUid)));
    let updatedDocs = 0;

    for (const messageDoc of messagesSnapshot.docs) {
        await updateDoc(messageDoc.ref, {
            uid: DELETED_USER_UID,
            name: DELETED_USER_NAME,
            text: DELETED_USER_MESSAGE,
            deletedUserUid: targetUid,
            deletedAt: serverTimestamp(),
        });
        updatedDocs += 1;
    }

    return { updatedDocs };
}

/**
 * @param {string} targetUid
 */
export async function deleteUserPushEndpoints(targetUid) {
    const pushSnapshot = await getDocs(collection(db, "users", targetUid, "push_endpoints"));
    let deletedDocs = 0;

    for (const endpointDoc of pushSnapshot.docs) {
        await deleteDoc(endpointDoc.ref);
        deletedDocs += 1;
    }

    return { deletedDocs };
}

/**
 * @param {string} targetUid
 */
export async function deleteUserProfileDocs(targetUid) {
    await Promise.all([
        deleteDoc(doc(db, "users", targetUid)),
        deleteDoc(doc(db, "user-public", targetUid)),
    ]);

    return { deleted: true };
}

/**
 * @param {string} targetUid
 */
export async function removeUserFromRoles(targetUid) {
    const rolesSnapshot = await getDocs(collection(db, "roles"));
    let updatedRoleDocs = 0;

    for (const roleDoc of rolesSnapshot.docs) {
        await setDoc(roleDoc.ref, { [targetUid]: deleteField() }, { merge: true });
        updatedRoleDocs += 1;
    }

    return { updatedRoleDocs };
}

/**
 * @param {string} targetUid
 * @param {{ removeRoles?: boolean }} [options]
 */
export async function deleteUserAppData(targetUid, options = {}) {
    const removeRoles = options.removeRoles === true;

    const [votes, attendance, messages, pushEndpoints] = await Promise.all([
        removeUserFromVotes(targetUid),
        removeUserFromAttendance(targetUid),
        anonymizeUserMessages(targetUid),
        deleteUserPushEndpoints(targetUid),
    ]);

    const roles = removeRoles ? await removeUserFromRoles(targetUid) : { updatedRoleDocs: 0 };
    const profiles = await deleteUserProfileDocs(targetUid);

    return {
        votes,
        attendance,
        messages,
        pushEndpoints,
        roles,
        profiles,
    };
}
