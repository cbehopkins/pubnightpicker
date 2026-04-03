import { useCallback, useRef, useState } from "react";
import { clearNotificationPing, pingNotificationTool } from "../dbtools/notificationPings";

/**
 * Manages the lifecycle of a single notification ping request/ack pair.
 *
 * Returns status ("idle" | "checking" | "ok" | "timeout" | "error"),
 * the last acknowledged ping value, and actions to run or clear the ping.
 *
 * runPing resolves with the raw result and re-throws on unexpected errors
 * so callers can add toast/UI behaviour on top.
 *
 * clearPing resets state and re-throws on unexpected errors.
 */
export function useNotificationPing(documentId, eventKey, timeoutMs = 60000) {
    const [status, setStatus] = useState("idle");
    const [lastPingValue, setLastPingValue] = useState(null);
    const inFlightPingRef = useRef(null);

    const runPing = useCallback(async () => {
        if (inFlightPingRef.current) {
            return inFlightPingRef.current;
        }

        setStatus("checking");
        const pingPromise = (async () => {
            try {
                const result = await pingNotificationTool(documentId, eventKey, timeoutMs);
                if (result.acknowledged) {
                    setStatus("ok");
                    setLastPingValue(result.pingValue);
                } else if (result.timedOut) {
                    setStatus("timeout");
                } else {
                    setStatus("error");
                }
                return result;
            } catch (error) {
                setStatus("error");
                throw error;
            } finally {
                inFlightPingRef.current = null;
            }
        })();

        inFlightPingRef.current = pingPromise;
        return pingPromise;
    }, [documentId, eventKey, timeoutMs]);

    const clearPing = useCallback(async () => {
        try {
            await clearNotificationPing(documentId, eventKey);
            setStatus("idle");
            setLastPingValue(null);
        } catch (error) {
            setStatus("error");
            throw error;
        }
    }, [documentId, eventKey]);

    return { status, lastPingValue, runPing, clearPing };
}
