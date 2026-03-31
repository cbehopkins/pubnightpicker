// @vitest-environment jsdom

import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import ActivePolls from "./ActivePolls";

const {
  completePollMock,
  usePollsMock,
  usePubsMock,
  useRoleMock,
} = vi.hoisted(() => {
  return {
    completePollMock: vi.fn(async () => undefined),
    usePollsMock: vi.fn(),
    usePubsMock: vi.fn(),
    useRoleMock: vi.fn(),
  };
});

vi.mock("../../dbtools/polls", () => {
  return {
    complete_a_poll: completePollMock,
  };
});

vi.mock("../../hooks/usePolls", () => {
  return {
    default: usePollsMock,
  };
});

vi.mock("../../hooks/usePubs", () => {
  return {
    default: usePubsMock,
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
    default: ({ on_complete, poll_id }) => {
      return (
        <button onClick={() => on_complete("selected-venue", "Selected Venue", poll_id)}>
          trigger-complete
        </button>
      );
    },
  };
});

vi.mock("./CompletePollModal", () => {
  return {
    default: ({ restaurantChoiceRequired, onRestaurantChange, onRestaurantTimeChange, onConfirm, onCancel }) => {
      return (
        <div data-testid="complete-poll-modal">
          {restaurantChoiceRequired && (
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
    polls: {
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
    usePollsMock.mockReset();
    usePubsMock.mockReset();
    useRoleMock.mockReset();
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

  it("completes without restaurant when no restaurants exist on the poll", async () => {
    usePollsMock.mockReturnValue(createPollData({
      date: "2026-03-30",
      pubs: {
        "selected-venue": { name: "The Maypole" },
      },
    }));
    usePubsMock.mockReturnValue({
      "selected-venue": { name: "The Maypole", venueType: "pub" },
    });

    render(<ActivePolls />);

    fireEvent.click(screen.getByText("trigger-complete"));
    fireEvent.click(screen.getByText("confirm-complete"));

    await waitFor(() => {
      expect(completePollMock).toHaveBeenCalledWith("selected-venue", "poll-1", undefined, undefined);
    });
  });

  it("auto-selects the single restaurant and includes it in completion", async () => {
    usePollsMock.mockReturnValue(createPollData({
      date: "2026-03-30",
      pubs: {
        "selected-venue": { name: "The Maypole" },
        r1: { name: "Bistro 12" },
      },
    }));
    usePubsMock.mockReturnValue({
      "selected-venue": { name: "The Maypole", venueType: "pub" },
      r1: { name: "Bistro 12", venueType: "restaurant" },
    });

    render(<ActivePolls />);

    fireEvent.click(screen.getByText("trigger-complete"));
    fireEvent.click(screen.getByText("confirm-complete"));

    await waitFor(() => {
      expect(completePollMock).toHaveBeenCalledWith("selected-venue", "poll-1", "r1", "18:30");
    });
  });

  it("requires explicit choice when multiple restaurants exist", async () => {
    usePollsMock.mockReturnValue(createPollData({
      date: "2026-03-30",
      pubs: {
        "selected-venue": { name: "The Maypole" },
        r1: { name: "Bistro 12" },
        r2: { name: "Pasta House" },
      },
    }));
    usePubsMock.mockReturnValue({
      "selected-venue": { name: "The Maypole", venueType: "pub" },
      r1: { name: "Bistro 12", venueType: "restaurant" },
      r2: { name: "Pasta House", venueType: "restaurant" },
    });

    render(<ActivePolls />);

    fireEvent.click(screen.getByText("trigger-complete"));
    fireEvent.click(screen.getByText("confirm-complete"));

    expect(completePollMock).not.toHaveBeenCalled();

    fireEvent.click(screen.getByText("pick-r2"));
    fireEvent.click(screen.getByText("confirm-complete"));

    await waitFor(() => {
      expect(completePollMock).toHaveBeenCalledWith("selected-venue", "poll-1", "r2", "18:30");
    });
  });

  it("persists a user-updated restaurant meetup time", async () => {
    usePollsMock.mockReturnValue(createPollData({
      date: "2026-03-30",
      pubs: {
        "selected-venue": { name: "The Maypole" },
        r1: { name: "Bistro 12" },
      },
    }));
    usePubsMock.mockReturnValue({
      "selected-venue": { name: "The Maypole", venueType: "pub" },
      r1: { name: "Bistro 12", venueType: "restaurant" },
    });

    render(<ActivePolls />);

    fireEvent.click(screen.getByText("trigger-complete"));
    fireEvent.click(screen.getByText("set-time-19-00"));
    fireEvent.click(screen.getByText("confirm-complete"));

    await waitFor(() => {
      expect(completePollMock).toHaveBeenCalledWith("selected-venue", "poll-1", "r1", "19:00");
    });
  });
});
