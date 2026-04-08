import { useCallback } from "react";
import { useNotificationPing } from "../../hooks/useNotificationPing";
import { notifyError, notifyInfo } from "../../utils/notify";

function formatPingTimestamp(rawPingValue) {
    const numericValue =
        typeof rawPingValue === "number"
            ? rawPingValue
            : typeof rawPingValue === "string"
                ? Number(rawPingValue)
                : Number.NaN;

    if (!Number.isFinite(numericValue)) {
        return null;
    }

    const timestampMs = numericValue < 1e11 ? numericValue * 1000 : numericValue;
    const pingDate = new Date(timestampMs);

    if (Number.isNaN(pingDate.getTime())) {
        return null;
    }

    return pingDate.toLocaleString();
}

function NotificationPingPanel({
    title,
    description,
    buttonLabel,
    checkingLabel,
    documentId,
    eventKey,
    timeoutMs = 60000,
}) {
    const { status, lastPingValue, runPing, clearPing } = useNotificationPing(documentId, eventKey, timeoutMs);
    const lastPingFriendlyTime = formatPingTimestamp(lastPingValue);

    const handlePing = useCallback(async () => {
        try {
            const result = await runPing();
            if (result.acknowledged) {
                notifyInfo(`Notification diagnostics ping acknowledged (value ${result.pingValue}).`);
            } else if (result.timedOut) {
                notifyError(`No response from notification tool after ${Math.round(timeoutMs / 1000)}s.`);
            } else {
                notifyError("Notification diagnostics ping did not receive an acknowledgement.");
            }
        } catch (error) {
            console.error(error);
            notifyError("Unable to run notification diagnostics ping.");
        }
    }, [runPing, timeoutMs]);

    const handleClear = useCallback(async () => {
        try {
            await clearPing();
            notifyInfo("Notification diagnostics ping cleared.");
        } catch (error) {
            console.error(error);
            notifyError("Unable to clear notification diagnostics ping.");
        }
    }, [clearPing]);

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
                    onClick={handlePing}
                    disabled={status === "checking"}
                >
                    {status === "checking" ? checkingLabel : buttonLabel}
                </button>
                <button
                    type="button"
                    className="btn btn-outline-danger"
                    onClick={handleClear}
                    disabled={status === "checking"}
                >
                    Clear Ping
                </button>
                <span className={`badge ${badgeClassName}`}>{statusLabel}</span>
            </div>
            {lastPingValue !== null && (
                <p className="small text-body-secondary mt-2 mb-0">
                    Last ping value: {lastPingValue}
                    {lastPingFriendlyTime && ` (${lastPingFriendlyTime})`}
                </p>
            )}
        </section>
    );
}

export default NotificationPingPanel;
