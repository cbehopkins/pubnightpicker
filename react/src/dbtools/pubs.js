import { addDoc, collection, deleteDoc, doc, updateDoc } from "firebase/firestore";
import { db } from "../firebase";
import { assertCurrentUserPermission, PERMISSIONS } from "../permissions";

export async function addNewPub(pubParams) {
    assertCurrentUserPermission(PERMISSIONS.canManagePubs, "creating a pub");
    await addDoc(collection(db, "pubs"), pubParams);
}

export async function modifyPub(id, pubParams) {
    assertCurrentUserPermission(PERMISSIONS.canManagePubs, "editing a pub");
    const docRef = doc(db, "pubs", id);
    await updateDoc(docRef, pubParams);
}

export async function deletePub(id) {
    assertCurrentUserPermission(PERMISSIONS.canManagePubs, "deleting a pub");
    await deleteDoc(doc(db, "pubs", id));
}
