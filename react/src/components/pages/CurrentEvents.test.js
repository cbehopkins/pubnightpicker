// @vitest-environment jsdom

import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import CurrentEvents from "./CurrentEvents";

const {
  useFutureCompletePollsMock,
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
    useFutureCompletePollsMock: vi.fn(),
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

vi.mock("../../hooks/usePolls", () => {
  return {
    useFutureCompletePolls: useFutureCompletePollsMock,
    usePastCompletePolls: vi.fn(),
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

describe("CurrentEvents", () => {
  beforeEach(() => {
    useFutureCompletePollsMock.mockReset();
    usePubsMock.mockReset();
    useVotesMock.mockReset();
    useAttendanceMock.mockReset();
    useRoleMock.mockReset();
    useSelectorMock.mockReset();
    setAttendanceStatusMock.mockReset();
    clearAttendanceMock.mockReset();
    runAttendanceActionMock.mockReset();

    runAttendanceActionMock.mockImplementation(async (fn) => fn());
    useSelectorMock.mockReturnValue("user-1");
    useVotesMock.mockReturnValue([{}]);
    useAttendanceMock.mockReturnValue([{}, setAttendanceStatusMock, clearAttendanceMock]);
    useRoleMock.mockReturnValue(false);
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
});
