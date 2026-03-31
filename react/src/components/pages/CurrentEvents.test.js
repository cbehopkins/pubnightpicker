// @vitest-environment jsdom

import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import CurrentEvents, { PastEvents } from "./CurrentEvents";

const {
  reschedulePollMock,
  deletePollMock,
  useFutureCompletePollsMock,
  usePastCompletePollsMock,
  usePubsMock,
  useVotesMock,
  useAttendanceMock,
  useRoleMock,
  useSelectorMock,
  setAttendanceStatusMock,
  clearAttendanceMock,
  runAttendanceActionMock,
} = vi.hoisted(() => {
  return {
    reschedulePollMock: vi.fn(async () => undefined),
    deletePollMock: vi.fn(async () => undefined),
    useFutureCompletePollsMock: vi.fn(),
    usePastCompletePollsMock: vi.fn(),
    usePubsMock: vi.fn(),
    useVotesMock: vi.fn(),
    useAttendanceMock: vi.fn(),
    useRoleMock: vi.fn(),
    useSelectorMock: vi.fn(),
    setAttendanceStatusMock: vi.fn(async () => undefined),
    clearAttendanceMock: vi.fn(async () => undefined),
    runAttendanceActionMock: vi.fn(async (fn) => fn()),
  };
});

vi.mock("../../dbtools/polls", () => {
  return {
    deletePoll: deletePollMock,
    reschedule_a_poll: reschedulePollMock,
  };
});

vi.mock("../../hooks/usePolls", () => {
  return {
    useFutureCompletePolls: useFutureCompletePollsMock,
    usePastCompletePolls: usePastCompletePollsMock,
  };
});

vi.mock("../../hooks/usePubs", () => {
  return {
    default: usePubsMock,
  };
});

vi.mock("../../hooks/useVotes", () => {
  return {
    default: useVotesMock,
  };
});

vi.mock("../../hooks/useAttendance", () => {
  return {
    default: useAttendanceMock,
  };
});

vi.mock("../../hooks/useRole", () => {
  return {
    default: useRoleMock,
  };
});

vi.mock("react-redux", () => {
  return {
    useSelector: useSelectorMock,
  };
});

vi.mock("../../utils/attendance", () => {
  return {
    runAttendanceAction: runAttendanceActionMock,
  };
});

vi.mock("../UI/AttendanceActions", () => {
  return {
    default: ({ onSetStatus, onClear }) => {
      return (
        <div data-testid="attendance-actions">
          <button onClick={() => onSetStatus("canCome")}>set-status</button>
          <button onClick={onClear}>clear-status</button>
        </div>
      );
    },
  };
});

vi.mock("../UI/ShowAttendance", () => {
  return {
    default: () => <div data-testid="show-attendance" />,
  };
});

vi.mock("../UI/ConfirmModal", () => {
  return {
    default: () => <div data-testid="confirm-modal" />,
    QuestionRender: ({ children }) => <div>{children}</div>,
  };
});

vi.mock("../UI/Modal", () => {
  return {
    default: ({ children }) => <div>{children}</div>,
  };
});

function createFuturePollData(pollValue) {
  return {
    *sortedByDate() {
      yield ["poll-1", pollValue];
    },
  };
}

function createPastPollData(pollValue) {
  return {
    *sortedByDate() {
      yield ["poll-1", pollValue];
    },
  };
}

