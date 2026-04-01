import { setDoc, updateDoc } from "firebase/firestore";

/** @typedef {import("firebase/firestore").DocumentReference} DocumentReference */

/**
 * @typedef {Error & {
 *   code?: string;
 * }} FirestoreLikeError
 */

/**
 * Checks if error is due to missing Firestore document
 * Common when trying to update a document that doesn't exist yet
 * @param {FirestoreLikeError | null | undefined} error - The error to check
 * @returns {boolean} True if error is "document not found"
 */
export function isMissingDeterminingDocError(error) {
    const code = error?.code;
    const message = String(error?.message || "").toLowerCase();
    return code === "not-found"
        || code === "NOT_FOUND"
        || message.includes("no entity to update");
}

/**
 * Wrapper for Firestore document updates with auto-initialization
 * If document doesn't exist, creates it first (with merge: true) then performs update
 * Reusable pattern for any Firestore collection
 * 
 * @param {DocumentReference} docRef - Firestore document reference
 * @param {Object} payload - Data to update
 * @returns {Promise<void>}
 */
export async function updateDocWithInitialization(docRef, payload) {
    try {
        await updateDoc(docRef, payload);
    } catch (error) {
        if (!isMissingDeterminingDocError(error)) {
            throw error;
        }

        // Document doesn't exist; create it first, then update
        await setDoc(docRef, {}, { merge: true });
        await updateDoc(docRef, payload);
    }
}
