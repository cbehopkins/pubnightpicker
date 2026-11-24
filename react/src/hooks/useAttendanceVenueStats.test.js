// @vitest-environment jsdom

import { renderHook, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import useAttendanceVenueStats, { aggregateVenueAttendance, getVenueIdsForMeet } from "./useAttendanceVenueStats";
import useWinningVenueStats from "./useWinningVenueStats";

const { whereMock, requestLastOrdersMock } = vi.hoisted(() => ({
  whereMock: vi.fn((...args) => args),
  requestLastOrdersMock: vi.fn(),
}));

vi.mock("../firebase", () => ({ db: {} }));
vi.mock("../utils/lastOrdersApi", () => ({
  requestLastOrders: requestLastOrdersMock,
}));
vi.mock("firebase/firestore", () => ({
  collection: vi.fn((...args) => args),
  query: vi.fn((...args) => args),
  where: whereMock,
  orderBy: vi.fn((...args) => args),
  doc: vi.fn((...args) => args),
  getDoc: vi.fn(),
  onSnapshot: vi.fn((query, onNext) => {
    onNext({
      forEach: (visit) => visit({
        id: "meet-1", data: () => ({
          completed: true,
          date: "2026-04-04",
          selected: "venue-a",
          restaurant: "venue-r",
          pubs: { "venue-a": {}, "venue-b": {} },
        })
      })
    });
    return () => { };
  }),
}));

describe("attendance statistics API boundary", () => {
  it("loads aggregate counts from Last Orders instead of direct attendance reads", async () => {
    requestLastOrdersMock.mockResolvedValue({
      counts_by_venue_id: { "venue-a": 1, "venue-b": 1, "venue-r": 0 },
      last_date_by_venue_id: { "venue-a": "2026-04-04", "venue-b": "2026-04-04", "venue-r": "2026-04-04" },
    });
    const { result } = renderHook(() => ({
      winning: useWinningVenueStats(1),
      attendance: useAttendanceVenueStats(1),
    }));

    await waitFor(() => expect(result.current.attendance.isLoading).toBe(false));
    expect(result.current.winning.meets["meet-1"]).toMatchObject({
      votingCompleted: true,
      selectedVenueId: "venue-a",
      restaurantId: "venue-r",
      venues: { "venue-a": {}, "venue-b": {} },
    });
    expect(result.current.attendance.countsByVenueId).toEqual({
      "venue-a": 1,
      "venue-b": 1,
      "venue-r": 0,
    });
    expect(result.current.attendance.lastDateByVenueId["venue-r"]).toBe("2026-04-04");
    expect(requestLastOrdersMock).toHaveBeenCalledWith("api/attendance/statistics", expect.objectContaining({
      method: "GET",
      query: expect.objectContaining({ start: expect.any(String), end: expect.any(String) }),
    }));
    expect(whereMock).toHaveBeenCalledWith("completed", "==", true);
    expect(whereMock).toHaveBeenCalledWith("date", ">=", result.current.winning.startDate);
    expect(whereMock).toHaveBeenCalledWith("date", "<=", result.current.winning.endDate);
  });
});

describe("useAttendanceVenueStats helpers", () => {
  it("includes Meet shortlist venues and ignores the global any key", () => {
    const venueIds = getVenueIdsForMeet(
      {
        selectedVenueId: "venue-a",
        restaurantId: "venue-r",
        venues: {
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
          venues: {
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
          venues: {
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

  it("does not count standing attendance for restaurant Venues", () => {
    const { countsByVenueId } = aggregateVenueAttendance(
      {
        poll1: {
          date: "2026-05-10",
          selectedVenueId: "venue-pub",
          restaurantId: "venue-restaurant",
          venues: { "venue-pub": {}, "venue-restaurant": {} },
        },
      },
      {
        poll1: {
          any: { canCome: ["u1"], cannotCome: [] },
        },
      },
      { "venue-pub": { venueType: "pub" }, "venue-restaurant": { venueType: "restaurant" } }
    );

    expect(countsByVenueId["venue-pub"]).toBe(1);
    expect(countsByVenueId["venue-restaurant"]).toBe(0);
  });
});
