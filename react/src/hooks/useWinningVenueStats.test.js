// @vitest-environment jsdom

import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import useWinningVenueStats from "./useWinningVenueStats";

const { requestMock, state } = vi.hoisted(() => ({
    requestMock: vi.fn(), state: { groupId: "group-a" },
}));
vi.mock("react-redux", () => ({ useSelector: (select) => select({ group: { groupId: state.groupId } }) }));
vi.mock("../utils/lastOrdersApi", () => ({ requestLastOrders: requestMock }));

describe("useWinningVenueStats", () => {
    beforeEach(() => { vi.clearAllMocks(); state.groupId = "group-a"; });

    it("requests fresh Group aggregates for the selected year window", async () => {
        requestMock.mockResolvedValue({
            group_id: "group-a", start_date: "2025-01-01", end_date: "2026-01-01",
            counts_by_venue_id: { "known-venue": 2, "unknown-venue": 1 },
            last_date_by_venue_id: { "known-venue": "2025-12-01", "unknown-venue": "2025-11-01" },
        });
        const { result } = renderHook(() => useWinningVenueStats(1));
        await waitFor(() => expect(result.current.isLoading).toBe(false));
        expect(requestMock).toHaveBeenCalledWith("api/groups/group-a/winning-statistics", expect.objectContaining({
            method: "GET", allowAnonymous: true, query: { years: "1" },
        }));
        expect(Object.values(result.current.meets)).toHaveLength(3);
        expect(result.current.meets["unknown-venue-0"].selectedVenueId).toBe("unknown-venue");
        expect(result.current.errorMessage).toBeNull();
    });

    it("clears old Group results while loading the newly selected Group", async () => {
        let resolve;
        requestMock.mockImplementation(() => new Promise((done) => { resolve = done; }));
        const { result, rerender } = renderHook(() => useWinningVenueStats(1));
        await waitFor(() => expect(requestMock).toHaveBeenCalledTimes(1));
        state.groupId = "group-b";
        rerender();
        expect(result.current.meets).toEqual({});
        expect(requestMock).toHaveBeenCalledTimes(2);
        await act(async () => resolve({ group_id: "group-b", start_date: "2025-01-01", end_date: "2026-01-01", counts_by_venue_id: {}, last_date_by_venue_id: {} }));
        await waitFor(() => expect(result.current.isEmpty).toBe(true));
    });

    it("shows source failures instead of an empty report", async () => {
        requestMock.mockRejectedValue(new Error("offline"));
        const { result } = renderHook(() => useWinningVenueStats(2));
        await waitFor(() => expect(result.current.isLoading).toBe(false));
        expect(result.current.errorMessage).toMatch(/Unable to load/);
        expect(result.current.isEmpty).toBe(false);
    });
});
