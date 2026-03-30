// @vitest-environment jsdom

import { render } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import App from "./App";

const {
  createBrowserRouterMock,
  useDispatchMock,
  useAuthStateMock,
  useRolesMock,
  useUsersSourceMock,
  useSelfMock,
} = vi.hoisted(() => {
  return {
    createBrowserRouterMock: vi.fn(() => ({ id: "router" })),
    useDispatchMock: vi.fn(),
    useAuthStateMock: vi.fn(),
    useRolesMock: vi.fn(),
    useUsersSourceMock: vi.fn(),
    useSelfMock: vi.fn(),
  };
});

vi.mock("react-router-dom", () => {
  const Navigate = (props) => null;
  return {
    createBrowserRouter: createBrowserRouterMock,
    RouterProvider: () => <div data-testid="router-provider" />,
    Navigate,
    useParams: vi.fn(() => ({ pubId: "venue-1" })),
  };
});

vi.mock("react-redux", () => {
  return {
    useDispatch: useDispatchMock,
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

vi.mock("./store/authSlice", () => {
  return {
    setRoles: (roles) => ({ type: "auth/setRoles", payload: roles }),
  };
});

vi.mock("./components/login/Login", () => ({ default: () => null }));
vi.mock("./components/login/Register", () => ({ default: () => null }));
vi.mock("./components/login/Reset", () => ({ default: () => null }));
vi.mock("./components/pages/ManagePubs", () => ({ default: () => null }));
vi.mock("./components/pages/ManageUsers", () => ({ default: () => null }));
vi.mock("./components/pages/Error", () => ({ default: () => null }));
vi.mock("./components/pages/NewPub", () => ({
  default: () => null,
  action: vi.fn(),
}));
vi.mock("./components/pages/ActivePolls", () => ({ default: () => null }));
vi.mock("./components/pages/CurrentEvents", () => ({
  default: () => null,
  PastEvents: () => null,
}));
vi.mock("./components/pages/RootLayout", () => ({ default: () => null }));
vi.mock("./components/pages/EditPub", () => ({
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
    createBrowserRouterMock.mockClear();
    useDispatchMock.mockReset();
    useAuthStateMock.mockReset();
    useRolesMock.mockReset();
    useUsersSourceMock.mockReset();
    useSelfMock.mockReset();

    useDispatchMock.mockReturnValue(vi.fn());
    useAuthStateMock.mockReturnValue([null, false]);
    useRolesMock.mockReturnValue({});
  });

  it("keeps legacy /pubs routes redirecting to /venues equivalents", () => {
    render(<App />);

    const routeConfig = createBrowserRouterMock.mock.calls[0][0];
    const rootRoute = routeConfig[0];
    const pubsRoute = rootRoute.children.find((child) => child.path === "pubs");

    expect(pubsRoute).toBeTruthy();
    expect(pubsRoute.children.find((child) => child.path === "new").element.props.to).toBe("/venues/new");
    expect(pubsRoute.children.find((child) => child.path === ":pubId")).toBeTruthy();

    const indexChild = pubsRoute.children.find((child) => child.index);
    expect(indexChild.element.props.to).toBe("/venues");
  });
});