describe("CurrentEvents", () => {
  beforeEach(() => {
    useFutureCompletePollsMock.mockReset();
    usePastCompletePollsMock.mockReset();
    usePubsMock.mockReset();
    useVotesMock.mockReset();
    useAttendanceMock.mockReset();
    useRoleMock.mockReset();
    useSelectorMock.mockReset();
    reschedulePollMock.mockReset();
    deletePollMock.mockReset();
    setAttendanceStatusMock.mockReset();
    clearAttendanceMock.mockReset();
    runAttendanceActionMock.mockReset();

    runAttendanceActionMock.mockImplementation(async (fn) => fn());
    useSelectorMock.mockReturnValue("user-1");
    useVotesMock.mockReturnValue([{}]);
    useAttendanceMock.mockReturnValue([{}, setAttendanceStatusMock, clearAttendanceMock]);
    useRoleMock.mockReturnValue(false);
    usePastCompletePollsMock.mockReturnValue({
      pollData: createPastPollData({
        selected: "venue-main",
        date: "2026-03-20",
      }),
      hasNextPage: false,
      hasPreviousPage: false,
      goToNextPage: vi.fn(),
      goToPreviousPage: vi.fn(),
      pageIndex: 0,
      isLoading: false,
    });
  });

  afterEach(() => {
    cleanup();
  });

  it("shows only the main venue section when restaurant field is absent", () => {
    useFutureCompletePollsMock.mockReturnValue(createFuturePollData({
      selected: "venue-main",
      date: "2026-03-30",
    }));
    usePubsMock.mockReturnValue({
      "venue-main": { name: "The Maypole", venueType: "pub" },
    });

    render(<CurrentEvents />);

    expect(screen.getByText("The Maypole")).toBeTruthy();
    expect(screen.queryByText(/Restaurant:/)).toBeNull();
    expect(screen.getAllByTestId("attendance-actions")).toHaveLength(1);
  });

  it("shows a separate restaurant section when restaurant field is present", () => {
    useFutureCompletePollsMock.mockReturnValue(createFuturePollData({
      selected: "venue-main",
      restaurant: "venue-restaurant",
      restaurant_time: "18:30",
      date: "2026-03-30",
    }));
    usePubsMock.mockReturnValue({
      "venue-main": { name: "The Maypole", venueType: "pub" },
      "venue-restaurant": { name: "Bistro 12", venueType: "restaurant" },
    });

    render(<CurrentEvents />);

    expect(screen.getByText("The Maypole")).toBeTruthy();
    expect(screen.getByText(/Restaurant:\s*Bistro 12\s*\(18:30\)/)).toBeTruthy();
    expect(screen.getAllByTestId("attendance-actions")).toHaveLength(2);
  });

  it("hides restaurant time when the field is missing", () => {
    useFutureCompletePollsMock.mockReturnValue(createFuturePollData({
      selected: "venue-main",
      restaurant: "venue-restaurant",
      date: "2026-03-30",
    }));
    usePubsMock.mockReturnValue({
      "venue-main": { name: "The Maypole", venueType: "pub" },
      "venue-restaurant": { name: "Bistro 12", venueType: "restaurant" },
    });

    render(<CurrentEvents />);

    expect(screen.getByText(/Restaurant:\s*Bistro 12/)).toBeTruthy();
    expect(screen.queryByText(/18:30/)).toBeNull();
  });

  it("routes attendance actions independently for main venue and restaurant", async () => {
    useFutureCompletePollsMock.mockReturnValue(createFuturePollData({
      selected: "venue-main",
      restaurant: "venue-restaurant",
      date: "2026-03-30",
    }));
    usePubsMock.mockReturnValue({
      "venue-main": { name: "The Maypole", venueType: "pub" },
      "venue-restaurant": { name: "Bistro 12", venueType: "restaurant" },
    });

    render(<CurrentEvents />);

    const statusButtons = screen.getAllByText("set-status");
    fireEvent.click(statusButtons[0]);
    fireEvent.click(statusButtons[1]);

    await waitFor(() => {
      expect(setAttendanceStatusMock).toHaveBeenCalledWith("venue-main", "user-1", "canCome");
      expect(setAttendanceStatusMock).toHaveBeenCalledWith("venue-restaurant", "user-1", "canCome");
    });
  });

  it("does not render restaurant section when restaurant id is missing from pubs", () => {
    useFutureCompletePollsMock.mockReturnValue(createFuturePollData({
      selected: "venue-main",
      restaurant: "venue-missing",
      date: "2026-03-30",
    }));
    usePubsMock.mockReturnValue({
      "venue-main": { name: "The Maypole", venueType: "pub" },
    });

    render(<CurrentEvents />);

    expect(screen.queryByText(/Restaurant:/)).toBeNull();
    expect(screen.getAllByTestId("attendance-actions")).toHaveLength(1);
  });

  it("reschedules to any main venue and adds a restaurant", async () => {
    useFutureCompletePollsMock.mockReturnValue(createFuturePollData({
      selected: "venue-main",
      date: "2026-03-30",
    }));
    usePubsMock.mockReturnValue({
      "venue-main": { name: "The Maypole", venueType: "pub" },
      "venue-alt": { name: "The Anchor", venueType: "pub" },
      "venue-food": { name: "The Oak", venueType: "pub", food: true },
      "venue-restaurant": { name: "Bistro 12", venueType: "restaurant" },
      "venue-restaurant-2": { name: "Pizza Town", venueType: "restaurant" },
    });
    useRoleMock.mockImplementation((roleName) => roleName === "canCompletePoll");

    render(<CurrentEvents />);

    fireEvent.click(screen.getByText("Reschedule Event"));

    const selects = screen.getAllByRole("combobox");
    fireEvent.change(selects[0], { target: { value: "venue-alt" } });
    fireEvent.change(selects[1], { target: { value: "venue-restaurant-2" } });
    fireEvent.click(screen.getByText("Save Changes"));

    await waitFor(() => {
      expect(reschedulePollMock).toHaveBeenCalledWith(
        "poll-1",
        "venue-main",
        "venue-alt",
        "venue-restaurant-2",
        "18:30",
      );
    });
  });

  it("allows editing only the restaurant while keeping the main venue", async () => {
    useFutureCompletePollsMock.mockReturnValue(createFuturePollData({
      selected: "venue-main",
      restaurant: "venue-restaurant",
      restaurant_time: "18:30",
      date: "2026-03-30",
    }));
    usePubsMock.mockReturnValue({
      "venue-main": { name: "The Maypole", venueType: "pub" },
      "venue-alt": { name: "The Anchor", venueType: "pub" },
      "venue-restaurant": { name: "Bistro 12", venueType: "restaurant" },
      "venue-restaurant-2": { name: "Pizza Town", venueType: "restaurant" },
    });
    useRoleMock.mockImplementation((roleName) => roleName === "canCompletePoll");

    render(<CurrentEvents />);

    fireEvent.click(screen.getByText("Reschedule Event"));

    const selects = screen.getAllByRole("combobox");
    fireEvent.change(selects[1], { target: { value: "venue-restaurant-2" } });
    fireEvent.click(screen.getByText("Save Changes"));

    await waitFor(() => {
      expect(reschedulePollMock).toHaveBeenCalledWith(
        "poll-1",
        "venue-main",
        "venue-main",
        "venue-restaurant-2",
        "18:30",
      );
    });
  });
});

