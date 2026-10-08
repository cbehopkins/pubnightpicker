import { useCallback, useEffect, useRef, useState } from "react";
import { doc, onSnapshot, setDoc } from "firebase/firestore";
import { db } from "../firebase";

export const TEST_EMAIL_REQ_FIELD = "testEmailReq";
export const TEST_EMAIL_ACK_FIELD = "testEmailAck";

export function resolveTestEmailAddress(userData) {
    for (const field of ["notificationEmail", "email"]) {
        const value = userData?.[field];
        if (typeof value === "string" && value.trim()) {
            return value.trim();
        }
    }
    return null;
}

/**
 * @param {string | null | undefined} uid
 * @param {number} timeoutMs
 */
export default function useTestEmailRequest(uid, timeoutMs = 60000) {
    const [status, setStatus] = useState("idle");
    const [address, setAddress] = useState(null);
    // Only a request made in this session counts, so a stale req==ack does not show "sent".
    const pendingRequestRef = useRef(null);
    const timeoutRef = useRef(null);

    const clearPendingTimeout = useCallback(() => {
        if (timeoutRef.current !== null) {
            clearTimeout(timeoutRef.current);
            timeoutRef.current = null;
        }
    }, []);

    useEffect(() => {
        if (!uid) {
            return undefined;
        }
        return onSnapshot(
            doc(db, "users", uid),
            (snapshot) => {
                const data = snapshot.data();
                setAddress(resolveTestEmailAddress(data));
                const pending = pendingRequestRef.current;
                if (pending !== null && data?.[TEST_EMAIL_ACK_FIELD] === pending) {
                    pendingRequestRef.current = null;
                    clearPendingTimeout();
                    setStatus("sent");
                }
            },
            (error) => {
                console.error(error);
                setStatus("error");
            },
        );
    }, [uid, clearPendingTimeout]);

    useEffect(() => clearPendingTimeout, [clearPendingTimeout]);

    const sendTestEmail = useCallback(async () => {
        if (!uid || pendingRequestRef.current !== null) {
            return;
        }
        const requestId = crypto.randomUUID();
        pendingRequestRef.current = requestId;
        setStatus("sending");
        timeoutRef.current = setTimeout(() => {
            timeoutRef.current = null;
            pendingRequestRef.current = null;
            setStatus("timeout");
        }, timeoutMs);

        try {
            await setDoc(doc(db, "users", uid), { [TEST_EMAIL_REQ_FIELD]: requestId }, { merge: true });
        } catch (error) {
            console.error(error);
            clearPendingTimeout();
            pendingRequestRef.current = null;
            setStatus("error");
        }
    }, [uid, timeoutMs, clearPendingTimeout]);

    return { status, address, sendTestEmail };
}
