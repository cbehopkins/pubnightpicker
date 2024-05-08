import { doc, updateDoc, deleteDoc, arrayUnion, deleteField } from "firebase/firestore";
import { db } from "../firebase";

export async function deletePoll(pollId) {
    const pollsDocRef = deleteDoc(doc(db, "polls", pollId));
    const openActionsDocRef = deleteDoc(doc(db, "open_actions", pollId));
    const votesDocRef = deleteDoc(doc(db, "votes", pollId));
    await pollsDocRef;
    await openActionsDocRef;
    await votesDocRef;
}
export async function reschedule_a_poll(poll_id, current_pub_id, new_pub_id) {
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
    const docRef = doc(db, "polls", pollId);
    await updateDoc(docRef, {
        [`pubs.${pubId}`]: deleteField(),
    })
}
export async function complete_a_poll(key, poll_id) {
    const docRef = doc(db, "polls", poll_id);
    await updateDoc(docRef, {
        completed: true,
        selected: key,
    });
};