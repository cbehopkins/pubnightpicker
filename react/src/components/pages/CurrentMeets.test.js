// @vitest-environment jsdom

import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import CurrentMeets, { PastMeets } from "./CurrentMeets";

const {
  reschedulePollMock,
  deletePollMock,
  useCurrentMeetsMock,
  usePastMeetsMock,
  usePubsMock,
  useVotesMock,
  useAttendanceMock,
  useRoleMock,
  useAdminMock,
  useSelectorMock,
  setAttendanceStatusMock,
  clearAttendanceMock,
  runAttendanceActionMock,
} = vi.hoisted(() => {
  return {
    reschedulePollMock: vi.fn(async () => undefined),
    deletePollMock: vi.fn(async () => undefined),
    useCurrentMeetsMock: vi.fn(),
    usePastMeetsMock: vi.fn(),
    usePubsMock: vi.fn(),
    useVotesMock: vi.fn(),
    useAttendanceMock: vi.fn(),
    useRoleMock: vi.fn(),
    useAdminMock: vi.fn(),
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
    useCurrentMeets: useCurrentMeetsMock,
    usePastMeets: usePastMeetsMock,
  };
});

vi.mock("../../hooks/useVenues", () => {
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

vi.mock("../../hooks/useAdmin", () => {
  return {
    default: useAdminMock,
  };
});

vi.mock("../../hooks/useUserPrivateData", () => ({ default: () => null }));

vi.mock("./EventChatModal", () => ({ default: () => null }));

vi.mock("../../firebase", () => {
  throw new Error("CurrentMeets unit tests must not initialise Firebase.");
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

describe("CurrentMeets", () => {
  beforeEach(() => {
    useCurrentMeetsMock.mockReset();
    usePastMeetsMock.mockReset();
    usePubsMock.mockReset();
    useVotesMock.mockReset();
    useAttendanceMock.mockReset();
    useRoleMock.mockReset();
    useAdminMock.mockReset();
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
    useAdminMock.mockReturnValue(false);
    usePastMeetsMock.mockReturnValue({
      meetData: createPastPollData({
        selectedVenueId: "venue-main",
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

  it("offers Meet Chat for a current Meet when chat is permitted", () => {
    useCurrentMeetsMock.mockReturnValue(createFuturePollData({
      selectedVenueId: "venue-main",
      date: "2026-03-30",
    }));
    usePubsMock.mockReturnValue({
      "venue-main": { name: "The Maypole", venueType: "pub" },
    });
    useRoleMock.mockImplementation((role) => role === "canChat");

    render(<CurrentMeets />);

    expect(screen.getByRole("button", { name: "Meet Chat" })).toBeTruthy();
  });

  it("shows only the main venue section when restaurant field is absent", () => {
    useCurrentMeetsMock.mockReturnValue(createFuturePollData({
      selectedVenueId: "venue-main",
      date: "2026-03-30",
    }));
    usePubsMock.mockReturnValue({
      "venue-main": { name: "The Maypole", venueType: "pub" },
    });

    render(<CurrentMeets />);

    expect(screen.getByRole("heading", { name: "Current Meets" })).toBeTruthy();
    expect(screen.getByText("The Maypole")).toBeTruthy();
    expect(screen.queryByText(/Restaurant:/)).toBeNull();
    expect(screen.getAllByTestId("attendance-actions")).toHaveLength(1);
  });

  it("displays a domain Venue's website and image on a current Meet", () => {
    useCurrentMeetsMock.mockReturnValue(createFuturePollData({
      selectedVenueId: "venue-main",
      date: "2026-03-30",
    }));
    usePubsMock.mockReturnValue({
      "venue-main": { name: "The Maypole", website: "example.com", image: "https://img.example.com/maypole.png" },
    });

    render(<CurrentMeets />);

    expect(screen.getByRole("link", { name: "The Maypole" }).getAttribute("href")).toBe("https://example.com");
    expect(screen.getByAltText("What the venue looks like").getAttribute("src")).toBe("https://img.example.com/maypole.png");
  });

  it("displays the selected Venue website and image on a current Meet", () => {
    useCurrentMeetsMock.mockReturnValue(createFuturePollData({
      selectedVenueId: "venue-main",
      date: "2026-03-30",
    }));
    usePubsMock.mockReturnValue({
      "venue-main": {
        name: "The Maypole",
        website: "https://example.com/maypole",
        image: "https://img.example.com/maypole.png",
      },
    });

    render(<CurrentMeets />);

    expect(screen.getByRole("link", { name: "The Maypole" }).getAttribute("href")).toBe("https://example.com/maypole");
    expect(screen.getByAltText("What the venue looks like").getAttribute("src")).toBe("https://img.example.com/maypole.png");
  });

  it("shows a separate restaurant section when restaurant field is present", () => {
    useCurrentMeetsMock.mockReturnValue(createFuturePollData({
      selectedVenueId: "venue-main",
      restaurantId: "venue-restaurant",
      restaurantTime: "18:30",
      date: "2026-03-30",
    }));
    usePubsMock.mockReturnValue({
      "venue-main": { name: "The Maypole", venueType: "pub" },
      "venue-restaurant": { name: "Bistro 12", venueType: "restaurant" },
    });

    render(<CurrentMeets />);

    expect(screen.getByText("The Maypole")).toBeTruthy();
    expect(screen.getByText(/Restaurant:\s*Bistro 12\s*\(18:30\)/)).toBeTruthy();
    expect(screen.getAllByTestId("attendance-actions")).toHaveLength(2);
  });

  it("hides restaurant time when the field is missing", () => {
    useCurrentMeetsMock.mockReturnValue(createFuturePollData({
      selectedVenueId: "venue-main",
      restaurantId: "venue-restaurant",
      date: "2026-03-30",
    }));
    usePubsMock.mockReturnValue({
      "venue-main": { name: "The Maypole", venueType: "pub" },
      "venue-restaurant": { name: "Bistro 12", venueType: "restaurant" },
    });

    render(<CurrentMeets />);

    expect(screen.getByText(/Restaurant:\s*Bistro 12/)).toBeTruthy();
    expect(screen.queryByText(/18:30/)).toBeNull();
  });

  it("shows stats links for admins on the past meets page", () => {
    useAdminMock.mockReturnValue(true);
    usePastMeetsMock.mockReturnValue({
      meetData: createPastPollData({
        selectedVenueId: "venue-main",
        date: "2026-03-20",
      }),
      hasNextPage: false,
      hasPreviousPage: false,
      goToNextPage: vi.fn(),
      goToPreviousPage: vi.fn(),
      pageIndex: 0,
      isLoading: false,
    });
    usePubsMock.mockReturnValue({
      "venue-main": { name: "The Maypole", venueType: "pub" },
    });

    render(
      <MemoryRouter initialEntries={["/past_meets"]}>
        <Routes>
          <Route path="/past_meets" element={<PastMeets />} />
        </Routes>
      </MemoryRouter>
    );

    expect(screen.getByRole("heading", { name: "Past Meets" })).toBeTruthy();
    expect(screen.getByRole("link", { name: /view winning venue stats/i })).toBeTruthy();
    expect(screen.getByRole("link", { name: /view attendance stats/i })).toBeTruthy();
  });

  it("routes attendance actions independently for main venue and restaurant", async () => {
    useCurrentMeetsMock.mockReturnValue(createFuturePollData({
      selectedVenueId: "venue-main",
      restaurantId: "venue-restaurant",
      date: "2026-03-30",
    }));
    usePubsMock.mockReturnValue({
      "venue-main": { name: "The Maypole", venueType: "pub" },
      "venue-restaurant": { name: "Bistro 12", venueType: "restaurant" },
    });

    render(<CurrentMeets />);

    const statusButtons = screen.getAllByText("set-status");
    fireEvent.click(statusButtons[0]);
    fireEvent.click(statusButtons[1]);

    await waitFor(() => {
      expect(setAttendanceStatusMock).toHaveBeenCalledWith("venue-main", "user-1", "canCome");
      expect(setAttendanceStatusMock).toHaveBeenCalledWith("venue-restaurant", "user-1", "canCome");
    });
  });

  it("does not render restaurant section when restaurant id is missing from pubs", () => {
    useCurrentMeetsMock.mockReturnValue(createFuturePollData({
      selectedVenueId: "venue-main",
      restaurantId: "venue-missing",
      date: "2026-03-30",
    }));
    usePubsMock.mockReturnValue({
      "venue-main": { name: "The Maypole", venueType: "pub" },
    });

    render(<CurrentMeets />);

    expect(screen.queryByText(/Restaurant:/)).toBeNull();
    expect(screen.getAllByTestId("attendance-actions")).toHaveLength(1);
  });

  it("reschedules to any main venue and adds a restaurant", async () => {
    useCurrentMeetsMock.mockReturnValue(createFuturePollData({
      selectedVenueId: "venue-main",
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

    render(<CurrentMeets />);

    fireEvent.click(screen.getByText("Reschedule Meet"));

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
    useCurrentMeetsMock.mockReturnValue(createFuturePollData({
      selectedVenueId: "venue-main",
      restaurantId: "venue-restaurant",
      restaurantTime: "18:30",
      date: "2026-03-30",
    }));
    usePubsMock.mockReturnValue({
      "venue-main": { name: "The Maypole", venueType: "pub" },
      "venue-alt": { name: "The Anchor", venueType: "pub" },
      "venue-restaurant": { name: "Bistro 12", venueType: "restaurant" },
      "venue-restaurant-2": { name: "Pizza Town", venueType: "restaurant" },
    });
    useRoleMock.mockImplementation((roleName) => roleName === "canCompletePoll");

    render(<CurrentMeets />);

    fireEvent.click(screen.getByText("Reschedule Meet"));

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

describe("PastMeets", () => {
  beforeEach(() => {
    usePastMeetsMock.mockReset();
    usePubsMock.mockReset();
  });

  afterEach(() => {
    cleanup();
  });

  function renderPastMeets(initialEntry = "/past_meets") {
    return render(
      <MemoryRouter initialEntries={[initialEntry]}>
        <Routes>
          <Route path="/past_meets" element={<PastMeets />} />
        </Routes>
      </MemoryRouter>
    );
  }

  it("requests page size changes as numbers", () => {
    usePastMeetsMock.mockReturnValue({
      meetData: createPastPollData({ selectedVenueId: "venue-main", date: "2026-03-20" }),
      hasNextPage: false,
      lastVisibleId: null,
      isLoading: false,
    });
    usePubsMock.mockReturnValue({
      "venue-main": { name: "The Maypole" },
    });

    renderPastMeets("/past_meets?pastPageSize=5&pastCursorTrail=poll-older");

    fireEvent.change(screen.getByRole("combobox"), { target: { value: "10" } });

    expect(usePastMeetsMock).toHaveBeenLastCalledWith(10, null);
  });

  it("uses URL cursor trail for older/newer navigation", () => {
    usePastMeetsMock.mockReturnValue({
      meetData: createPastPollData({ selectedVenueId: "venue-main", date: "2026-03-20" }),
      hasNextPage: true,
      lastVisibleId: "poll-last-visible",
      isLoading: false,
    });
    usePubsMock.mockReturnValue({
      "venue-main": { name: "The Maypole" },
    });

    renderPastMeets();

    fireEvent.click(screen.getByText("Older Meets"));
    expect(usePastMeetsMock).toHaveBeenLastCalledWith(5, "poll-last-visible");
    expect(screen.getByText("Page 2")).toBeTruthy();

    fireEvent.click(screen.getByText("Newer Meets"));
    expect(usePastMeetsMock).toHaveBeenLastCalledWith(5, null);
    expect(screen.getByText("Page 1")).toBeTruthy();
  });

  it("shows placeholder when pub image URL is invalid", () => {
    usePastMeetsMock.mockReturnValue({
      meetData: createPastPollData({ selectedVenueId: "venue-main", date: "2026-03-20" }),
      hasNextPage: false,
      lastVisibleId: null,
      isLoading: false,
    });
    usePubsMock.mockReturnValue({
      "venue-main": { name: "The Maypole", image: "not-a-valid-image-url" },
    });

    renderPastMeets();

    expect(screen.getByText("No image available")).toBeTruthy();
    expect(screen.queryByAltText("Photo of The Maypole")).toBeNull();
  });

  it("falls back to placeholder when image request fails", () => {
    usePastMeetsMock.mockReturnValue({
      meetData: createPastPollData({ selectedVenueId: "venue-main", date: "2026-03-20" }),
      hasNextPage: false,
      lastVisibleId: null,
      isLoading: false,
    });
    usePubsMock.mockReturnValue({
      "venue-main": { name: "The Maypole", image: "https://img.example.com/maypole.png" },
    });

    renderPastMeets();

    const image = screen.getByAltText("Photo of The Maypole");
    fireEvent.error(image);

    expect(screen.getByText("No image available")).toBeTruthy();
    expect(screen.queryByAltText("Photo of The Maypole")).toBeNull();
  });
});
