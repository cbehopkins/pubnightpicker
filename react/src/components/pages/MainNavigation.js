import { useEffect, useMemo, useState } from "react";
import { NavLink } from "react-router-dom";
import { useSelector } from "react-redux";
import { Container, Dropdown, Nav, Navbar } from "react-bootstrap";
import styles from "./MainNavigation.module.css";
import { logout } from "../../firebase";
import useAdmin from "../../hooks/useAdmin";
import useRole from "../../hooks/useRole";
import Button from "../UI/Button";

const THEME_STORAGE_KEY = "pnp-theme-mode";

const getStoredThemeMode = () => {
  if (typeof window === "undefined") {
    return "auto";
  }

  const stored = window.localStorage.getItem(THEME_STORAGE_KEY);
  return stored === "light" || stored === "dark" || stored === "auto" ? stored : "auto";
};

const getResolvedTheme = (mode) => {
  if (mode === "light" || mode === "dark") {
    return mode;
  }

  if (typeof window !== "undefined" && window.matchMedia("(prefers-color-scheme: dark)").matches) {
    return "dark";
  }

  return "light";
};

const applyTheme = (mode) => {
  if (typeof document === "undefined") {
    return;
  }

  const resolved = getResolvedTheme(mode);
  const root = document.documentElement;
  root.setAttribute("data-bs-theme", resolved);
  root.style.colorScheme = resolved;
};

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
  const name = useSelector((state) => state.auth.name);
  const loggedIn = useSelector((state) => state.auth.loggedIn);
  const admin = useAdmin();
  const canChat = useRole("canChat");
  const email = useSelector((state) => state.auth.email);
  const [themeMode, setThemeMode] = useState(getStoredThemeMode);

  const themeLabel = useMemo(() => {
    if (themeMode === "light") {
      return "Light";
    }
    if (themeMode === "dark") {
      return "Dark";
    }
    return "Auto";
  }, [themeMode]);

  useEffect(() => {
    applyTheme(themeMode);

    if (typeof window === "undefined") {
      return undefined;
    }

    if (themeMode !== "auto") {
      return undefined;
    }

    const mediaQuery = window.matchMedia("(prefers-color-scheme: dark)");
    const handleChange = () => applyTheme("auto");
    mediaQuery.addEventListener("change", handleChange);

    return () => {
      mediaQuery.removeEventListener("change", handleChange);
    };
  }, [themeMode]);

  const setTheme = (nextMode) => {
    setThemeMode(nextMode);
    if (typeof window !== "undefined") {
      window.localStorage.setItem(THEME_STORAGE_KEY, nextMode);
    }
  };

  const navLinkClassName = ({ isActive }) =>
    `nav-link px-2 py-1 rounded ${styles.navLink} ${isActive ? `active ${styles.navLinkActive}` : ""}`;

  return (
    <header className="border-bottom">
      <Navbar expand="lg" bg="body-tertiary" className="py-2">
        <Container fluid>
          <Navbar.Brand as={NavLink} to="/" className="fw-semibold">
            Pub Night Picker
          </Navbar.Brand>
          <Navbar.Toggle aria-controls="main-navbar" />
          <Navbar.Collapse id="main-navbar">
            <Nav className="me-auto mb-2 mb-lg-0 flex-wrap">
              <Nav.Link as={NavLink} to="/" end className={navLinkClassName}>Home Page</Nav.Link>
              {canChat && <Nav.Link as={NavLink} to="/chat" end className={navLinkClassName}>Chat Page</Nav.Link>}
              <Nav.Link as={NavLink} to="/venues" end className={navLinkClassName}>
                {loggedIn ? "Manage Venues" : "View Venues"}
              </Nav.Link>
              {admin && <Nav.Link as={NavLink} to="/manage_users" end className={navLinkClassName}>Manage Users</Nav.Link>}
              {!loggedIn && <Nav.Link as={NavLink} to="/login" end className={navLinkClassName}>Login</Nav.Link>}
              {loggedIn && <Nav.Link as={NavLink} to="/active_polls" end className={navLinkClassName}>Active Polls</Nav.Link>}
              <Nav.Link as={NavLink} to="/current_events" end className={navLinkClassName}>Current Events</Nav.Link>
              {loggedIn && <Nav.Link as={NavLink} to="/past_events" end className={navLinkClassName}>Past Events</Nav.Link>}
              {loggedIn && <Nav.Link as={NavLink} to="/preferences" end className={navLinkClassName}>My Preferences</Nav.Link>}
            </Nav>

            <div className={`${styles.controls} d-flex flex-column flex-lg-row align-items-start align-items-lg-center gap-2 gap-lg-3`}>
              <Dropdown align="end">
                <Dropdown.Toggle variant="outline-secondary" size="sm" id="theme-mode-toggle">
                  Theme: {themeLabel}
                </Dropdown.Toggle>
                <Dropdown.Menu>
                  <Dropdown.Item active={themeMode === "light"} onClick={() => setTheme("light")}>Light</Dropdown.Item>
                  <Dropdown.Item active={themeMode === "dark"} onClick={() => setTheme("dark")}>Dark</Dropdown.Item>
                  <Dropdown.Item active={themeMode === "auto"} onClick={() => setTheme("auto")}>Auto</Dropdown.Item>
                </Dropdown.Menu>
              </Dropdown>
              {loggedIn && <LoggedInElement name={name} email={email} />}
            </div>
          </Navbar.Collapse>
        </Container>
      </Navbar>
    </header>
  );
}

export default MainNavigation;
