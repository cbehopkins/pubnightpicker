// @vitest-environment jsdom

import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import App from "./App";
import authReducer, { authAdded } from "./store/authSlice";
import { setGroupId } from "./store/groupSlice";
import { useLocation, useParams } from "react-router-dom";

const {
  createBrowserRouterMock,
  useDispatchMock,
  useSelectorMock,
  useAuthStateMock,
  useRolesMock,
  useUsersSourceMock,
  useSelfMock,
  useWebPushLifecycleMock,
} = vi.hoisted(() => {
  return {
    createBrowserRouterMock: vi.fn(() => ({ id: "router" })),
    useDispatchMock: vi.fn(),
    useSelectorMock: vi.fn(),
    useAuthStateMock: vi.fn(),
    useRolesMock: vi.fn(),
    useUsersSourceMock: vi.fn(),
    useSelfMock: vi.fn(),
    useWebPushLifecycleMock: vi.fn(),
  };
});

vi.mock("react-router-dom", () => {
  const Navigate = () => null;
  return {
    createBrowserRouter: createBrowserRouterMock,
    RouterProvider: () => <div data-testid="router-provider" />,
    Navigate,
    useLocation: vi.fn(() => ({ state: null })),
    useParams: vi.fn(() => ({ pubId: "venue-1" })),
  };
});

vi.mock("react-redux", () => {
  return {
    useDispatch: useDispatchMock,
    useSelector: useSelectorMock,
  };
});

vi.mock("react-firebase-hooks/auth", () => {
  return {
    useAuthState: useAuthStateMock,
  };
});

vi.mock("./firebase", () => {
  return {
    auth: {},
  };
});

vi.mock("./hooks/useRoles", () => {
  return {
    default: useRolesMock,
  };
});

vi.mock("./hooks/useUsers", () => {
  return {
    useUsersSource: useUsersSourceMock,
  };
});

vi.mock("./hooks/useSelf", () => {
  return {
    default: useSelfMock,
  };
});

vi.mock("./hooks/useWebPushLifecycle", () => {
  return {
    default: useWebPushLifecycleMock,
  };
});

vi.mock("./components/login/Login", () => ({ default: () => null }));
vi.mock("./components/login/Register", () => ({ default: () => null }));
vi.mock("./components/login/Reset", () => ({ default: () => null }));
vi.mock("./components/pages/ManageVenues", () => ({ default: () => null }));
vi.mock("./components/pages/ManageUsers", () => ({
  default: () => null,
  ManageUserDetail: () => null,
}));
vi.mock("./components/pages/Error", () => ({ default: () => null }));
vi.mock("./components/pages/NewVenue", () => ({
  default: () => null,
  action: vi.fn(),
}));
vi.mock("./components/pages/ActivePolls", () => ({ default: () => null }));
vi.mock("./components/pages/CurrentMeets", () => ({
  default: () => null,
  PastMeets: () => null,
}));
vi.mock("./components/pages/WinningVenueStats", () => ({
  default: () => null,
}));
vi.mock("./components/pages/AttendanceVenueStats", () => ({
  default: () => null,
}));
vi.mock("./components/pages/RootLayout", () => ({ default: () => null }));
vi.mock("./components/pages/EditVenue", () => ({
  default: () => null,
  loader: vi.fn(),
}));
vi.mock("./components/pages/Preferences", () => ({
  default: () => null,
  action: vi.fn(),
}));
vi.mock("./components/pages/NotificationUnsub", () => ({
  default: () => null,
  loader: vi.fn(),
}));
vi.mock("./components/pages/Homepage", () => ({ default: () => null }));
vi.mock("./components/pages/ChatPage", () => ({ default: () => null }));

