import { useSelector } from "react-redux";
import ProtectedRoute from "../ProtectedRoute";
import {
    NOTIFICATION_DIAGNOSTICS_DOC,
    NOTIFICATION_PUSH_TEST_DOC,
} from "../../dbtools/notificationPings";
import NotificationPingPanel from "../UI/NotificationPingPanel";

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
        </div>
    );
}

export default ProtectedRoute(DiagnosticsPage, "admin", "/");
