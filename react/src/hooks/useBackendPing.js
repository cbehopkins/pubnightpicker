import { useEffect, useRef, useState } from "react";
import { onAuthStateChanged } from "firebase/auth";
import { auth } from "../firebase";
import { LastOrdersApiError, requestLastOrders } from "../utils/lastOrdersApi";

const initialState = { status: "idle", uid: null, message: "Not checked." };

export default function useBackendPing(timeoutMs = 10000) {
    const [result, setResult] = useState(initialState);
    const pendingRef = useRef(null);

    useEffect(() => {
        let previousUID = auth.currentUser?.uid;
        const unsubscribe = onAuthStateChanged(auth, (user) => {
            if (user?.uid !== previousUID) {
                previousUID = user?.uid;
                const pending = pendingRef.current;
                pendingRef.current = null;
                pending?.controller.abort();
                if (pending) clearTimeout(pending.timer);
                setResult(initialState);
            }
        });
        return () => {
            unsubscribe();
            const pending = pendingRef.current;
            pendingRef.current = null;
            pending?.controller.abort();
            if (pending) clearTimeout(pending.timer);
        };
    }, []);

    const runPing = async () => {
        if (pendingRef.current) return;
        const controller = new AbortController();
        const pending = {
            controller,
            uid: auth.currentUser?.uid,
            timer: setTimeout(() => controller.abort(new LastOrdersApiError("timeout", "Last Orders did not respond within the time limit.")), timeoutMs),
        };
        pendingRef.current = pending;
        setResult({ status: "checking", uid: null, message: "Checking..." });
        try {
            const requestID = crypto.randomUUID();
            const response = await requestLastOrders("api/ping", { body: { request_id: requestID }, signal: controller.signal });
            if (pendingRef.current !== pending) return;
            if (response?.status !== "ok" || typeof response.uid !== "string" || !response.uid ||
                response.uid !== pending.uid || response.request_id !== requestID) {
                throw new LastOrdersApiError("invalid_response", "Last Orders returned an unexpected response.");
            }
            setResult({ status: "ok", uid: response.uid, message: "Ping accepted." });
        } catch (error) {
            if (pendingRef.current !== pending) return;
            const failure = error instanceof LastOrdersApiError
                ? error
                : new LastOrdersApiError("backend_error", "Unable to complete the Last Orders ping.");
            setResult({ status: failure.code, uid: null, message: failure.message });
        } finally {
            clearTimeout(pending.timer);
            if (pendingRef.current === pending) pendingRef.current = null;
        }
    };

    return { ...result, runPing };
}
