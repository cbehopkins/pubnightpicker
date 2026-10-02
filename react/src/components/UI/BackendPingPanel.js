import useBackendPing from "../../hooks/useBackendPing";

export default function BackendPingPanel({ timeoutMs = 10000 }) {
    const { status, uid, message, runPing } = useBackendPing(timeoutMs);
    const badgeClass = status === "ok" ? "bg-success"
        : status === "checking" ? "bg-warning text-dark"
            : status === "idle" ? "bg-secondary" : "bg-danger";
    const label = status === "ok" ? "Authenticated"
        : status === "checking" ? "Checking"
            : status === "idle" ? "Not Checked"
                : status === "unauthenticated" ? "Authentication Refused"
                    : status === "forbidden" ? "Access Refused"
                        : status === "timeout" ? "Timeout" : "Error";

    return (
        <section className="mb-4" aria-labelledby="last-orders-ping-heading">
            <h2 id="last-orders-ping-heading" className="h5 mb-2">Last Orders API</h2>
            <div className="d-flex flex-wrap align-items-center gap-2">
                <button type="button" className="btn btn-outline-secondary" onClick={runPing}
                    disabled={status === "checking"}>
                    {status === "checking" ? "Checking..." : "Ping Last Orders"}
                </button>
                <div role="status" aria-live="polite" aria-atomic="true" className="text-break">
                    <span className={`badge ${badgeClass}`}>{label}</span>
                    {status !== "idle" && status !== "checking" && (
                        <span className="small ms-2">{message}{uid && ` Verified UID: ${uid}`}</span>
                    )}
                </div>
            </div>
        </section>
    );
}