describe("App routes", () => {
  beforeEach(() => {
    vi.unstubAllGlobals();
    createBrowserRouterMock.mockClear();
    useDispatchMock.mockReset();
    useSelectorMock.mockReset();
    useAuthStateMock.mockReset();
    useRolesMock.mockReset();
    useUsersSourceMock.mockReset();
    useSelfMock.mockReset();
    useWebPushLifecycleMock.mockReset();

    useDispatchMock.mockReturnValue(vi.fn());
    useSelectorMock.mockReturnValue(null);
    useAuthStateMock.mockReturnValue([null, false]);
    useRolesMock.mockReturnValue({});
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ groups: [{ id: "group-1", name: "ampubnight", visibility: "public" }] }),
    }));
  });

  async function waitForApplication() {
    await waitFor(() => expect(createBrowserRouterMock).toHaveBeenCalled());
  }

  it("applies roles when Redux identity arrives after the roles snapshot", async () => {
    let state = { auth: authReducer(undefined, { type: "init" }) };
    const dispatch = vi.fn((action) => {
      state = { auth: authReducer(state.auth, action) };
    });
    const roles = { admin: true, canManagePubs: true };
    useDispatchMock.mockReturnValue(dispatch);
    useSelectorMock.mockImplementation((selector) => selector(state));
    useAuthStateMock.mockReturnValue([{ uid: "admin-user" }, false]);
    useRolesMock.mockReturnValue(roles);

    const { rerender } = render(<App />);
    await waitForApplication();
    dispatch(authAdded({ uid: "admin-user" }));
    rerender(<App />);

    expect(state.auth.roles).toEqual(roles);
  });

  it("blocks the application while the default Group is being resolved", () => {
    vi.stubGlobal("fetch", vi.fn(() => new Promise(() => { })));

    render(<App />);

    expect(screen.getByRole("status")).toHaveTextContent("Resolving the default Group...");
    expect(createBrowserRouterMock).not.toHaveBeenCalled();
  });

  it("discovers the exact default Group before entering the application", async () => {
    const dispatch = vi.fn();
    useDispatchMock.mockReturnValue(dispatch);
    render(<App />);

    await waitForApplication();

    expect(fetch).toHaveBeenCalledWith(
      "http://localhost:8081/api/groups?name=ampubnight",
      expect.objectContaining({ method: "GET" }),
    );
    await waitFor(() => expect(dispatch).toHaveBeenCalledWith(setGroupId("group-1")));
    expect(screen.getByTestId("router-provider")).toBeTruthy();
  });

  it.each([
    ["missing", [], 'The default Group "ampubnight" could not be found.'],
    ["ambiguous", [
      { id: "group-1", name: "ampubnight", visibility: "public" },
      { id: "group-2", name: "ampubnight", visibility: "public" },
    ], 'More than one Group named "ampubnight" was returned.'],
  ])("blocks entry when the default Group is %s", async (_state, groups, message) => {
    fetch.mockResolvedValueOnce({ ok: true, json: async () => ({ groups }) });

    render(<App />);

    expect(await screen.findByRole("alert")).toHaveTextContent(message);
    expect(createBrowserRouterMock).not.toHaveBeenCalled();
  });

  it("shows request failures and permits an explicit successful retry", async () => {
    fetch.mockRejectedValueOnce(new Error("offline"));

    render(<App />);

    expect(await screen.findByRole("alert")).toHaveTextContent("Unable to resolve the default Group.");
    expect(createBrowserRouterMock).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));

    await waitForApplication();
    expect(fetch).toHaveBeenCalledTimes(2);
  });

  it("does not use a persisted Group ID when fresh discovery fails", async () => {
    vi.stubGlobal("localStorage", {
      getItem: vi.fn(() => "stale-group-id"),
      setItem: vi.fn(),
      removeItem: vi.fn(),
      clear: vi.fn(),
    });
    fetch.mockRejectedValueOnce(new Error("offline"));

    render(<App />);

    expect(await screen.findByRole("alert")).toHaveTextContent("Unable to resolve the default Group.");
    expect(createBrowserRouterMock).not.toHaveBeenCalled();
  });

  it("rediscovers the Group after remount", async () => {
    fetch
      .mockResolvedValueOnce({ ok: true, json: async () => ({ groups: [{ id: "first-id", name: "ampubnight", visibility: "public" }] }) })
      .mockResolvedValueOnce({ ok: true, json: async () => ({ groups: [{ id: "second-id", name: "ampubnight", visibility: "public" }] }) });

    const firstMount = render(<App />);
    await waitForApplication();
    firstMount.unmount();
    createBrowserRouterMock.mockClear();

    render(<App />);
    await waitForApplication();

    expect(fetch).toHaveBeenCalledTimes(2);
    expect(fetch.mock.calls[1][0]).toBe("http://localhost:8081/api/groups?name=ampubnight");
  });

  it("keeps legacy /pubs routes redirecting to /venues equivalents", async () => {
    render(<App />);
    await waitForApplication();

    const routeConfig = createBrowserRouterMock.mock.calls[0][0];
    const rootRoute = routeConfig[0];
    const pubsRoute = rootRoute.children.find((child) => child.path === "pubs");

    expect(pubsRoute).toBeTruthy();
    expect(pubsRoute.children.find((child) => child.path === "new").element.props.to).toBe("/venues/new");
    expect(pubsRoute.children.find((child) => child.path === ":pubId")).toBeTruthy();

    const indexChild = pubsRoute.children.find((child) => child.index);
    expect(indexChild.element.props.to).toBe("/venues");
  });

  it("registers admin sections and redirects the old diagnostics URL", async () => {
    render(<App />);
    await waitForApplication();

    const children = createBrowserRouterMock.mock.calls[0][0][0].children;
    const admin = children.find((child) => child.path === "admin");
    expect(admin.element).toBeTruthy();
    expect(admin.children.find((child) => child.index).element.props.to).toBe("/admin/users");
    expect(admin.children.filter((child) => child.path).map((child) => child.path)).toEqual([
      "users", "users/:userId", "diagnostics", "logs", "config",
    ]);
    expect(children.find((child) => child.path === "diagnostics").element.props.to).toBe("/admin/diagnostics");
    const legacyUsers = children.find((child) => child.path === "manage_users");
    expect(legacyUsers.children.find((child) => child.index).element.type().props.to).toBe("/admin/users");
    vi.mocked(useParams).mockReturnValue({ userId: "user-123" });
    const state = { lastDeleteSummaryLines: ["Deletion complete"] };
    vi.mocked(useLocation).mockReturnValue({ state });
    const redirect = legacyUsers.children.find((child) => child.path === ":userId").element.type();
    expect(redirect.props.to).toBe("/admin/users/user-123");
    expect(redirect.props.state).toBe(state);
    expect(redirect.props.replace).toBe(true);
    vi.mocked(useParams).mockReturnValue({ pubId: "venue-1" });
    vi.mocked(useLocation).mockReturnValue({ state: null });
  });

  it("registers the dedicated Meet Chat route", async () => {
    render(<App />);
    await waitForApplication();

    const routeConfig = createBrowserRouterMock.mock.calls[0][0];
    const rootRoute = routeConfig[0];
    const eventChatRoute = rootRoute.children.find((child) => child.path === "chat/meet/:meetId");

    expect(eventChatRoute).toBeTruthy();
  });

  it("keeps current and past Meet routes available after discovery", async () => {
    render(<App />);
    await waitForApplication();

    const routePaths = createBrowserRouterMock.mock.calls[0][0][0].children.map((route) => route.path);
    expect(routePaths).toContain("current_meets");
    expect(routePaths).toContain("past_meets");
  });

  it("registers the winning venue stats route", async () => {
    render(<App />);
    await waitForApplication();

    const routeConfig = createBrowserRouterMock.mock.calls[0][0];
    const rootRoute = routeConfig[0];
    const statsRoute = rootRoute.children.find((child) => child.path === "stats");

    expect(statsRoute).toBeTruthy();
    expect(statsRoute.children.find((child) => child.path === "winning_venues")).toBeTruthy();
    expect(statsRoute.children.find((child) => child.path === "attendance")).toBeTruthy();
  });

  it("starts web push lifecycle with the current user uid", async () => {
    useAuthStateMock.mockReturnValue([{ uid: "user-123" }, false]);

    render(<App />);

    await waitForApplication();
    expect(useWebPushLifecycleMock).toHaveBeenCalledWith("user-123");
  });
});
