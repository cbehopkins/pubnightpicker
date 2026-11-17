import { doc, updateDoc, deleteDoc, arrayUnion, deleteField } from "firebase/firestore";
import { auth, db } from "../firebase";
import { assertCurrentUserPermission, PERMISSIONS } from "../permissions";
import {
    logPollActionAudit,
    POLL_ACTION_ADD_VENUE,
    POLL_ACTION_COMPLETE,
    POLL_ACTION_DELETE_VENUE,
} from "./pollActionAudit";

export async function deleteMeet(meetId) {
    assertCurrentUserPermission(PERMISSIONS.canCreatePoll, "deleting a poll");
    const pollsDocRef = deleteDoc(doc(db, "polls", meetId));
    const openActionsDocRef = deleteDoc(doc(db, "open_actions", meetId));
    const votesDocRef = deleteDoc(doc(db, "votes", meetId));
    const attendanceDocRef = deleteDoc(doc(db, "attendance", meetId));
    await pollsDocRef;
    await openActionsDocRef;
    await votesDocRef;
    await attendanceDocRef;
}
export async function rescheduleMeet(meetId, currentVenueId, nextVenueId, restaurantId, restaurantTime) {
    assertCurrentUserPermission(PERMISSIONS.canCompletePoll, "rescheduling a poll");
    const docRef = doc(db, "polls", meetId);
    const payload = {
        selected: nextVenueId,
        completedByUid: deleteField(),
    };

    if (nextVenueId && currentVenueId && nextVenueId !== currentVenueId) {
        payload.previous_pubs = arrayUnion(currentVenueId);
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
export async function addVenueToMeet(selectedVenueId, meetId, venues, pollDate) {
    if (!selectedVenueId) {
        return;
    }
    assertCurrentUserPermission(PERMISSIONS.canAddPubToPoll, "adding a venue to a poll");
    try {
        const docRef = doc(db, "polls", meetId);
        const venueName = venues[selectedVenueId].name;
        await updateDoc(docRef, {
            [`pubs.${selectedVenueId}`]: {
                name: venueName,
            },
        });
        if (pollDate) {
            await logPollActionAudit(POLL_ACTION_ADD_VENUE, {
                pollId: meetId,
                pollDate,
                selectedVenueId,
                venueName,
            });
        }
    } catch (err) {
        console.error("Error adding document: ", err);
    }
}
export async function removeVenueFromMeet(meetId, venueId, pollDate, venueName) {
    assertCurrentUserPermission(PERMISSIONS.canAddPubToPoll, "deleting a venue from a poll");
    const docRef = doc(db, "polls", meetId);
    await updateDoc(docRef, {
        [`pubs.${venueId}`]: deleteField(),
    });

    if (pollDate) {
        try {
            await logPollActionAudit(POLL_ACTION_DELETE_VENUE, {
                pollId: meetId,
                pollDate,
                selectedVenueId: venueId,
                venueName,
            });
        } catch (auditError) {
            console.warn("Venue deleted from poll but audit logging failed", auditError);
        }
    }
}
export async function completeMeetVoting(selectedVenueId, meetId, pollDate, restaurantId, restaurantTime) {
    assertCurrentUserPermission(PERMISSIONS.canCompletePoll, "completing a poll");
    const completedByUid = auth.currentUser?.uid;
    if (!completedByUid) throw new Error("No authenticated poll completer.");
    const docRef = doc(db, "polls", meetId);
    const payload = {
        completed: true,
        selected: selectedVenueId,
        completedByUid,
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
            pollId: meetId,
            pollDate,
            selectedVenueId,
            restaurantId,
            restaurantTime,
        });
    } catch (auditError) {
        console.warn("Poll completed but audit logging failed", auditError);
    }
};
