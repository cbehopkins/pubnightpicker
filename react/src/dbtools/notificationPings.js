import { deleteField, doc, getDoc, onSnapshot, setDoc, updateDoc } from "firebase/firestore";
import { db } from "../firebase";

export const NOTIFICATION_REQ_COLLECTION = "notification_req";
export const NOTIFICATION_ACK_COLLECTION = "notification_ack";
export const NOTIFICATION_DIAGNOSTICS_DOC = "diagnostics";

export function createNotificationPingValue() {
    return Date.now();
}

export async function requestNotificationPing(documentId, eventKey, pingValue = createNotificationPingValue()) {
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

export async function pingNotificationTool(documentId, eventKey, timeoutMs = 60000) {
    const pingValue = await requestNotificationPing(documentId, eventKey);
    const result = await waitForNotificationAck(documentId, eventKey, pingValue, timeoutMs);
    return {
        ...result,
        pingValue,
    };
}

export async function clearNotificationPing(documentId, eventKey) {
    const collections = [NOTIFICATION_REQ_COLLECTION, NOTIFICATION_ACK_COLLECTION];
    const clearFieldPromiseList = collections.map(async (collectionName) => {
        const docRef = doc(db, collectionName, documentId);
        const snapshot = await getDoc(docRef);
        if (!snapshot.exists()) {
            return;
        }
        await updateDoc(docRef, { [eventKey]: deleteField() });
    });

    await Promise.all(clearFieldPromiseList);
}
