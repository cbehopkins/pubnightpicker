import { doc, serverTimestamp, setDoc } from "firebase/firestore";
import { auth, db } from "../firebase";

export const POLL_ACTION_AUDIT_COLLECTION = "poll_action_audit";
export const POLL_ACTION_CREATE = "create";
export const POLL_ACTION_COMPLETE = "complete";
export const POLL_ACTION_ADD_VENUE = "addVenue";
export const POLL_ACTION_DELETE_VENUE = "deleteVenue";

const ACTIONS_REQUIRING_SELECTED_VENUE = new Set([
    POLL_ACTION_COMPLETE,
    POLL_ACTION_ADD_VENUE,
    POLL_ACTION_DELETE_VENUE,
]);

const VALID_POLL_ACTIONS = new Set([
    POLL_ACTION_CREATE,
    POLL_ACTION_COMPLETE,
    POLL_ACTION_ADD_VENUE,
    POLL_ACTION_DELETE_VENUE,
]);

function getCurrentActorUid() {
    const actorUid = auth.currentUser?.uid;
    if (!actorUid) {
        throw new Error("No authenticated user available for poll action audit logging.");
    }
    return actorUid;
}

function buildAuditDocId(pollId, actionType, nowMs = Date.now()) {
    const timestampMicros = nowMs * 1000;
    return `${pollId}_${actionType}_${timestampMicros}`;
}

export async function logPollActionAudit(actionType, payload) {
    const { pollId, pollDate, selectedVenueId, venueName, restaurantId, restaurantTime } = payload;
    if (!pollId) {
        throw new Error("pollId is required for poll action audit logging.");
    }
    if (!pollDate) {
        throw new Error("pollDate is required for poll action audit logging.");
    }
    if (!VALID_POLL_ACTIONS.has(actionType)) {
        throw new Error(`Unsupported poll action type: ${actionType}`);
    }
    if (ACTIONS_REQUIRING_SELECTED_VENUE.has(actionType) && !selectedVenueId) {
        throw new Error(`selectedVenueId is required when logging poll action ${actionType}.`);
    }

    const actorUid = getCurrentActorUid();
    const auditDocId = buildAuditDocId(pollId, actionType);
    const documentPayload = {
        pollId,
        actionType,
        actorUid,
        at: serverTimestamp(),
        pollDate,
    };

    if (selectedVenueId) {
        documentPayload.selectedVenueId = selectedVenueId;
    }
    if (venueName) {
        documentPayload.venueName = venueName;
    }
    if (restaurantId) {
        documentPayload.restaurantId = restaurantId;
    }
    if (restaurantTime) {
        documentPayload.restaurantTime = restaurantTime;
    }

    await setDoc(doc(db, POLL_ACTION_AUDIT_COLLECTION, auditDocId), documentPayload);
}
