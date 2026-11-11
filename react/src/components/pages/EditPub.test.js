// @vitest-environment jsdom

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import EditPubPage from "./EditPub";

const {
  navigateMock,
  usePubsStateMock,
  useRoleMock,
  useRouteLoaderDataMock,
} = vi.hoisted(() => {
  return {
    navigateMock: vi.fn(),
    usePubsStateMock: vi.fn(),
    useRoleMock: vi.fn(),
    useRouteLoaderDataMock: vi.fn(),
  };
});

vi.mock("react-router-dom", () => {
  return {
    defer: vi.fn((value) => value),
    useNavigate: () => navigateMock,
    useRouteLoaderData: useRouteLoaderDataMock,
  };
});

vi.mock("../../hooks/usePubs", () => {
  return {
    usePubsState: usePubsStateMock,
  };
});

vi.mock("../../hooks/useRole", () => {
  return {
    default: useRoleMock,
    useRoleStatus: () => ({ hasPermission: useRoleMock(), loading: false }),
  };
});

vi.mock("./PubForm", () => {
  return {
    default: ({ method, pub_object }) => (
      <div data-testid="pub-form">
        <span>{method}</span>
        <span>{pub_object.name}</span>
        <span>{pub_object.venueType}</span>
      </div>
    ),
  };
});

describe("EditPubPage", () => {
  beforeEach(() => {
    navigateMock.mockReset();
    usePubsStateMock.mockReset();
    useRoleMock.mockReset();
    useRouteLoaderDataMock.mockReset();

    useRoleMock.mockReturnValue(true);
    useRouteLoaderDataMock.mockReturnValue({ pub_id: "venue-1" });
  });

  afterEach(() => {
    cleanup();
  });

  it("shows a loading state until the venue document has loaded", () => {
    usePubsStateMock.mockReturnValue({
      venues: {},
      isLoading: true,
      isStale: false,
      errorMessage: null,
    });

    render(<EditPubPage />);

    expect(screen.getByText("Loading venue...")).toBeTruthy();
    expect(screen.queryByTestId("pub-form")).toBeNull();
  });

  it("renders the form only after the loaded venue is available, preserving venueType", () => {
    let currentPubs = {};
    let state = { venues: currentPubs, isLoading: true, isStale: false, errorMessage: null };
    usePubsStateMock.mockImplementation(() => state);

    const { rerender } = render(<EditPubPage />);

    expect(screen.getByText("Loading venue...")).toBeTruthy();
    expect(screen.queryByTestId("pub-form")).toBeNull();

    currentPubs = {
      "venue-1": {
        name: "Cambridge Beer Festival",
        venueType: "event",
      },
    };
    state = { venues: currentPubs, isLoading: false, isStale: false, errorMessage: null };

    rerender(<EditPubPage />);

    expect(screen.queryByText("Loading venue...")).toBeNull();
    expect(screen.getByTestId("pub-form")).toBeTruthy();
    expect(screen.getByText("patch")).toBeTruthy();
    expect(screen.getByText("Cambridge Beer Festival")).toBeTruthy();
    expect(screen.getByText("event")).toBeTruthy();
  });
});
