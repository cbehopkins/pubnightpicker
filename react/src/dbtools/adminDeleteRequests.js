import { addDoc, collection, serverTimestamp } from "firebase/firestore";

import { db } from "../firebase";

/**
 * @param {{
 *   targetUid: string,
 *   targetEmail?: string | null,
 *   requestedByUid: string,
 *   requestedByEmail?: string | null,
 *   reason?: string,
 * }} payload
 */
export async function enqueueAdminAuthDeleteRequest(payload) {
    const targetUid = String(payload?.targetUid || "").trim();
    const requestedByUid = String(payload?.requestedByUid || "").trim();
    if (!targetUid) {
        throw new Error("Missing target uid for admin delete request");
    }
    if (!requestedByUid) {
        throw new Error("Missing requester uid for admin delete request");
    }

    const requestDoc = {
        schemaVersion: 1,
        targetUid,
        targetEmail: payload?.targetEmail || null,
        requestedByUid,
        requestedByEmail: payload?.requestedByEmail || null,
        reason: payload?.reason || "admin_user_delete",
        scrubbedAppData: true,
        status: "pending",
        createdAt: serverTimestamp(),
        updatedAt: serverTimestamp(),
    };

    const ref = await addDoc(collection(db, "admin_delete_requests"), requestDoc);
    return { id: ref.id };
}
