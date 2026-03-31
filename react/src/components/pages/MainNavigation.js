import { NavLink } from "react-router-dom";
import { useSelector } from "react-redux";
import styles from "./MainNavigation.module.css";
import { logout } from "../../firebase";
import useAdmin from "../../hooks/useAdmin";
import useRole from "../../hooks/useRole";
import Button from "../UI/Button";

const LoggedInElement = (params) => {
  return (
    <div className={`${styles.logged_in} text-light text-md-end`}>
      <div className="small text-uppercase text-secondary-emphasis">Logged in as</div>
      <div className="fw-semibold">{params.name}</div>
      <div className="small mb-2 text-break">{params.email}</div>
      <Button type="button" variant="secondary" className={styles.logged_in__btn} onClick={logout}>
        Logout
      </Button>
    </div>
  );
};

function MainNavigation() {
  const name = useSelector((state) => state.auth.name);
  const loggedIn = useSelector((state) => state.auth.loggedIn);
  const admin = useAdmin();
  const canChat = useRole("canChat");
  const email = useSelector((state) => state.auth.email);

  const navLinkClassName = ({ isActive }) =>
    `nav-link px-2 py-1 rounded ${styles.navLink} ${isActive ? `${styles.active} active` : ""}`;

  return (
    <header className={`${styles.header} container-fluid py-2 py-md-3`}>
      <nav className={`${styles.nav} d-flex flex-wrap align-items-center gap-2`}>
        <ul className={`${styles.list} nav nav-pills flex-wrap gap-1 gap-md-2`}>
          <li className="nav-item">
            <NavLink
              to="/"
              className={navLinkClassName}
              end
            >
              Home Page
            </NavLink>
          </li>
          {canChat && <li className="nav-item">
            <NavLink
              to="/chat"
              className={navLinkClassName}
              end
            >
              Chat Page
            </NavLink>
          </li>}
          <li className="nav-item">
            <NavLink
              to="/venues"
              className={navLinkClassName}
              end
            >
              {loggedIn ? "Manage Venues" : "View Venues"}
            </NavLink>
          </li>
          {admin && <li className="nav-item">
            <NavLink
              to="/manage_users"
              className={navLinkClassName}
              end
            >
              Manage Users
            </NavLink>
          </li>}
          {!loggedIn && (
            <li className="nav-item">
              <NavLink
                to="/login"
                className={navLinkClassName}
                end
              >
                Login
              </NavLink>
            </li>
          )}
          {loggedIn && (
            <li className="nav-item">
              <NavLink
                to="/active_polls"
                className={navLinkClassName}
                end
              >
                Active Polls
              </NavLink>
            </li>
          )}
          <li className="nav-item">
            <NavLink
              to="/current_events"
              className={navLinkClassName}
              end
            >
              Current Events
            </NavLink>
          </li>
          {loggedIn && <li className="nav-item">
            <NavLink
              to="/past_events"
              className={navLinkClassName}
              end
            >
              Past Events
            </NavLink>
          </li>}
          {loggedIn && (
            <li className="nav-item">
              <NavLink
                to="/preferences"
                className={navLinkClassName}
                end
              >
                My Preferences
              </NavLink>
            </li>
          )}
        </ul>
      </nav>
      {loggedIn && <LoggedInElement name={name} email={email} />}
    </header>
  );
}

export default MainNavigation;
