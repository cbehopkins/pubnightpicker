import { describe, expect, it } from "vitest";
import { buildCurrentEventViewModel, getDedupedVotesForVenue } from "./currentEventViewModel";

describe("currentEventViewModel", () => {
  it("returns null when the selected venue is missing", () => {
    const result = buildCurrentEventViewModel({
      current_pub_id: "venue-1",
      restaurant_id: undefined,
      pub_parameters: {},
      votes: {},
      attendance: {},
      currUserId: "user-1",
      show_voters: true,
    });

    expect(result).toBeNull();
  });

  it("builds the main venue view model with deduped votes and attendance state", () => {
    const result = buildCurrentEventViewModel({
      current_pub_id: "venue-1",
      restaurant_id: undefined,
      pub_parameters: {
        "venue-1": {
          name: "The Maypole",
          web_site: "https://example.com/maypole",
          address: "8 Portugal Place",
        },
      },
      votes: {
        "venue-1": ["user-1", "user-2"],
        any: ["user-2", "user-3"],
      },
      attendance: {
        "venue-1": {
          canCome: ["user-1"],
          cannotCome: ["user-4"],
        },
      },
      currUserId: "user-1",
      show_voters: true,
    });

    expect(result.mainVenue.name).toBe("The Maypole");
    expect(result.mainVenue.userCanCome).toBe(true);
    expect(result.mainVenue.userCannotCome).toBe(false);
    expect(result.mainVenue.dedupedVotes).toEqual(["user-1", "user-2", "user-3"]);
    expect(result.mainVenue.allowShowVoters).toBe(true);
    expect(result.restaurantVenue).toBeNull();
  });

  it("builds a separate restaurant view model when restaurant is present", () => {
    const result = buildCurrentEventViewModel({
      current_pub_id: "venue-1",
      restaurant_id: "venue-2",
      restaurant_time: "18:30",
      pub_parameters: {
        "venue-1": { name: "The Maypole" },
        "venue-2": { name: "Bistro 12", address: "12 Market Street" },
      },
      votes: {
        any: ["user-1"],
        "venue-2": ["user-2"],
      },
      attendance: {
        "venue-2": {
          canCome: ["user-2"],
          cannotCome: ["user-1"],
        },
      },
      currUserId: "user-1",
      show_voters: true,
    });

    expect(result.restaurantVenue).toEqual(expect.objectContaining({
      id: "venue-2",
      name: "Bistro 12",
      restaurantTime: "18:30",
      address: "12 Market Street",
      userCanCome: false,
      userCannotCome: true,
      dedupedVotes: ["user-2", "user-1"],
      allowShowVoters: true,
    }));
  });

  it("dedupes venue-specific votes with the any bucket", () => {
    expect(getDedupedVotesForVenue({
      venueA: ["user-1", "user-2"],
      any: ["user-2", "user-3"],
    }, "venueA")).toEqual(["user-1", "user-2", "user-3"]);
  });
});
