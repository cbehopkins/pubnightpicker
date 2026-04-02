import { useCallback, useState } from "react";
import { clearNotificationPing, pingNotificationTool } from "../../dbtools/notificationPings";
import { notifyError, notifyInfo } from "../../utils/notify";

function NotificationPingPanel({
    title,
    description,
    buttonLabel,
    checkingLabel,
    documentId,
    eventKey,
    timeoutMs = 60000,
}) {
    const [status, setStatus] = useState("idle");
    const [lastPingValue, setLastPingValue] = useState(null);

    const runPing = useCallback(async () => {
        setStatus("checking");
        try {
            const result = await pingNotificationTool(documentId, eventKey, timeoutMs);
            if (result.acknowledged) {
                setStatus("ok");
                setLastPingValue(result.pingValue);
                notifyInfo(`Notification diagnostics ping acknowledged (value ${result.pingValue}).`);
                return;
            }

            if (result.timedOut) {
                setStatus("timeout");
                notifyError(`No response from notification tool after ${Math.round(timeoutMs / 1000)}s.`);
                return;
            }

            setStatus("error");
            notifyError("Notification diagnostics ping did not receive an acknowledgement.");
        } catch (error) {
            console.error(error);
            setStatus("error");
            notifyError("Unable to run notification diagnostics ping.");
        }
    }, [documentId, eventKey, timeoutMs]);

    const clearPing = useCallback(async () => {
        try {
            await clearNotificationPing(documentId, eventKey);
            setStatus("idle");
            setLastPingValue(null);
            notifyInfo("Notification diagnostics ping cleared.");
        } catch (error) {
            console.error(error);
            notifyError("Unable to clear notification diagnostics ping.");
        }
    }, [documentId, eventKey]);

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
                ? "Notification Tool: Checking"
                : status === "timeout"
                    ? "Notification Tool: Timeout"
                    : status === "error"
                        ? "Notification Tool: Error"
                        : "Notification Tool: Not Checked";

    return (
        <section className="mb-4">
            <h2 className="h5 mb-2">{title}</h2>
            <p className="mb-3 text-body-secondary">{description}</p>
            <div className="d-flex flex-wrap align-items-center gap-2">
                <button
                    type="button"
                    className="btn btn-outline-secondary"
                    onClick={runPing}
                    disabled={status === "checking"}
                >
                    {status === "checking" ? checkingLabel : buttonLabel}
                </button>
                <button
                    type="button"
                    className="btn btn-outline-danger"
                    onClick={clearPing}
                    disabled={status === "checking"}
                >
                    Clear Ping
                </button>
                <span className={`badge ${badgeClassName}`}>{statusLabel}</span>
            </div>
            {lastPingValue !== null && (
                <p className="small text-body-secondary mt-2 mb-0">Last ping value: {lastPingValue}</p>
            )}
        </section>
    );
}

export default NotificationPingPanel;
