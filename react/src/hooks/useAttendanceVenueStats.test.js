// @vitest-environment jsdom

import { describe, expect, it } from "vitest";
import { aggregateVenueAttendance, getVenueIdsForPoll } from "./useAttendanceVenueStats";

describe("useAttendanceVenueStats helpers", () => {
  it("includes poll shortlist venues and ignores the global any key", () => {
    const venueIds = getVenueIdsForPoll(
      {
        selected: "venue-a",
        restaurant: "venue-r",
        pubs: {
          "venue-a": {},
          "venue-b": {},
          any: {},
        },
      },
      {
        any: { canCome: ["u1"] },
        "venue-extra": { canCome: ["u2"] },
      }
    );

    expect(new Set(venueIds)).toEqual(new Set(["venue-a", "venue-b", "venue-r", "venue-extra"]));
  });

  it("counts global any attendance toward each venue in a poll shortlist", () => {
    const { countsByVenueId } = aggregateVenueAttendance(
      {
        poll1: {
          date: "2026-05-10",
          pubs: {
            "venue-a": {},
            "venue-b": {},
            any: {},
          },
        },
      },
      {
        poll1: {
          any: {
            canCome: ["u1", "u2"],
            cannotCome: [],
          },
        },
      }
    );

    expect(countsByVenueId["venue-a"]).toBe(2);
    expect(countsByVenueId["venue-b"]).toBe(2);
  });

  it("applies local venue overrides on top of global any attendance", () => {
    const { countsByVenueId } = aggregateVenueAttendance(
      {
        poll1: {
          date: "2026-05-10",
          pubs: {
            "venue-a": {},
            "venue-b": {},
            any: {},
          },
        },
      },
      {
        poll1: {
          any: {
            canCome: ["u1", "u2"],
            cannotCome: [],
          },
          "venue-a": {
            cannotCome: ["u2"],
          },
        },
      }
    );

    expect(countsByVenueId["venue-a"]).toBe(1);
    expect(countsByVenueId["venue-b"]).toBe(2);
  });
});
