import { useEffect } from "react";
import { useNotificationPing } from "../../hooks/useNotificationPing";

const acknowledgedAutoPingKeys = new Set();

/**
 * Fires a notification ping automatically on mount and renders a single
 * status badge. No buttons or user interaction required.
 */
function NotificationPingStatus({ documentId, eventKey, timeoutMs = 60000 }) {
    const { status, runPing } = useNotificationPing(documentId, eventKey, timeoutMs);
    const cacheKey = `${documentId}::${eventKey}`;
    const hasAcknowledgedAutoPing = acknowledgedAutoPingKeys.has(cacheKey);

    useEffect(() => {
        if (hasAcknowledgedAutoPing) {
            return;
        }

        runPing()
            .then((result) => {
                if (result.acknowledged) {
                    acknowledgedAutoPingKeys.add(cacheKey);
                }
            })
            .catch(() => {
                // The hook updates status to "error"/"timeout".
            });
    }, [cacheKey, hasAcknowledgedAutoPing, runPing]);

    const effectiveStatus = hasAcknowledgedAutoPing ? "ok" : status;

    const badgeClassName =
        effectiveStatus === "ok"
            ? "bg-success"
            : effectiveStatus === "checking"
                ? "bg-warning text-dark"
                : effectiveStatus === "timeout" || effectiveStatus === "error"
                    ? "bg-danger"
                    : "bg-secondary";

    const statusLabel =
        effectiveStatus === "ok"
            ? "Notification Tool: OK"
            : effectiveStatus === "checking"
                ? "Notification Tool: Checking…"
                : effectiveStatus === "timeout"
                    ? "Notification Tool: Timeout"
                    : effectiveStatus === "error"
                        ? "Notification Tool: Error"
                        : "Notification Tool: Not Checked";

    return <span className={`badge ${badgeClassName}`}>{statusLabel}</span>;
}

export default NotificationPingStatus;
