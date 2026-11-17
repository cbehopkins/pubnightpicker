// @vitest-environment jsdom

import { act, renderHook } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { useCompletePolls } from "./useCompletePolls";

vi.mock("../dbtools/polls", () => {
  return {
    completeMeetVoting: vi.fn(async () => undefined),
  };
});

vi.mock("../permissions", () => {
  return {
    getUserFacingErrorMessage: vi.fn(() => "Unable to complete this poll."),
  };
});

vi.mock("../utils/notify", () => {
  return {
    notifyError: vi.fn(),
  };
});

describe("useCompletePolls", () => {
  it("defaults restaurant time to 18:30 when a restaurant is selected later", () => {
    const pollData = {
      meets: {
        "poll-1": {
          venues: {
            "pub-1": { name: "The Maypole" },
          },
        },
      },
    };

    const venues = {
      "pub-1": { name: "The Maypole", venueType: "pub" },
      "restaurant-1": { name: "Pizza Town", venueType: "restaurant" },
    };

    const { result } = renderHook(() => useCompletePolls(pollData, venues, true));

    act(() => {
      result.current.completeHandler("pub-1", "The Maypole", "poll-1");
    });

    expect(result.current.restaurantTime).toBe("");

    act(() => {
      result.current.setRestaurantChoice("restaurant-1");
    });

    expect(result.current.chosenRestaurantId).toBe("restaurant-1");
    expect(result.current.restaurantTime).toBe("18:30");
  });

  it("clears restaurant time when no restaurant is selected", () => {
    const pollData = {
      meets: {
        "poll-1": {
          venues: {
            "pub-1": { name: "The Maypole" },
            "restaurant-1": { name: "Pizza Town" },
          },
        },
      },
    };

    const venues = {
      "pub-1": { name: "The Maypole", venueType: "pub" },
      "restaurant-1": { name: "Pizza Town", venueType: "restaurant" },
    };

    const { result } = renderHook(() => useCompletePolls(pollData, venues, true));

    act(() => {
      result.current.completeHandler("pub-1", "The Maypole", "poll-1");
    });

    expect(result.current.chosenRestaurantId).toBe("restaurant-1");
    expect(result.current.restaurantTime).toBe("18:30");

    act(() => {
      result.current.setRestaurantChoice("");
    });

    expect(result.current.chosenRestaurantId).toBe("");
    expect(result.current.restaurantTime).toBe("");
  });
});