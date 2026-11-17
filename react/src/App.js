import { createBrowserRouter, Navigate, RouterProvider, useLocation, useParams } from "react-router-dom";
import { useDispatch, useSelector } from "react-redux";
import { useAuthState } from "react-firebase-hooks/auth";
import { auth } from "./firebase";
import { lazy, Suspense, useCallback, useEffect, useState } from "react";
import useRoles from "./hooks/useRoles";
import Login from "./components/login/Login";
import Register from "./components/login/Register";
import Reset from "./components/login/Reset";
import ErrorPage from "./components/pages/Error";
import NewVenuePage, {
  action as manipulateVenueAction,
} from "./components/pages/NewVenue";
import ActivePolls from "./components/pages/ActivePolls";

import RootLayout from "./components/pages/RootLayout";
import EditVenuePage, { loader as venueLoader } from "./components/pages/EditVenue";
import Preferences, {
  action as notificationAction,
} from "./components/pages/Preferences";
import NotificationUnsub, {
  loader as notificationLoader,
} from "./components/pages/NotificationUnsub";
import { useUsersSource } from "./hooks/useUsers";
import Homepage from "./components/pages/Homepage";
import HelpPage from "./components/pages/HelpPage";
import useSelf from "./hooks/useSelf";
import useWebPushLifecycle from "./hooks/useWebPushLifecycle";
import { setRoles } from "./store/authSlice";

const ManageVenues = lazy(() => import("./components/pages/ManageVenues"));
const ManageUsers = lazy(() => import("./components/pages/ManageUsers"));
const ManageUserDetail = lazy(() =>
  import("./components/pages/ManageUsers").then((module) => ({
    default: module.ManageUserDetail,
  })),
);
const PrivacyPage = lazy(() => import("./components/pages/PrivacyPage"));
const ChatPage = lazy(() => import("./components/pages/ChatPage"));
const DiagnosticsPage = lazy(() => import("./components/pages/DiagnosticsPage"));
const AdminLayout = lazy(() => import("./components/pages/AdminLayout"));
const AdminLogsPage = lazy(() =>
  import("./components/pages/DiagnosticsPage").then((module) => ({ default: module.AdminLogsPage })),
);
const AdminConfigPage = lazy(() =>
  import("./components/pages/DiagnosticsPage").then((module) => ({ default: module.AdminConfigPage })),
);
const CurrentMeets = lazy(() => import("./components/pages/CurrentMeets"));
const PastMeets = lazy(() =>
  import("./components/pages/CurrentMeets").then((module) => ({
    default: module.PastMeets,
  })),
);
const WinningVenueStats = lazy(() => import("./components/pages/WinningVenueStats"));
const AttendanceVenueStats = lazy(() => import("./components/pages/AttendanceVenueStats"));

function LazyRoute({ children }) {
  return <Suspense fallback={<div className="p-3">Loading...</div>}>{children}</Suspense>;
}

function LegacyPubRouteRedirect() {
  const { pubId } = useParams();
  if (!pubId) {
    return <Navigate to="/venues" replace />;
  }
  return <Navigate to={`/venues/${pubId}`} replace />;
}

function LegacyUserRouteRedirect() {
  const { userId } = useParams();
  const location = useLocation();
  return <Navigate to={userId ? `/admin/users/${userId}` : "/admin/users"} state={location.state} replace />;
}

