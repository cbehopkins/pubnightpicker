import { deleteField, doc, getDoc, onSnapshot, setDoc, updateDoc } from "firebase/firestore";
import { db } from "../firebase";

export const NOTIFICATION_REQ_COLLECTION = "notification_req";
export const NOTIFICATION_ACK_COLLECTION = "notification_ack";
export const NOTIFICATION_DIAGNOSTICS_DOC = "diagnostics";
export const NOTIFICATION_PUSH_TEST_DOC = "push_test";

export function createNotificationPingValue() {
    return Date.now();
}

/**
 * @param {string} documentId
 */
async function ensureNotificationDocs(documentId) {
    const reqRef = doc(db, NOTIFICATION_REQ_COLLECTION, documentId);
    await setDoc(reqRef, {}, { merge: true });
}

/**
 * @param {string} collectionName
 * @param {string} documentId
 * @param {{ [x: number]: import("@firebase/firestore").FieldValue; }} updateData
 */
async function updateFieldWithCreateFallback(collectionName, documentId, updateData) {
    const docRef = doc(db, collectionName, documentId);
    try {
        await updateDoc(docRef, updateData);
    } catch (error) {
        if (error?.code === "not-found") {
            await setDoc(docRef, updateData, { merge: true });
            return;
        }
        throw error;
    }
}

/**
 * @param {string} documentId
 * @param {any} eventKey
 */
export async function requestNotificationPing(documentId, eventKey, pingValue = createNotificationPingValue()) {
    await ensureNotificationDocs(documentId);
    await setDoc(
        doc(db, NOTIFICATION_REQ_COLLECTION, documentId),
        { [eventKey]: pingValue },
        { merge: true },
    );
    return pingValue;
}

export function waitForNotificationAck(documentId, eventKey, expectedValue, timeoutMs = 60000) {
    return new Promise((resolve, reject) => {
        let settled = false;

        const finish = (callback, value) => {
            if (settled) {
                return;
            }
            settled = true;
            clearTimeout(timeoutId);
            unsubscribe();
            callback(value);
        };

        const timeoutId = setTimeout(() => {
            finish(resolve, {
                acknowledged: false,
                timedOut: true,
            });
        }, timeoutMs);

        const unsubscribe = onSnapshot(
            doc(db, NOTIFICATION_ACK_COLLECTION, documentId),
            (snapshot) => {
                const data = snapshot.data();
                if (!data) {
                    return;
                }
                if (data[eventKey] === expectedValue) {
                    finish(resolve, {
                        acknowledged: true,
                        timedOut: false,
                    });
                }
            },
            (error) => finish(reject, error),
        );
    });
}

/**
 * @param {string} documentId
 * @param {any} eventKey
 */
export async function pingNotificationTool(documentId, eventKey, timeoutMs = 60000) {
    await ensureNotificationDocs(documentId);
    const pingValue = await requestNotificationPing(documentId, eventKey);
    const result = await waitForNotificationAck(documentId, eventKey, pingValue, timeoutMs);
    return {
        ...result,
        pingValue,
    };
}

/**
 * @param {string} documentId
 * @param {any} eventKey
 */
export async function clearNotificationPing(documentId, eventKey) {
    await ensureNotificationDocs(documentId);
    const docRef = doc(db, NOTIFICATION_REQ_COLLECTION, documentId);
    const snapshot = await getDoc(docRef);
    if (!snapshot.exists()) {
        return;
    }
    await updateFieldWithCreateFallback(NOTIFICATION_REQ_COLLECTION, documentId, { [eventKey]: deleteField() });
}