describe("PastEvents", () => {
  beforeEach(() => {
    usePastCompletePollsMock.mockReset();
    usePubsMock.mockReset();
  });

  afterEach(() => {
    cleanup();
  });

  function renderPastEvents(initialEntry = "/events/past") {
    return render(
      <MemoryRouter initialEntries={[initialEntry]}>
        <Routes>
          <Route path="/events/past" element={<PastEvents />} />
        </Routes>
      </MemoryRouter>
    );
  }

  it("requests page size changes as numbers", () => {
    usePastCompletePollsMock.mockReturnValue({
      pollData: createPastPollData({ selected: "venue-main", date: "2026-03-20" }),
      hasNextPage: false,
      lastVisibleId: null,
      isLoading: false,
    });
    usePubsMock.mockReturnValue({
      "venue-main": { name: "The Maypole" },
    });

    renderPastEvents("/events/past?pastPageSize=5&pastCursorTrail=poll-older");

    fireEvent.change(screen.getByRole("combobox"), { target: { value: "10" } });

    expect(usePastCompletePollsMock).toHaveBeenLastCalledWith(10, null);
  });

  it("uses URL cursor trail for older/newer navigation", () => {
    usePastCompletePollsMock.mockReturnValue({
      pollData: createPastPollData({ selected: "venue-main", date: "2026-03-20" }),
      hasNextPage: true,
      lastVisibleId: "poll-last-visible",
      isLoading: false,
    });
    usePubsMock.mockReturnValue({
      "venue-main": { name: "The Maypole" },
    });

    renderPastEvents();

    fireEvent.click(screen.getByText("Older Events"));
    expect(usePastCompletePollsMock).toHaveBeenLastCalledWith(5, "poll-last-visible");
    expect(screen.getByText("Page 2")).toBeTruthy();

    fireEvent.click(screen.getByText("Newer Events"));
    expect(usePastCompletePollsMock).toHaveBeenLastCalledWith(5, null);
    expect(screen.getByText("Page 1")).toBeTruthy();
  });
});
