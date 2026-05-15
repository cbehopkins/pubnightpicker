import { useEffect, useMemo, useState } from "react";
import { useSelector } from "react-redux";
import {
    Timestamp,
    collection,
    doc,
    getDoc,
    limit,
    onSnapshot,
    orderBy,
    query,
    where,
} from "firebase/firestore";
import ProtectedRoute from "../ProtectedRoute";
import {
    NOTIFICATION_DIAGNOSTICS_DOC,
    NOTIFICATION_PUSH_TEST_DOC,
} from "../../dbtools/notificationPings";
import NotificationPingPanel from "../UI/NotificationPingPanel";
import {
    POLL_ACTION_ADD_VENUE,
    POLL_ACTION_AUDIT_COLLECTION,
    POLL_ACTION_COMPLETE,
    POLL_ACTION_CREATE,
    POLL_ACTION_DELETE_VENUE,
} from "../../dbtools/pollActionAudit";
import { db } from "../../firebase";

const AUDIT_ACTION_FILTER_ALL = "all";
const DEFAULT_AUDIT_DAYS = 7;
const DEFAULT_AUDIT_WINDOW = `days:${DEFAULT_AUDIT_DAYS}`;

function parseAuditWindowSelection(value) {
    const [kind, rawAmount] = String(value).split(":");
    const amount = Number(rawAmount);

    if (!Number.isFinite(amount) || amount <= 0) {
        return { kind: "days", amount: DEFAULT_AUDIT_DAYS };
    }
    if (kind === "entries") {
        return { kind: "entries", amount };
    }
    return { kind: "days", amount };
}

function formatAuditTimestamp(timestampValue) {
    if (!timestampValue || typeof timestampValue.toDate !== "function") {
        return "Pending";
    }
    return timestampValue.toDate().toLocaleString();
}

