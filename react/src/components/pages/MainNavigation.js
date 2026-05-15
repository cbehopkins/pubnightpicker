import { useEffect, useState } from "react";
import { NavLink } from "react-router-dom";
import { useSelector } from "react-redux";
import { Container, Nav, Navbar } from "react-bootstrap";
import styles from "./MainNavigation.module.css";
import { logout } from "../../firebase";
import useAdmin from "../../hooks/useAdmin";
import useRole from "../../hooks/useRole";
import useUsers from "../../hooks/useUsers";
import Button from "../UI/Button";
import { applyThemeMode, getStoredThemeMode, subscribeToSystemThemeChanges } from "../../utils/themeMode";

const LoggedInElement = (params) => {
  return (
    <div className={`${styles.logged_in} text-md-end`}>
      <div className="small text-uppercase text-body-secondary">Logged in as</div>
      <div className="fw-semibold">{params.name}</div>
      <div className="small text-break text-body-secondary">{params.email}</div>
      <Button type="button" variant="outline-secondary" className={styles.logged_in__btn} onClick={logout}>
        Logout
      </Button>
    </div>
  );
};

function MainNavigation() {
  const [expanded, setExpanded] = useState(false);
  const authName = useSelector((state) => state.auth.name);
  const uid = useSelector((state) => state.auth.uid);
  const loggedIn = useSelector((state) => state.auth.loggedIn);
  const admin = useAdmin();
  const canChat = useRole("canChat");
  const email = useSelector((state) => state.auth.email);
  const users = useUsers();
  const name = (uid && users[uid]?.name) || authName;
  useEffect(() => {
    const mode = getStoredThemeMode();
    applyThemeMode(mode);

    if (mode !== "auto") {
      return undefined;
    }

    const handleChange = () => applyThemeMode("auto");
    return subscribeToSystemThemeChanges(handleChange);
  }, []);

  const navLinkClassName = ({ isActive }) =>
    `nav-link px-2 py-1 rounded ${styles.navLink} ${isActive ? `active ${styles.navLinkActive}` : ""}`;

  return (
    <header className="border-bottom">
      <Navbar expand="lg" bg="body-tertiary" className="py-2" expanded={expanded} onToggle={(exp) => setExpanded(exp)}>
        <Container fluid>
          <Navbar.Brand as={NavLink} to="/" className="fw-semibold">
            Pub Night Picker
          </Navbar.Brand>
          <Navbar.Toggle aria-controls="main-navbar" />
          <Navbar.Collapse id="main-navbar">
            <Nav className="me-auto mb-2 mb-lg-0 flex-wrap">
              <Nav.Link as={NavLink} to="/" end className={navLinkClassName} onClick={() => setExpanded(false)}>Home Page</Nav.Link>
              <Nav.Link as={NavLink} to="/help" end className={navLinkClassName} onClick={() => setExpanded(false)}>Help</Nav.Link>
              {canChat && <Nav.Link as={NavLink} to="/chat" end className={navLinkClassName} onClick={() => setExpanded(false)}>Chat Page</Nav.Link>}
              <Nav.Link as={NavLink} to="/venues" end className={navLinkClassName} onClick={() => setExpanded(false)}>
                {loggedIn ? "Manage Venues" : "View Venues"}
              </Nav.Link>
              {admin && <Nav.Link as={NavLink} to="/manage_users" end className={navLinkClassName} onClick={() => setExpanded(false)}>Manage Users</Nav.Link>}
              {admin && <Nav.Link as={NavLink} to="/diagnostics" end className={navLinkClassName} onClick={() => setExpanded(false)}>Diagnostics</Nav.Link>}
              {!loggedIn && <Nav.Link as={NavLink} to="/login" end className={navLinkClassName} onClick={() => setExpanded(false)}>Login</Nav.Link>}
              {loggedIn && <Nav.Link as={NavLink} to="/active_polls" end className={navLinkClassName} onClick={() => setExpanded(false)}>Active Polls</Nav.Link>}
              <Nav.Link as={NavLink} to="/current_events" end className={navLinkClassName} onClick={() => setExpanded(false)}>Current Events</Nav.Link>
              {loggedIn && <Nav.Link as={NavLink} to="/past_events" end className={navLinkClassName} onClick={() => setExpanded(false)}>Past Events</Nav.Link>}
              {loggedIn && <Nav.Link as={NavLink} to="/preferences" end className={navLinkClassName} onClick={() => setExpanded(false)}>My Preferences</Nav.Link>}
            </Nav>

            <div className={`${styles.controls} d-flex flex-column flex-lg-row align-items-start align-items-lg-center gap-2 gap-lg-3`}>
              {loggedIn && <LoggedInElement name={name || email} email={email} />}
            </div>
          </Navbar.Collapse>
        </Container>
      </Navbar>
    </header>
  );
}

export default MainNavigation;
