// @vitest-environment jsdom

import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import AttendanceVenueStats, { AttendanceVenueStatsPage } from "./AttendanceVenueStats";

const {
  usePubsMock,
  useAttendanceVenueStatsMock,
  useRoleMock,
} = vi.hoisted(() => {
  return {
    usePubsMock: vi.fn(),
    useAttendanceVenueStatsMock: vi.fn(),
    useRoleMock: vi.fn(),
  };
});

vi.mock("../../hooks/usePubs", () => {
  return {
    default: usePubsMock,
  };
});

vi.mock("../../hooks/useAttendanceVenueStats", () => {
  return {
    default: useAttendanceVenueStatsMock,
  };
});

vi.mock("../../hooks/useRole", () => {
  return {
    default: useRoleMock,
  };
});

vi.mock("../UI/Modal", () => {
  return {
    default: ({ children }) => <div>{children}</div>,
  };
});

function renderAttendancePage(initialEntry = "/stats/attendance") {
  return render(
    <MemoryRouter initialEntries={[initialEntry]}>
      <Routes>
        <Route path="/stats/attendance" element={<AttendanceVenueStatsPage />} />
      </Routes>
    </MemoryRouter>
  );
}

describe("AttendanceVenueStatsPage", () => {
  afterEach(() => {
    cleanup();
  });

  beforeEach(() => {
    usePubsMock.mockReset();
    useAttendanceVenueStatsMock.mockReset();
    useRoleMock.mockReset();
    useRoleMock.mockReturnValue(true);

    usePubsMock.mockReturnValue({
      "venue-1": { name: "The Maypole" },
      "venue-2": { name: "The Anchor" },
    });

    useAttendanceVenueStatsMock.mockReturnValue({
      countsByVenueId: {
        "venue-1": 4,
      },
      lastDateByVenueId: {
        "venue-1": "2026-04-01",
      },
      isLoading: false,
      errorMessage: null,
      startDate: "2025-05-11",
      endDate: "2026-05-11",
    });
  });

  it("uses default query values when params are missing", () => {
    renderAttendancePage();

    expect(screen.getByText(/last year/i)).toBeTruthy();
    expect(screen.getByText(/Showing 5 venues per list/i)).toBeTruthy();
  });

  it("reads limit and years from query params", () => {
    renderAttendancePage("/stats/attendance?limit=8&years=2");

    expect(screen.getByText(/last 2 years/i)).toBeTruthy();
    expect(screen.getByText(/Showing 8 venues per list/i)).toBeTruthy();
  });

  it("applies settings from the modal and updates page text", () => {
    renderAttendancePage();

    fireEvent.click(screen.getByRole("button", { name: /adjust window/i }));
    fireEvent.change(screen.getByLabelText(/venues to show/i), { target: { value: "6" } });
    fireEvent.change(screen.getByLabelText(/time window/i), { target: { value: "4" } });
    fireEvent.click(screen.getByRole("button", { name: /apply/i }));

    expect(screen.getByText(/last 4 years/i)).toBeTruthy();
    expect(screen.getByText(/Showing 6 venues per list/i)).toBeTruthy();
  });

  it("renders an error alert when attendance stats query fails", () => {
    useAttendanceVenueStatsMock.mockReturnValue({
      countsByVenueId: {},
      lastDateByVenueId: {},
      isLoading: false,
      errorMessage: "Unable to load attendance stats right now.",
      startDate: "2025-05-11",
      endDate: "2026-05-11",
    });

    renderAttendancePage();

    expect(screen.getByText(/Unable to load attendance stats right now/i)).toBeTruthy();
  });

  it("redirects to home when user is not admin", () => {
    useRoleMock.mockReturnValue(false);

    render(
      <MemoryRouter initialEntries={["/stats/attendance"]}>
        <Routes>
          <Route path="/stats/attendance" element={<AttendanceVenueStats />} />
          <Route path="/" element={<div>Home</div>} />
        </Routes>
      </MemoryRouter>
    );

    expect(screen.getByText("Home")).toBeTruthy();
    expect(screen.queryByText(/Attendance Venue Stats/i)).toBeNull();
  });
});