export function PollActionAuditPanel() {
    const [auditEntries, setAuditEntries] = useState([]);
    const [actorNamesByUid, setActorNamesByUid] = useState({});
    const [actionFilter, setActionFilter] = useState(AUDIT_ACTION_FILTER_ALL);
    const [windowSelection, setWindowSelection] = useState(DEFAULT_AUDIT_WINDOW);
    const [isLoading, setIsLoading] = useState(true);
    const [errorMessage, setErrorMessage] = useState("");

    useEffect(() => {
        setIsLoading(true);
        setErrorMessage("");

        const parsedWindow = parseAuditWindowSelection(windowSelection);

        const auditQuery =
            parsedWindow.kind === "entries"
                ? query(
                    collection(db, POLL_ACTION_AUDIT_COLLECTION),
                    orderBy("at", "desc"),
                    limit(parsedWindow.amount)
                )
                : query(
                    collection(db, POLL_ACTION_AUDIT_COLLECTION),
                    where(
                        "at",
                        ">=",
                        Timestamp.fromDate(
                            new Date(Date.now() - parsedWindow.amount * 24 * 60 * 60 * 1000)
                        )
                    ),
                    orderBy("at", "desc"),
                    limit(50)
                );

        const unsubscribe = onSnapshot(
            auditQuery,
            (snapshot) => {
                const nextEntries = snapshot.docs.map((entry) => ({
                    id: entry.id,
                    ...entry.data(),
                }));
                setAuditEntries(nextEntries);
                setIsLoading(false);
            },
            (error) => {
                setErrorMessage(error?.message || "Unable to load poll action audit records.");
                setIsLoading(false);
            }
        );

        return () => unsubscribe();
    }, [windowSelection]);

    const filteredEntries = useMemo(() => {
        if (actionFilter === AUDIT_ACTION_FILTER_ALL) {
            return auditEntries;
        }
        return auditEntries.filter((entry) => entry.actionType === actionFilter);
    }, [actionFilter, auditEntries]);

    useEffect(() => {
        const actorUids = Array.from(
            new Set(
                auditEntries
                    .map((entry) => entry.actorUid)
                    .filter((uid) => typeof uid === "string" && uid.length > 0)
            )
        );

        if (actorUids.length === 0) {
            return;
        }

        const missingUids = actorUids.filter((uid) => !(uid in actorNamesByUid));
        if (missingUids.length === 0) {
            return;
        }

        let cancelled = false;
        const loadActorNames = async () => {
            const nextNames = {};
            await Promise.all(
                missingUids.map(async (uid) => {
                    try {
                        const snapshot = await getDoc(doc(db, "user-public", uid));
                        if (!snapshot.exists()) {
                            return;
                        }
                        const profile = snapshot.data() || {};
                        const displayName =
                            typeof profile.name === "string" && profile.name.length > 0
                                ? profile.name
                                : null;
                        if (displayName) {
                            nextNames[uid] = displayName;
                        }
                    } catch {
                        // Fall back to uid when profile lookup fails.
                    }
                })
            );

            if (!cancelled && Object.keys(nextNames).length > 0) {
                setActorNamesByUid((current) => ({ ...current, ...nextNames }));
            }
        };

        void loadActorNames();
        return () => {
            cancelled = true;
        };
    }, [actorNamesByUid, auditEntries]);

    const summaryLabel = useMemo(() => {
        if (actionFilter === POLL_ACTION_CREATE) {
            return "Showing poll creation events";
        }
        if (actionFilter === POLL_ACTION_COMPLETE) {
            return "Showing poll completion events";
        }
        if (actionFilter === POLL_ACTION_ADD_VENUE) {
            return "Showing venue add events";
        }
        if (actionFilter === POLL_ACTION_DELETE_VENUE) {
            return "Showing venue delete events";
        }
        return "Showing all poll action events";
    }, [actionFilter]);

    return (
        <section className="mb-4">
            <h2 className="h5 mb-2">Poll Action Audit</h2>
            <p className="mb-3 text-body-secondary">
                Trace who created and completed polls.
            </p>
            <div className="row g-2 mb-3">
                <div className="col-12 col-md-4">
                    <label htmlFor="audit-action-filter" className="form-label mb-1">Action</label>
                    <select
                        id="audit-action-filter"
                        className="form-select"
                        value={actionFilter}
                        onChange={(event) => setActionFilter(event.target.value)}
                    >
                        <option value={AUDIT_ACTION_FILTER_ALL}>All actions</option>
                        <option value={POLL_ACTION_CREATE}>Create</option>
                        <option value={POLL_ACTION_COMPLETE}>Complete</option>
                        <option value={POLL_ACTION_ADD_VENUE}>Add venue</option>
                        <option value={POLL_ACTION_DELETE_VENUE}>Delete venue</option>
                    </select>
                </div>
                <div className="col-12 col-md-4">
                    <label htmlFor="audit-days-window" className="form-label mb-1">Window</label>
                    <select
                        id="audit-days-window"
                        className="form-select"
                        value={windowSelection}
                        onChange={(event) => setWindowSelection(event.target.value)}
                    >
                        <option value="days:1">Last 24 hours</option>
                        <option value="days:7">Last 7 days</option>
                        <option value="days:30">Last 30 days</option>
                        <option value="days:90">Last 90 days</option>
                        <option value="entries:10">Last 10 entries</option>
                        <option value="entries:50">Last 50 entries</option>
                        <option value="entries:100">Last 100 entries</option>
                    </select>
                </div>
            </div>
            <p className="small text-body-secondary mb-2">{summaryLabel}</p>
            {isLoading && <p className="small text-body-secondary">Loading audit records...</p>}
            {!isLoading && errorMessage && (
                <p className="small text-danger mb-0">{errorMessage}</p>
            )}
            {!isLoading && !errorMessage && filteredEntries.length === 0 && (
                <p className="small text-body-secondary mb-0">No matching poll audit records found.</p>
            )}
            {!isLoading && !errorMessage && filteredEntries.length > 0 && (
                <div className="table-responsive">
                    <table className="table table-sm align-middle">
                        <thead>
                            <tr>
                                <th scope="col">Time</th>
                                <th scope="col">Action</th>
                                <th scope="col">Poll</th>
                                <th scope="col">Actor</th>
                                <th scope="col">Details</th>
                            </tr>
                        </thead>
                        <tbody>
                            {filteredEntries.map((entry) => (
                                <tr key={entry.id}>
                                    <td>{formatAuditTimestamp(entry.at)}</td>
                                    <td>{entry.actionType || "unknown"}</td>
                                    <td>
                                        <div>{entry.pollId || "-"}</div>
                                        <div className="small text-body-secondary">{entry.pollDate || "-"}</div>
                                    </td>
                                    <td>
                                        <div>{actorNamesByUid[entry.actorUid] || entry.actorUid || "-"}</div>
                                        {actorNamesByUid[entry.actorUid] && (
                                            <div className="small text-body-secondary">{entry.actorUid}</div>
                                        )}
                                    </td>
                                    <td>
                                        {entry.selectedVenueId
                                            ? `venue=${entry.selectedVenueId}${entry.venueName ? ` (${entry.venueName})` : ""}${entry.actionType === POLL_ACTION_COMPLETE && entry.restaurantId ? `, restaurant=${entry.restaurantId}` : ""}`
                                            : "-"}
                                    </td>
                                </tr>
                            ))}
                        </tbody>
                    </table>
                </div>
            )}
        </section>
    );
}

function DiagnosticsPage() {
    const uid = useSelector((state) => state.auth.uid);

    return (
        <div className="container py-4 py-md-5">
            <h1 className="display-6 fw-bold mb-3">Diagnostics</h1>
            <p className="text-body-secondary mb-4">
                Admin-only tools for validating notification and push messaging.
            </p>

            <NotificationPingPanel
                title="Admin Diagnostics"
                description="Run a manual ping to confirm the notification tool is responding."
                buttonLabel="Ping Notification Tool"
                checkingLabel="Checking..."
                documentId={NOTIFICATION_DIAGNOSTICS_DOC}
                eventKey="manual"
                timeoutMs={60000}
                statusPrefix="Notification Tool"
                showStatusBadge
                showClearButton
            />

            {uid && (
                <NotificationPingPanel
                    title="Push Diagnostics"
                    description="Send a test push notification to your own account."
                    buttonLabel="Send Push To Me"
                    checkingLabel="Sending..."
                    documentId={NOTIFICATION_PUSH_TEST_DOC}
                    eventKey={uid}
                    timeoutMs={60000}
                    statusPrefix="Push Diagnostics"
                    showStatusBadge={false}
                    showClearButton={false}
                    preSendDelaySeconds={5}
                />
            )}

            <PollActionAuditPanel />
        </div>
    );
}

export default ProtectedRoute(DiagnosticsPage, "admin", "/");
