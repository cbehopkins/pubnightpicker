import { useEffect, useState } from "react";
import { onAuthStateChanged } from "firebase/auth";
import { Button, Card, Modal } from "react-bootstrap";
import { auth } from "../../firebase";
import { LastOrdersApiError, requestLastOrders } from "../../utils/lastOrdersApi";
import { formatLocalDateTime } from "../../utils/dateTimeFormatting";

const emptyResult = { status: "loading", entries: [], message: "" };

function validTimestamp(value) {
    return typeof value === "string" && Number.isFinite(Date.parse(value));
}

function stateVariant(state) {
    if (["Refused", "HardBounced", "SpamComplaint"].includes(state)) return "danger";
    if (["Delivered", "Opened", "Clicked"].includes(state)) return "success";
    if (["Pending", "Recovery", "RecoveryWaiting"].includes(state)) return "warning";
    return "secondary";
}

function EmailHistoryDialog({ onClose, timeoutMs }) {
    const [result, setResult] = useState(emptyResult);
    const [reload, setReload] = useState(0);

    useEffect(() => {
        const uid = auth.currentUser?.uid;
        const controller = new AbortController();
        let active = true;
        const timer = setTimeout(() => controller.abort(new LastOrdersApiError("timeout", "Email history did not respond within the time limit.")), timeoutMs);
        const unsubscribe = onAuthStateChanged(auth, (user) => {
            if (user?.uid !== uid) {
                active = false;
                controller.abort();
                clearTimeout(timer);
                onClose();
            }
        });
        setResult(emptyResult);
        const load = async () => {
            try {
                const requestID = crypto.randomUUID();
                const response = await requestLastOrders("api/email-history", { body: { request_id: requestID }, signal: controller.signal });
                if (!active) return;
                if (response?.status !== "ok" || !uid || response.uid !== uid || response.request_id !== requestID ||
                    !Array.isArray(response.entries) || response.entries.length > 20 ||
                    !response.entries.every((entry) => entry && typeof entry.subject === "string" &&
                        typeof entry.recipient === "string" && typeof entry.state === "string" &&
                        validTimestamp(entry.created_at) && (entry.submitted_at === null || validTimestamp(entry.submitted_at)))) {
                    throw new LastOrdersApiError("invalid_response", "Last Orders returned an unexpected response.");
                }
                setResult({ status: "ok", entries: response.entries, message: "" });
            } catch (error) {
                if (!active) return;
                setResult({ status: "error", entries: [], message: error instanceof LastOrdersApiError ? error.message : "Unable to load email history." });
            } finally {
                clearTimeout(timer);
            }
        };
        void load();
        return () => {
            active = false;
            unsubscribe();
            clearTimeout(timer);
            controller.abort();
        };
    }, [reload, timeoutMs, onClose]);

    return (
        <Modal show onHide={onClose} size="lg" scrollable aria-labelledby="email-history-title">
            <Modal.Header closeButton>
                <Modal.Title id="email-history-title">Recent Email Status</Modal.Title>
            </Modal.Header>
            <Modal.Body>
                {result.status === "loading" && <p role="status" className="mb-0">Loading email history...</p>}
                {result.status === "error" && <p role="alert" className="text-danger mb-0">{result.message}</p>}
                {result.status === "ok" && result.entries.length === 0 && <p className="mb-0">No email history found.</p>}
                {result.status === "ok" && result.entries.length > 0 && (
                    <div className="table-responsive">
                        <table className="table table-sm align-middle mb-0">
                            <thead><tr><th scope="col" className="d-none d-md-table-cell">Requested</th><th scope="col">Email</th><th scope="col">Status</th></tr></thead>
                            <tbody>
                                {result.entries.map((entry, index) => (
                                    <tr key={index}>
                                        <td className="text-nowrap d-none d-md-table-cell">{formatLocalDateTime(new Date(entry.created_at))}</td>
                                        <td style={{ overflowWrap: "anywhere" }}>
                                            <div className="small text-body-secondary d-md-none">{formatLocalDateTime(new Date(entry.created_at))}</div>
                                            <div>{entry.subject || "(No subject)"}</div>
                                            <div className="small text-body-secondary">{entry.recipient}</div>
                                            {entry.submitted_at && <div className="small text-body-secondary">Submitted {formatLocalDateTime(new Date(entry.submitted_at))}</div>}
                                        </td>
                                        <td><span className={`badge text-bg-${stateVariant(entry.state)}`}>
                                            {entry.state.replace(/([a-z])([A-Z])/g, "$1 $2")}
                                        </span></td>
                                    </tr>
                                ))}
                            </tbody>
                        </table>
                    </div>
                )}
            </Modal.Body>
            <Modal.Footer>
                <Button variant="outline-secondary" disabled={result.status === "loading"} onClick={() => setReload((current) => current + 1)}>Refresh</Button>
                <Button variant="secondary" onClick={onClose}>Close</Button>
            </Modal.Footer>
        </Modal>
    );
}

export default function EmailHistoryPanel({ timeoutMs = 10000 }) {
    const [show, setShow] = useState(false);
    const [close] = useState(() => () => setShow(false));

    return (
        <Card>
            <Card.Body>
                <h2 className="h5 mb-3">Email History</h2>
                <Button variant="outline-secondary" onClick={() => setShow(true)}>Email Status</Button>
            </Card.Body>
            {show && <EmailHistoryDialog onClose={close} timeoutMs={timeoutMs} />}
        </Card>
    );
}
