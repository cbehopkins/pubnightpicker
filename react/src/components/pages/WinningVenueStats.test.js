// @vitest-environment jsdom

import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import WinningVenueStats, { WinningVenueStatsPage } from "./WinningVenueStats";

const {
  usePubsMock,
  useWinningVenueStatsMock,
  useRoleMock,
} = vi.hoisted(() => {
  return {
    usePubsMock: vi.fn(),
    useWinningVenueStatsMock: vi.fn(),
    useRoleMock: vi.fn(),
  };
});

vi.mock("../../hooks/usePubs", () => {
  return {
    default: usePubsMock,
  };
});

vi.mock("../../hooks/useWinningVenueStats", () => {
  return {
    default: useWinningVenueStatsMock,
  };
});

vi.mock("../../hooks/useRole", () => {
  return {
    default: useRoleMock,
    useRoleStatus: () => ({ hasPermission: useRoleMock(), loading: false }),
  };
});

vi.mock("../UI/Modal", () => {
  return {
    default: ({ children }) => <div>{children}</div>,
  };
});

function renderWinningPage(initialEntry = "/stats/winning_venues") {
  return render(
    <MemoryRouter initialEntries={[initialEntry]}>
      <Routes>
        <Route path="/stats/winning_venues" element={<WinningVenueStatsPage />} />
      </Routes>
    </MemoryRouter>
  );
}

describe("WinningVenueStatsPage", () => {
  afterEach(() => {
    cleanup();
  });

  beforeEach(() => {
    usePubsMock.mockReset();
    useWinningVenueStatsMock.mockReset();
    useRoleMock.mockReset();
    useRoleMock.mockReturnValue(true);

    usePubsMock.mockReturnValue({
      "venue-1": { name: "The Maypole" },
      "venue-2": { name: "The Anchor" },
    });

    useWinningVenueStatsMock.mockReturnValue({
      polls: {
        p1: { selected: "venue-1", date: "2026-04-01" },
      },
      isLoading: false,
      errorMessage: null,
      startDate: "2025-05-11",
      endDate: "2026-05-11",
    });
  });

  it("uses default query values when params are missing", () => {
    renderWinningPage();

    expect(screen.getByText(/last year/i)).toBeTruthy();
    expect(screen.getByText(/Showing 5 venues per list/i)).toBeTruthy();
  });

  it("reads limit and years from query params", () => {
    renderWinningPage("/stats/winning_venues?limit=7&years=2");

    expect(screen.getByText(/last 2 years/i)).toBeTruthy();
    expect(screen.getByText(/Showing 7 venues per list/i)).toBeTruthy();
  });

  it("applies settings from the modal and updates page text", () => {
    renderWinningPage();

    fireEvent.click(screen.getByRole("button", { name: /adjust window/i }));
    fireEvent.change(screen.getByLabelText(/venues to show/i), { target: { value: "9" } });
    fireEvent.change(screen.getByLabelText(/time window/i), { target: { value: "3" } });
    fireEvent.click(screen.getByRole("button", { name: /apply/i }));

    expect(screen.getByText(/last 3 years/i)).toBeTruthy();
    expect(screen.getByText(/Showing 9 venues per list/i)).toBeTruthy();
  });

  it("renders an error alert when stats query fails", () => {
    useWinningVenueStatsMock.mockReturnValue({
      polls: {},
      isLoading: false,
      errorMessage: "Unable to load winning venue stats right now.",
      startDate: "2025-05-11",
      endDate: "2026-05-11",
    });

    renderWinningPage();

    expect(screen.getByText(/Unable to load winning venue stats right now/i)).toBeTruthy();
  });

  it("redirects to home when user is not admin", () => {
    useRoleMock.mockReturnValue(false);

    render(
      <MemoryRouter initialEntries={["/stats/winning_venues"]}>
        <Routes>
          <Route path="/stats/winning_venues" element={<WinningVenueStats />} />
          <Route path="/" element={<div>Home</div>} />
        </Routes>
      </MemoryRouter>
    );

    expect(screen.getByText("Home")).toBeTruthy();
    expect(screen.queryByText(/Winning Venue Stats/i)).toBeNull();
  });
});
