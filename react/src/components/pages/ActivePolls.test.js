// @vitest-environment jsdom

import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import ActivePolls from "./ActivePolls";

const {
  completePollMock,
  useActiveMeetsMock,
  useVenuesMock,
  useRoleMock,
  activePollPropsMock,
} = vi.hoisted(() => {
  return {
    completePollMock: vi.fn(async () => undefined),
    useActiveMeetsMock: vi.fn(),
    useVenuesMock: vi.fn(),
    useRoleMock: vi.fn(),
    activePollPropsMock: vi.fn(),
  };
});

vi.mock("../../dbtools/polls", () => {
  return {
    completeMeetVoting: completePollMock,
  };
});

vi.mock("../../hooks/usePolls", () => {
  return {
    useActiveMeets: useActiveMeetsMock,
  };
});

vi.mock("../../hooks/useVenues", () => {
  return {
    default: useVenuesMock,
  };
});

vi.mock("../../hooks/useRole", () => {
  return {
    default: useRoleMock,
  };
});

vi.mock("../UI/NewPoll", () => {
  return {
    default: () => <div data-testid="new-poll" />,
  };
});

vi.mock("../UI/ActivePoll", () => {
  return {
    default: (props) => {
      activePollPropsMock(props);
      const { on_complete, poll_id, poll_data } = props;
      return (
        <>
          <span>{poll_data.date}</span>
          <button onClick={() => on_complete("selected-venue", "Selected Venue", poll_id)}>
            trigger-complete
          </button>
        </>
      );
    },
  };
});

vi.mock("./CompletePollModal", () => {
  return {
    default: ({ availableRestaurants, pubHasFood, onRestaurantChange, onRestaurantTimeChange, onConfirm, onCancel }) => {
      return (
        <div data-testid="complete-poll-modal">
          {!pubHasFood && availableRestaurants.length > 1 && (
            <button onClick={() => onRestaurantChange("r2")}>pick-r2</button>
          )}
          <button onClick={() => onRestaurantTimeChange("19:00")}>set-time-19-00</button>
          <button onClick={onConfirm}>confirm-complete</button>
          <button onClick={onCancel}>cancel-complete</button>
        </div>
      );
    },
  };
});

function createPollData(poll) {
  return {
    meets: {
      "poll-1": poll,
    },
    dates: new Set([poll.date || "2026-03-30"]),
    *sortedByDate() {
      yield ["poll-1", poll];
    },
  };
}

