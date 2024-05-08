import { NavLink } from "react-router-dom";
import { useSelector } from "react-redux";
import classes from "./MainNavigation.module.css";
import { logout } from "../../firebase";
import useAdmin from "../../hooks/useAdmin";
import useKnown from "../../hooks/useKnown";
const LoggedInElement = (params) => {
  return (
    <div className={classes.logged_in}>
      Logged in as
      <div>{params.name}</div>
      <div>{params.email}</div>
      <button className={classes.logged_in__btn} onClick={logout}>
        Logout
      </button>
    </div>
  );
};

function MainNavigation() {
  const name = useSelector((state) => state.auth.name);
  const loggedIn = useSelector((state) => state.auth.loggedIn);
  const admin = useAdmin();
  const known = useKnown();
  const email = useSelector((state) => state.auth.email);

  return (
    <header className={classes.header}>
      <nav className={classes.nav}>
        <ul className={classes.list}>
          <li>
            <NavLink
              to="/"
              end
            >
              Home Page
            </NavLink>
          </li>
          {known && <li>
            <NavLink
              to="/chat"
              className={classes.active}
              end
            >
              Chat Page
            </NavLink>
          </li>}
          <li>
            <NavLink
              to="/pubs"
              className={({ isActive }) =>
                isActive ? classes.active : undefined
              }
              end
            >
              {loggedIn ? "Manage Pubs" : "View Pubs"}
            </NavLink>
          </li>
          {admin && <li>
            <NavLink
              to="/manage_users"
              className={({ isActive }) =>
                isActive ? classes.active : undefined
              }
              end
            >
              Manage Users
            </NavLink>
          </li>}
          {!loggedIn && (
            <li>
              <NavLink
                to="/login"
                className={({ isActive }) =>
                  isActive ? classes.active : undefined
                }
                end
              >
                Login
              </NavLink>
            </li>
          )}
          {loggedIn && (
            <li>
              <NavLink
                to="/active_polls"
                className={({ isActive }) =>
                  isActive ? classes.active : undefined
                }
                end
              >
                Active Polls
              </NavLink>
            </li>
          )}
          <li>
            <NavLink
              to="/current_events"
              className={({ isActive }) =>
                isActive ? classes.active : undefined
              }
              end
            >
              Current Events
            </NavLink>
          </li>
          { loggedIn && <li>
            <NavLink
              to="/past_events"
              className={({ isActive }) =>
                isActive ? classes.active : undefined
              }
              end
            >
              Past Events
            </NavLink>
          </li>}
          {loggedIn && (
            <li>
              <NavLink
                to="/preferences"
                className={({ isActive }) =>
                  isActive ? classes.active : undefined
                }
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
