import { createBrowserRouter, Navigate, RouterProvider, useParams } from "react-router-dom";
import { useDispatch } from "react-redux";
import { useAuthState } from "react-firebase-hooks/auth";
import { auth } from "./firebase";
import { lazy, Suspense, useCallback, useEffect, useState } from "react";
import useRoles from "./hooks/useRoles";
import Login from "./components/login/Login";
import Register from "./components/login/Register";
import Reset from "./components/login/Reset";
import ErrorPage from "./components/pages/Error";
import NewPubPage, {
  action as manipulatePubAction,
} from "./components/pages/NewPub";
import ActivePolls from "./components/pages/ActivePolls";

import RootLayout from "./components/pages/RootLayout";
import EditPubPage, { loader as pubLoader } from "./components/pages/EditPub";
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

const ManagePubs = lazy(() => import("./components/pages/ManagePubs"));
const ManageUsers = lazy(() => import("./components/pages/ManageUsers"));
const ManageUserDetail = lazy(() =>
  import("./components/pages/ManageUsers").then((module) => ({
    default: module.ManageUserDetail,
  })),
);
const PrivacyPage = lazy(() => import("./components/pages/PrivacyPage"));
const ChatPage = lazy(() => import("./components/pages/ChatPage"));
const CurrentEvents = lazy(() => import("./components/pages/CurrentEvents"));
const PastEvents = lazy(() =>
  import("./components/pages/CurrentEvents").then((module) => ({
    default: module.PastEvents,
  })),
);

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

function App() {
  const dispatch = useDispatch();
  const [user, loading] = useAuthState(auth);
  useSelf();
  useWebPushLifecycle(user?.uid || null);
  const [databaseError, setDatabaseError] = useState("")
  const databaseErrorHandler = useCallback((error) => {
    setDatabaseError(error)
  }, [setDatabaseError])
  const roles = useRoles(user, loading, databaseErrorHandler)
  useEffect(() => {
    dispatch(setRoles(roles))
  }, [roles, dispatch])

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
              element: <LazyRoute><ManagePubs /></LazyRoute>,
            },
            {
              path: ":pubId",
              id: "pub_id",
              loader: pubLoader,
              action: manipulatePubAction,
              element: <EditPubPage />,
            },
            {
              path: "new",
              element: <NewPubPage />,
              action: manipulatePubAction,
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
          path: "manage_users",
          children: [
            {
              index: true,
              element: <LazyRoute><ManageUsers /></LazyRoute>,
            },
            {
              path: ":userId",
              element: <LazyRoute><ManageUserDetail /></LazyRoute>,
            },
          ],
        },
        {
          path: "chat",
          element: <LazyRoute><ChatPage /></LazyRoute>
        },
        {
          path: "active_polls",
          element: <ActivePolls />,
        },
        {
          path: "current_events",
          element: <LazyRoute><CurrentEvents /></LazyRoute>,
        },
        {
          path: "past_events",
          element: <LazyRoute><PastEvents /></LazyRoute>,
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