describe("ActivePolls", () => {
  beforeEach(() => {
    completePollMock.mockReset();
    completePollMock.mockResolvedValue(undefined);
    useActiveMeetsMock.mockReset();
    useVenuesMock.mockReset();
    useRoleMock.mockReset();
    activePollPropsMock.mockReset();
    useRoleMock.mockImplementation((roleName) => {
      if (roleName === "canCompletePoll") {
        return true;
      }
      return false;
    });
  });

  afterEach(() => {
    cleanup();
  });

  it("shows an overdue incomplete Meet in Active Polls", () => {
    useActiveMeetsMock.mockReturnValue(createPollData({ date: "2020-01-01", votingCompleted: false, venues: {} }));
    useVenuesMock.mockReturnValue({});

    render(<ActivePolls />);

    expect(screen.getByRole("heading", { name: "Active Polls" })).toBeTruthy();
    expect(screen.getByText("2020-01-01")).toBeTruthy();
  });

  it("passes the Meet and Venue models to each active poll", () => {
    const meet = { date: "2026-03-30", venues: { v1: { name: "Venue 1" } } };
    const venues = { v1: { name: "Venue 1", dogFriendly: true } };
    useActiveMeetsMock.mockReturnValue(createPollData(meet));
    useVenuesMock.mockReturnValue(venues);

    render(<ActivePolls />);

    expect(activePollPropsMock).toHaveBeenLastCalledWith(expect.objectContaining({
      poll_id: "poll-1",
      poll_data: meet,
      venues,
    }));
  });

  it("does not start completion without permission", () => {
    useRoleMock.mockReturnValue(false);
    useActiveMeetsMock.mockReturnValue(createPollData({ date: "2026-03-30", venues: {} }));
    useVenuesMock.mockReturnValue({});

    render(<ActivePolls />);

    fireEvent.click(screen.getByText("trigger-complete"));

    expect(screen.queryByTestId("complete-poll-modal")).toBeNull();
    expect(completePollMock).not.toHaveBeenCalled();
  });

  it("completes without restaurant when no restaurants exist on the poll", async () => {
    useActiveMeetsMock.mockReturnValue(createPollData({
      date: "2026-03-30",
      venues: {
        "selected-venue": { name: "The Maypole" },
      },
    }));
    useVenuesMock.mockReturnValue({
      "selected-venue": { name: "The Maypole", venueType: "pub" },
    });

    render(<ActivePolls />);

    fireEvent.click(screen.getByText("trigger-complete"));
    fireEvent.click(screen.getByText("confirm-complete"));

    await waitFor(() => {
      expect(completePollMock).toHaveBeenCalledWith("selected-venue", "poll-1", "2026-03-30", undefined, undefined);
    });
  });

  it("auto-selects the single restaurant and includes it in completion", async () => {
    useActiveMeetsMock.mockReturnValue(createPollData({
      date: "2026-03-30",
      venues: {
        "selected-venue": { name: "The Maypole" },
        r1: { name: "Bistro 12" },
      },
    }));
    useVenuesMock.mockReturnValue({
      "selected-venue": { name: "The Maypole", venueType: "pub" },
      r1: { name: "Bistro 12", venueType: "restaurant" },
    });

    render(<ActivePolls />);

    fireEvent.click(screen.getByText("trigger-complete"));
    fireEvent.click(screen.getByText("confirm-complete"));

    await waitFor(() => {
      expect(completePollMock).toHaveBeenCalledWith("selected-venue", "poll-1", "2026-03-30", "r1", "18:30");
    });
  });

  it("allows no restaurant or an explicit restaurant choice when multiple restaurants exist", async () => {
    useActiveMeetsMock.mockReturnValue(createPollData({
      date: "2026-03-30",
      venues: {
        "selected-venue": { name: "The Maypole" },
        r1: { name: "Bistro 12" },
        r2: { name: "Pasta House" },
      },
    }));
    useVenuesMock.mockReturnValue({
      "selected-venue": { name: "The Maypole", venueType: "pub" },
      r1: { name: "Bistro 12", venueType: "restaurant" },
      r2: { name: "Pasta House", venueType: "restaurant" },
    });

    render(<ActivePolls />);

    fireEvent.click(screen.getByText("trigger-complete"));
    fireEvent.click(screen.getByText("confirm-complete"));

    await waitFor(() => {
      expect(completePollMock).toHaveBeenCalledWith("selected-venue", "poll-1", "2026-03-30", undefined, undefined);
    });

    completePollMock.mockClear();

    fireEvent.click(screen.getByText("trigger-complete"));
    fireEvent.click(screen.getByText("pick-r2"));
    fireEvent.click(screen.getByText("confirm-complete"));

    await waitFor(() => {
      expect(completePollMock).toHaveBeenCalledWith("selected-venue", "poll-1", "2026-03-30", "r2", "18:30");
    });
  });

  it("persists a user-updated restaurant meetup time", async () => {
    useActiveMeetsMock.mockReturnValue(createPollData({
      date: "2026-03-30",
      venues: {
        "selected-venue": { name: "The Maypole" },
        r1: { name: "Bistro 12" },
      },
    }));
    useVenuesMock.mockReturnValue({
      "selected-venue": { name: "The Maypole", venueType: "pub" },
      r1: { name: "Bistro 12", venueType: "restaurant" },
    });

    render(<ActivePolls />);

    fireEvent.click(screen.getByText("trigger-complete"));
    fireEvent.click(screen.getByText("set-time-19-00"));
    fireEvent.click(screen.getByText("confirm-complete"));

    await waitFor(() => {
      expect(completePollMock).toHaveBeenCalledWith("selected-venue", "poll-1", "2026-03-30", "r1", "19:00");
    });
  });
});
