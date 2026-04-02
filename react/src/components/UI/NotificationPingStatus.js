import { useEffect } from "react";
import { useNotificationPing } from "../../hooks/useNotificationPing";

/**
 * Fires a notification ping automatically on mount and renders a single
 * status badge. No buttons or user interaction required.
 */
function NotificationPingStatus({ documentId, eventKey, timeoutMs = 60000 }) {
    const { status, runPing } = useNotificationPing(documentId, eventKey, timeoutMs);

    useEffect(() => {
        runPing();
    }, [runPing]);

    const badgeClassName =
        status === "ok"
            ? "bg-success"
            : status === "checking"
                ? "bg-warning text-dark"
                : status === "timeout" || status === "error"
                    ? "bg-danger"
                    : "bg-secondary";

    const statusLabel =
        status === "ok"
            ? "Notification Tool: OK"
            : status === "checking"
                ? "Notification Tool: Checking…"
                : status === "timeout"
                    ? "Notification Tool: Timeout"
                    : status === "error"
                        ? "Notification Tool: Error"
                        : "Notification Tool: Not Checked";

    return <span className={`badge ${badgeClassName}`}>{statusLabel}</span>;
}

export default NotificationPingStatus;
