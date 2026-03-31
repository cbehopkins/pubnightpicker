import { describe, expect, it } from "vitest";
import {
  createCompletingPollState,
  getRestaurantIdForCompletion,
  getRestaurantOptionsForPoll,
  isRestaurantChoiceRequired,
} from "./venueSelection";

describe("venueSelection", () => {
  it("filters and sorts restaurant options from a poll", () => {
    const poll = {
      pubs: {
        venueA: { name: "Zulu Kitchen" },
        venueB: { name: "The Maypole" },
        venueC: { name: "Alto" },
        any: [],
      },
    };
    const venues = {
      venueA: { name: "Zulu Kitchen", venueType: "restaurant" },
      venueB: { name: "The Maypole", venueType: "pub" },
      venueC: { name: "Alto", venueType: "restaurant" },
    };

    expect(getRestaurantOptionsForPoll(poll, venues)).toEqual([
      { id: "venueC", name: "Alto" },
      { id: "venueA", name: "Zulu Kitchen" },
    ]);
  });

  it("auto-selects the only restaurant when building completion state", () => {
    const state = createCompletingPollState(
      "venue-pub",
      "The Maypole",
      "poll-1",
      {
        pubs: {
          "venue-pub": { name: "The Maypole" },
          "venue-restaurant": { name: "Pizza Town" },
        },
      },
      {
        "venue-pub": { name: "The Maypole", venueType: "pub" },
        "venue-restaurant": { name: "Pizza Town", venueType: "restaurant" },
      },
    );

    expect(state.restaurantOptions).toEqual([
      { id: "venue-restaurant", name: "Pizza Town" },
    ]);
    expect(state.restaurantId).toBe("venue-restaurant");
    expect(state.restaurantTime).toBe("18:30");
    expect(isRestaurantChoiceRequired(state)).toBe(false);
    expect(getRestaurantIdForCompletion(state)).toBe("venue-restaurant");
  });

  it("requires a choice only when multiple restaurants are present", () => {
    const state = createCompletingPollState(
      "venue-pub",
      "The Maypole",
      "poll-1",
      {
        pubs: {
          r1: { name: "Pizza Town" },
          r2: { name: "Bistro 12" },
        },
      },
      {
        r1: { name: "Pizza Town", venueType: "restaurant" },
        r2: { name: "Bistro 12", venueType: "restaurant" },
      },
    );

    expect(isRestaurantChoiceRequired(state)).toBe(true);
    expect(getRestaurantIdForCompletion(state)).toBeUndefined();
    expect(getRestaurantIdForCompletion({ ...state, restaurantId: "r2" })).toBe("r2");
  });
});
