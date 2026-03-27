import { doc, updateDoc, deleteDoc, arrayUnion, deleteField } from "firebase/firestore";
import { db } from "../firebase";
import { assertCurrentUserPermission, PERMISSIONS } from "../permissions";

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
export async function reschedule_a_poll(poll_id, current_pub_id, new_pub_id) {
    assertCurrentUserPermission(PERMISSIONS.canCompletePoll, "rescheduling a poll");
    const docRef = doc(db, "polls", poll_id);
    await updateDoc(docRef, {
        previous_pubs: arrayUnion(current_pub_id),
        selected: new_pub_id,
    });
}
export async function add_new_pub_to_poll(selectedPub, poll_id, pub_parameters) {
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
    } catch (err) {
        console.error("Error adding document: ", err);
    }
}
export async function deletePubFromPoll(pollId, pubId) {
    assertCurrentUserPermission(PERMISSIONS.canAddPubToPoll, "deleting a pub from a poll");
    const docRef = doc(db, "polls", pollId);
    await updateDoc(docRef, {
        [`pubs.${pubId}`]: deleteField(),
    })
}
export async function complete_a_poll(key, poll_id) {
    assertCurrentUserPermission(PERMISSIONS.canCompletePoll, "completing a poll");
    const docRef = doc(db, "polls", poll_id);
    await updateDoc(docRef, {
        completed: true,
        selected: key,
    });
};
