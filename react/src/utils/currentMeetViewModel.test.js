import { describe, expect, it } from "vitest";
import { buildCurrentMeetViewModel, getDedupedVotesForVenue } from "./currentMeetViewModel";

describe("currentMeetViewModel", () => {
  it("returns null when the selected venue is missing", () => {
    const result = buildCurrentMeetViewModel({
      currentVenueId: "venue-1",
      restaurantId: undefined,
      venues: {},
      votes: {},
      attendance: {},
      currUserId: "user-1",
      showVoters: true,
    });

    expect(result).toBeNull();
  });

  it("builds the main venue view model with deduped votes and attendance state", () => {
    const result = buildCurrentMeetViewModel({
      currentVenueId: "venue-1",
      restaurantId: undefined,
      venues: {
        "venue-1": {
          name: "The Maypole",
          website: "https://example.com/maypole",
          address: "8 Portugal Place",
        },
      },
      votes: {
        "venue-1": { total: 2, voterUids: ["user-1", "user-2"] },
        any: { total: 2, voterUids: ["user-2", "user-3"] },
      },
      attendance: {
        "venue-1": {
          canCome: ["user-1"],
          cannotCome: ["user-4"],
        },
      },
      currUserId: "user-1",
      showVoters: true,
    });

    expect(result.mainVenue.name).toBe("The Maypole");
    expect(result.mainVenue.userCanCome).toBe(true);
    expect(result.mainVenue.userCannotCome).toBe(false);
    expect(result.mainVenue.dedupedVotes).toEqual(["user-1", "user-2", "user-3"]);
    expect(result.mainVenue.allowShowVoters).toBe(true);
    expect(result.restaurantVenue).toBeNull();
  });

  it("keeps attendance details visible when voter identities are restricted", () => {
    const result = buildCurrentMeetViewModel({
      currentVenueId: "venue-1",
      restaurantId: undefined,
      venues: { "venue-1": { name: "The Maypole" } },
      votes: { "venue-1": { total: 1, voterUids: ["voter-1"] } },
      attendance: { "venue-1": { canCome: ["attendee-1"], cannotCome: [] } },
      currUserId: "member-1",
      showVoters: false,
    });

    expect(result.mainVenue.allowShowAttendance).toBe(true);
    expect(result.mainVenue.allowShowVoters).toBe(false);
  });

  it("builds a separate restaurant view model when restaurant is present", () => {
    const result = buildCurrentMeetViewModel({
      currentVenueId: "venue-1",
      restaurantId: "venue-2",
      restaurantTime: "18:30",
      venues: {
        "venue-1": { name: "The Maypole" },
        "venue-2": { name: "Bistro 12", address: "12 Market Street" },
      },
      votes: {
        any: { total: 1, voterUids: ["user-1"] },
        "venue-2": { total: 1, voterUids: ["user-2"] },
      },
      attendance: {
        "venue-2": {
          canCome: ["user-2"],
          cannotCome: ["user-1"],
        },
      },
      currUserId: "user-1",
      showVoters: true,
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
      venueA: { total: 2, voterUids: ["user-1", "user-2"] },
      any: { total: 2, voterUids: ["user-2", "user-3"] },
    }, "venueA")).toEqual(["user-1", "user-2", "user-3"]);
  });

  it("applies global attendance sentinel to the selected venue", () => {
    const result = buildCurrentMeetViewModel({
      currentVenueId: "venue-1",
      restaurantId: undefined,
      venues: {
        "venue-1": {
          name: "The Maypole",
        },
      },
      votes: {},
      attendance: {
        any: {
          canCome: ["user-1"],
          cannotCome: [],
        },
      },
      currUserId: "user-1",
      showVoters: true,
    });

    expect(result.mainVenue.userCanCome).toBe(true);
    expect(result.mainVenue.userCannotCome).toBe(false);
    expect(result.mainVenue.canCome).toEqual(["user-1"]);
  });

  it("keeps standing attendance off restaurants and applies one explicit response to a shared Venue", () => {
    const separateVenues = buildCurrentMeetViewModel({
      currentVenueId: "pub-1",
      restaurantId: "restaurant-1",
      venues: {
        "pub-1": { name: "The Maypole", venueType: "pub" },
        "restaurant-1": { name: "Bistro 12", venueType: "restaurant" },
      },
      votes: {},
      attendance: {
        any: { canCome: ["user-1"], cannotCome: [] },
        "restaurant-1": { canCome: ["user-2"], cannotCome: [] },
      },
      currUserId: "user-1",
      showVoters: true,
    });

    expect(separateVenues.mainVenue.userCanCome).toBe(true);
    expect(separateVenues.restaurantVenue.userCanCome).toBe(false);
    expect(separateVenues.restaurantVenue.canCome).toEqual(["user-2"]);

    const sharedVenue = buildCurrentMeetViewModel({
      currentVenueId: "shared-venue",
      restaurantId: "shared-venue",
      venues: { "shared-venue": { name: "The Bistro", venueType: "restaurant" } },
      votes: {},
      attendance: {
        any: { canCome: [], cannotCome: ["user-1"] },
        "shared-venue": { canCome: ["user-1"], cannotCome: [] },
      },
      currUserId: "user-1",
      showVoters: true,
    });

    expect(sharedVenue.mainVenue.userCanCome).toBe(true);
    expect(sharedVenue.restaurantVenue.userCanCome).toBe(true);
  });
});
