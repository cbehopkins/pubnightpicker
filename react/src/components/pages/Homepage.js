import { NavLink } from "react-router-dom";
import { useSelector } from "react-redux";
import useAdmin from "../../hooks/useAdmin";
import {
  NOTIFICATION_DIAGNOSTICS_DOC,
} from "../../dbtools/notificationPings";
import NotificationPingPanel from "../UI/NotificationPingPanel";

function Homepage() {
  const loggedIn = useSelector((state) => state.auth.loggedIn);
  const canRunDiagnostics = useAdmin();

  return (
    <div className="container py-4 py-md-5">
      <section className="mb-5">
        <h1 className="display-6 fw-bold mb-3">Pub Night Picker</h1>
        <p className="lead text-body-secondary mb-4">
          Vote together, pick a venue, and keep everyone updated for this week&apos;s pub night.
        </p>
        <div className="d-flex flex-wrap gap-2">
          {loggedIn && (
            <NavLink className="btn btn-primary" to="/active_polls">
              View Active Polls
            </NavLink>
          )}
          <NavLink className="btn btn-outline-primary" to="/current_events">
            View Current Events
          </NavLink>
          {!loggedIn && (
            <>
              <NavLink className="btn btn-outline-secondary" to="/login">
                Log In
              </NavLink>
              <NavLink className="btn btn-outline-secondary" to="/register">
                Register
              </NavLink>
            </>
          )}
        </div>
      </section>

      <section className="mb-5">
        <h2 className="h4 mb-3">How It Works</h2>
        <ol className="ps-3">
          <li className="mb-2">Check whether a poll is currently open.</li>
          <li className="mb-2">Vote for your preferred venue.</li>
          <li className="mb-2">View the selected venue and event details.</li>
        </ol>
      </section>

      <section className="mb-5">
        <h2 className="h4 mb-3">What You Can Do</h2>
        <div className="row g-3">
          <div className="col-sm-6 col-lg-3">
            <div className="border rounded p-3 h-100">
              <h3 className="h6 text-uppercase">Vote</h3>
              <p className="mb-0 text-body-secondary">Take part in active polls to help choose the venue.</p>
            </div>
          </div>
          <div className="col-sm-6 col-lg-3">
            <div className="border rounded p-3 h-100">
              <h3 className="h6 text-uppercase">Stay Updated</h3>
              <p className="mb-0 text-body-secondary">See current event details and browse past events.</p>
            </div>
          </div>
          <div className="col-sm-6 col-lg-3">
            <div className="border rounded p-3 h-100">
              <h3 className="h6 text-uppercase">Chat</h3>
              <p className="mb-0 text-body-secondary">Join the group chat when chat access is enabled.</p>
            </div>
          </div>
          <div className="col-sm-6 col-lg-3">
            <div className="border rounded p-3 h-100">
              <h3 className="h6 text-uppercase">Preferences</h3>
              <p className="mb-0 text-body-secondary">Manage notification and account preferences.</p>
            </div>
          </div>
          <div className="col-sm-6 col-lg-3">
            <div className="border rounded p-3 h-100">
              <h3 className="h6 text-uppercase">Confirm Attendance</h3>
              <p className="mb-0 text-body-secondary">Let others know if you&apos;ll attend—helps with planning and table bookings.</p>
            </div>
          </div>
        </div>
      </section>

      <section className="mb-4">
        <h2 className="h5 mb-2">Access and Support</h2>
        <p className="mb-2 text-body-secondary">
          Some features are available only when enabled on your account.
        </p>
        <p className="mb-0 text-body-secondary">
          Need help using the app? Visit the <NavLink to="/help">Help and FAQ page</NavLink>.
        </p>
      </section>

      {canRunDiagnostics && (
        <NotificationPingPanel
          title="Admin Diagnostics"
          description="Run a manual ping to confirm the notification tool is responding."
          buttonLabel="Ping Notification Tool"
          checkingLabel="Checking..."
          documentId={NOTIFICATION_DIAGNOSTICS_DOC}
          eventKey="manual"
          timeoutMs={60000}
        />
      )}

      <p className="small text-body-secondary mb-0">
        Maintained by Chris H. <a href="https://github.com/cbehopkins/pubnightpicker" target="_blank" rel="noopener noreferrer">We also accept PRs on GitHub</a>
      </p>
    </div>
  );
}

export default Homepage;
