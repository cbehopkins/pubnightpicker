import { doc, updateDoc, deleteDoc, arrayUnion, deleteField } from "firebase/firestore";
import { db } from "../firebase";
import { assertCurrentUserPermission, PERMISSIONS } from "../permissions";
import {
    logPollActionAudit,
    POLL_ACTION_ADD_VENUE,
    POLL_ACTION_COMPLETE,
    POLL_ACTION_DELETE_VENUE,
} from "./pollActionAudit";

export async function deletePoll(pollId) {
    assertCurrentUserPermission(PERMISSIONS.canCreatePoll, "deleting a poll");
    const pollsDocRef = deleteDoc(doc(db, "polls", pollId));
    const openActionsDocRef = deleteDoc(doc(db, "open_actions", pollId));
    const votesDocRef = deleteDoc(doc(db, "votes", pollId));
    const attendanceDocRef = deleteDoc(doc(db, "attendance", pollId));
    await pollsDocRef;
    await openActionsDocRef;
    await votesDocRef;
    await attendanceDocRef;
}
export async function reschedule_a_poll(poll_id, current_pub_id, new_pub_id, restaurantId, restaurantTime) {
    assertCurrentUserPermission(PERMISSIONS.canCompletePoll, "rescheduling a poll");
    const docRef = doc(db, "polls", poll_id);
    const payload = {
        selected: new_pub_id,
    };

    if (new_pub_id && current_pub_id && new_pub_id !== current_pub_id) {
        payload.previous_pubs = arrayUnion(current_pub_id);
    }

    if (restaurantId) {
        payload.restaurant = restaurantId;
        payload.restaurant_time = restaurantTime || deleteField();
    } else {
        payload.restaurant = deleteField();
        payload.restaurant_time = deleteField();
    }

    await updateDoc(docRef, payload);
}
export async function add_new_pub_to_poll(selectedPub, poll_id, pub_parameters, pollDate) {
    if (!selectedPub) {
        return;
    }
    assertCurrentUserPermission(PERMISSIONS.canAddPubToPoll, "adding a pub to a poll");
    try {
        const docRef = doc(db, "polls", poll_id);
        const pubName = pub_parameters[selectedPub].name;
        await updateDoc(docRef, {
            [`pubs.${selectedPub}`]: {
                name: pubName,
            },
        });
        if (pollDate) {
            await logPollActionAudit(POLL_ACTION_ADD_VENUE, {
                pollId: poll_id,
                pollDate,
                selectedVenueId: selectedPub,
                venueName: pubName,
            });
        }
    } catch (err) {
        console.error("Error adding document: ", err);
    }
}
export async function deletePubFromPoll(pollId, pubId, pollDate, pubName) {
    assertCurrentUserPermission(PERMISSIONS.canAddPubToPoll, "deleting a pub from a poll");
    const docRef = doc(db, "polls", pollId);
    await updateDoc(docRef, {
        [`pubs.${pubId}`]: deleteField(),
    });

    if (pollDate) {
        try {
            await logPollActionAudit(POLL_ACTION_DELETE_VENUE, {
                pollId,
                pollDate,
                selectedVenueId: pubId,
                venueName: pubName,
            });
        } catch (auditError) {
            console.warn("Pub deleted from poll but audit logging failed", auditError);
        }
    }
}
export async function complete_a_poll(key, poll_id, pollDate, restaurantId, restaurantTime) {
    assertCurrentUserPermission(PERMISSIONS.canCompletePoll, "completing a poll");
    const docRef = doc(db, "polls", poll_id);
    const payload = {
        completed: true,
        selected: key,
    };

    if (restaurantId) {
        payload.restaurant = restaurantId;
        if (restaurantTime) {
            payload.restaurant_time = restaurantTime;
        }
    }

    await updateDoc(docRef, {
        ...payload,
    });

    try {
        await logPollActionAudit(POLL_ACTION_COMPLETE, {
            pollId: poll_id,
            pollDate,
            selectedVenueId: key,
            restaurantId,
            restaurantTime,
        });
    } catch (auditError) {
        console.warn("Poll completed but audit logging failed", auditError);
    }
};