function App() {
  const dispatch = useDispatch();
  const [user, loading] = useAuthState(auth);
  const uid = useSelector((state) => state.auth.uid);
  const authUid = user?.uid || null;
  useSelf();
  useWebPushLifecycle(user?.uid || null);
  const [databaseError, setDatabaseError] = useState(null)
  const databaseErrorHandler = useCallback((error) => {
    if (!error) {
      setDatabaseError(null)
      return
    }

    if (typeof error === "string") {
      setDatabaseError({ message: error })
      return
    }

    setDatabaseError(error)
  }, [setDatabaseError])
  const roles = useRoles(user, loading, databaseErrorHandler)
  useEffect(() => {
    if (loading || uid !== authUid) {
      return;
    }
    dispatch(setRoles(roles))
  }, [roles, dispatch, loading, uid, authUid])

  // Get the subscriptions up and running
  useUsersSource()

  const router = createBrowserRouter([
    {
      path: "/",
      element: <RootLayout database_error={databaseError} />,
      errorElement: <ErrorPage />,
      children: [
        { index: true, element: <Homepage /> },
        {
          path: "help",
          element: <HelpPage />,
        },
        {
          path: "privacy",
          element: <LazyRoute><PrivacyPage /></LazyRoute>,
        },
        {
          path: "login",
          element: <Login />,
        },
        {
          path: "register",
          element: <Register />,
        },
        {
          path: "reset",
          element: <Reset />,
        },
        {
          path: "venues",
          children: [
            {
              index: true,
              element: <LazyRoute><ManageVenues /></LazyRoute>,
            },
            {
              path: ":venueId",
              id: "venue_id",
              loader: venueLoader,
              action: manipulateVenueAction,
              element: <EditVenuePage />,
            },
            {
              path: "new",
              element: <NewVenuePage />,
              action: manipulateVenueAction,
            },
          ],
        },
        {
          path: "pubs",
          children: [
            {
              index: true,
              element: <Navigate to="/venues" replace />,
            },
            {
              path: "new",
              element: <Navigate to="/venues/new" replace />,
            },
            {
              path: ":pubId",
              element: <LegacyPubRouteRedirect />,
            },
          ],
        },
        {
          path: "admin",
          element: <LazyRoute><AdminLayout /></LazyRoute>,
          children: [
            { index: true, element: <Navigate to="/admin/users" replace /> },
            { path: "users", element: <LazyRoute><ManageUsers /></LazyRoute> },
            { path: "users/:userId", element: <LazyRoute><ManageUserDetail /></LazyRoute> },
            { path: "diagnostics", element: <LazyRoute><DiagnosticsPage /></LazyRoute> },
            { path: "logs", element: <LazyRoute><AdminLogsPage /></LazyRoute> },
            { path: "config", element: <LazyRoute><AdminConfigPage /></LazyRoute> },
          ],
        },
        {
          path: "manage_users",
          children: [
            {
              index: true,
              element: <LegacyUserRouteRedirect />,
            },
            {
              path: ":userId",
              element: <LegacyUserRouteRedirect />,
            },
          ],
        },
        {
          path: "diagnostics",
          element: <Navigate to="/admin/diagnostics" replace />,
        },
        {
          path: "chat",
          element: <LazyRoute><ChatPage /></LazyRoute>
        },
        {
          path: "chat/meet/:meetId",
          element: <LazyRoute><ChatPage /></LazyRoute>
        },
        {
          path: "active_polls",
          element: <ActivePolls />,
        },
        {
          path: "current_meets",
          element: <LazyRoute><CurrentMeets /></LazyRoute>,
        },
        {
          path: "past_meets",
          element: <LazyRoute><PastMeets /></LazyRoute>,
        },
        {
          path: "stats",
          children: [
            {
              index: true,
              element: <Navigate to="/stats/winning_venues" replace />,
            },
            {
              path: "winning_venues",
              element: <LazyRoute><WinningVenueStats /></LazyRoute>,
            },
            {
              path: "attendance",
              element: <LazyRoute><AttendanceVenueStats /></LazyRoute>,
            },
          ],
        },
        {
          path: "preferences",
          children: [
            {
              index: true,
              action: notificationAction,
              element: <Preferences />,
            },
            {
              path: ":userId",
              id: "user_id",
              loader: notificationLoader,
              action: notificationAction,
              element: <NotificationUnsub />,
            },
          ],
        },
      ],
    },
  ]);
  return <RouterProvider router={router} />;
}
export default App;
