// @vitest-environment jsdom

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import EditVenuePage, { loader } from "./EditVenue";

const {
  navigateMock,
  useVenuesStateMock,
  useRoleMock,
  useRouteLoaderDataMock,
} = vi.hoisted(() => {
  return {
    navigateMock: vi.fn(),
    useVenuesStateMock: vi.fn(),
    useRoleMock: vi.fn(),
    useRouteLoaderDataMock: vi.fn(),
  };
});

vi.mock("react-router-dom", () => {
  return {
    useNavigate: () => navigateMock,
    useRouteLoaderData: useRouteLoaderDataMock,
  };
});

vi.mock("../../hooks/useVenues", () => {
  return {
    useVenuesState: useVenuesStateMock,
  };
});

vi.mock("../../hooks/useRole", () => {
  return {
    default: useRoleMock,
    useRoleStatus: () => ({ hasPermission: useRoleMock(), loading: false }),
  };
});

vi.mock("./VenueForm", () => {
  return {
    default: ({ method, venueObject }) => (
      <div data-testid="pub-form">
        <span>{method}</span>
        <span>{venueObject.name}</span>
        <span>{venueObject.venueType}</span>
        <span>{venueObject.website}</span>
      </div>
    ),
  };
});

describe("EditVenuePage", () => {
  it("returns the venue id as loader data", async () => {
    const data = await loader({ params: { venueId: "venue-1" } });

    expect(data.venue_id).toBe("venue-1");
  });

  beforeEach(() => {
    navigateMock.mockReset();
    useVenuesStateMock.mockReset();
    useRoleMock.mockReset();
    useRouteLoaderDataMock.mockReset();

    useRoleMock.mockReturnValue(true);
    useRouteLoaderDataMock.mockReturnValue({ venue_id: "venue-1" });
  });

  afterEach(() => {
    cleanup();
  });

  it("shows a loading state until the venue document has loaded", () => {
    useVenuesStateMock.mockReturnValue({
      venues: {},
      isLoading: true,
      isStale: false,
      errorMessage: null,
    });

    render(<EditVenuePage />);

    expect(screen.getByText("Loading venue...")).toBeTruthy();
    expect(screen.queryByTestId("pub-form")).toBeNull();
  });

  it("renders the form only after the loaded venue is available, preserving venueType", () => {
    let currentPubs = {};
    let state = { venues: currentPubs, isLoading: true, isStale: false, errorMessage: null };
    useVenuesStateMock.mockImplementation(() => state);

    const { rerender } = render(<EditVenuePage />);

    expect(screen.getByText("Loading venue...")).toBeTruthy();
    expect(screen.queryByTestId("pub-form")).toBeNull();

    currentPubs = {
      "venue-1": {
        name: "Cambridge Beer Festival",
        venueType: "event",
        website: "https://festival.example",
      },
    };
    state = { venues: currentPubs, isLoading: false, isStale: false, errorMessage: null };

    rerender(<EditVenuePage />);

    expect(screen.queryByText("Loading venue...")).toBeNull();
    expect(screen.getByTestId("pub-form")).toBeTruthy();
    expect(screen.getByText("patch")).toBeTruthy();
    expect(screen.getByText("Cambridge Beer Festival")).toBeTruthy();
    expect(screen.getByText("event")).toBeTruthy();
    expect(screen.getByText("https://festival.example")).toBeTruthy();
  });
});
