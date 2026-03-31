import { describe, expect, it } from "vitest";
import {
  createCompletingPollState,
  getAllRestaurantVenues,
  getRestaurantIdForCompletion,
  getRestaurantOptionsForPoll,
  isRestaurantChoiceRequired,
} from "./venueSelection";

// Shared fixture: a set of system venues used across multiple tests
const systemVenues = {
  "venue-pub": { name: "The Maypole", venueType: "pub" },
  "venue-restaurant": { name: "Pizza Town", venueType: "restaurant" },
  "venue-restaurant-2": { name: "Bistro 12", venueType: "restaurant" },
  "venue-pub-food": { name: "The Oak", venueType: "pub", food: true },
};

describe("getRestaurantOptionsForPoll", () => {
  it("filters and sorts restaurant options from a poll, ignoring pubs and 'any'", () => {
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
});

describe("getAllRestaurantVenues", () => {
  it("returns all restaurant venues sorted by name", () => {
    expect(getAllRestaurantVenues(systemVenues)).toEqual([
      { id: "venue-restaurant-2", name: "Bistro 12" },
      { id: "venue-restaurant", name: "Pizza Town" },
    ]);
  });

  it("returns empty array when no restaurants exist", () => {
    expect(getAllRestaurantVenues({ "p1": { name: "A Pub", venueType: "pub" } })).toEqual([]);
  });
});

describe("createCompletingPollState — pub with food", () => {
  it("sets pubHasFood true and does not auto-select a restaurant even when one is on the poll", () => {
    const state = createCompletingPollState(
      "venue-pub-food",
      "The Oak",
      "poll-1",
      {
        pubs: {
          "venue-pub-food": { name: "The Oak" },
          "venue-restaurant": { name: "Pizza Town" },
        },
      },
      systemVenues,
    );

    expect(state.pubHasFood).toBe(true);
    expect(state.restaurantId).toBe("");
    expect(state.restaurantTime).toBe("");
    expect(getRestaurantIdForCompletion(state)).toBeUndefined();
  });
});

describe("createCompletingPollState — pub without food", () => {
  it("auto-selects the only restaurant on the poll and defaults the time", () => {
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
      systemVenues,
    );

    expect(state.pubHasFood).toBe(false);
    expect(state.restaurantOptions).toEqual([{ id: "venue-restaurant", name: "Pizza Town" }]);
    expect(state.restaurantId).toBe("venue-restaurant");
    expect(state.restaurantTime).toBe("18:30");
    expect(isRestaurantChoiceRequired(state)).toBe(false);
    expect(getRestaurantIdForCompletion(state)).toBe("venue-restaurant");
  });

  it("does not auto-select when multiple poll restaurants exist, and requires a choice", () => {
    const state = createCompletingPollState(
      "venue-pub",
      "The Maypole",
      "poll-1",
      {
        pubs: {
          "venue-pub": { name: "The Maypole" },
          "venue-restaurant": { name: "Pizza Town" },
          "venue-restaurant-2": { name: "Bistro 12" },
        },
      },
      systemVenues,
    );

    expect(state.pubHasFood).toBe(false);
    expect(state.restaurantId).toBe("");
    expect(state.restaurantTime).toBe("");
    expect(isRestaurantChoiceRequired(state)).toBe(true);
    expect(getRestaurantIdForCompletion(state)).toBeUndefined();
    expect(getRestaurantIdForCompletion({ ...state, restaurantId: "venue-restaurant-2" })).toBe("venue-restaurant-2");
  });

  it("provides allRestaurantVenues as fallback when no restaurants are on the poll", () => {
    const state = createCompletingPollState(
      "venue-pub",
      "The Maypole",
      "poll-1",
      {
        pubs: {
          "venue-pub": { name: "The Maypole" },
        },
      },
      systemVenues,
    );

    expect(state.pubHasFood).toBe(false);
    expect(state.restaurantOptions).toEqual([]);
    expect(state.restaurantId).toBe("");
    expect(state.restaurantTime).toBe("");
    expect(state.allRestaurantVenues).toEqual([
      { id: "venue-restaurant-2", name: "Bistro 12" },
      { id: "venue-restaurant", name: "Pizza Town" },
    ]);
    expect(getRestaurantIdForCompletion(state)).toBeUndefined();
  });
});
