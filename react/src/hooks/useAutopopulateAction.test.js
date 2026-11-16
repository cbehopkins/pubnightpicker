// @ts-check
// @vitest-environment jsdom

import { renderHook } from "@testing-library/react";
import { describe, beforeEach, it, expect, vi } from "vitest";
import useAutopopulateAction from "./useAutopopulateAction";

const { addVenueToMeetMock, notifyInfoMock, notifyErrorMock } = vi.hoisted(() => ({
    addVenueToMeetMock: vi.fn(async () => undefined),
    notifyInfoMock: vi.fn(),
    notifyErrorMock: vi.fn(),
}));

vi.mock("../dbtools/polls", () => ({
    addVenueToMeet: addVenueToMeetMock,
}));

vi.mock("../utils/notify", () => ({
    notifyInfo: notifyInfoMock,
    notifyError: notifyErrorMock,
}));

describe("useAutopopulateAction", () => {
    beforeEach(() => {
        vi.clearAllMocks();
    });

    it("adds one venue per category when available", async () => {
        const { result } = renderHook(() => useAutopopulateAction({
            pollId: "poll-1",
            pollDate: "2026-05-11",
            pollVenues: {},
            venueParameters: { v1: {}, v2: {}, v3: {} },
            mostVisited: [{ id: "v1" }],
            leastVisited: [{ id: "v2" }],
            randomVenues: [{ id: "v3" }],
        }));

        await result.current.handleAutopopulate();

        expect(addVenueToMeetMock).toHaveBeenCalledTimes(3);
        expect(addVenueToMeetMock).toHaveBeenCalledWith("v1", "poll-1", expect.any(Object), "2026-05-11");
        expect(addVenueToMeetMock).toHaveBeenCalledWith("v2", "poll-1", expect.any(Object), "2026-05-11");
        expect(addVenueToMeetMock).toHaveBeenCalledWith("v3", "poll-1", expect.any(Object), "2026-05-11");
        expect(notifyInfoMock).toHaveBeenCalledWith("Added 3 venues to poll");
    });

    it("handles overlapping categories without duplicates", async () => {
        const { result } = renderHook(() => useAutopopulateAction({
            pollId: "poll-1",
            pollDate: "2026-05-11",
            pollVenues: {},
            venueParameters: { v1: {} },
            mostVisited: [{ id: "v1" }],
            leastVisited: [{ id: "v1" }],
            randomVenues: [{ id: "v1" }],
        }));

        await result.current.handleAutopopulate();

        expect(addVenueToMeetMock).toHaveBeenCalledTimes(1);
        expect(addVenueToMeetMock).toHaveBeenCalledWith("v1", "poll-1", expect.any(Object), "2026-05-11");
        expect(notifyInfoMock).toHaveBeenCalledWith("Added 1 venue to poll");
    });

    it("backfills to three unique venues when overlap still leaves additional candidates", async () => {
        const { result } = renderHook(() => useAutopopulateAction({
            pollId: "poll-1",
            pollDate: "2026-05-11",
            pollVenues: {},
            venueParameters: { v1: {}, v2: {}, v3: {} },
            mostVisited: [{ id: "v1" }],
            leastVisited: [{ id: "v1" }],
            randomVenues: [{ id: "v1" }, { id: "v2" }, { id: "v3" }],
        }));

        await result.current.handleAutopopulate();

        expect(addVenueToMeetMock).toHaveBeenCalledTimes(3);
        const addedIds = addVenueToMeetMock.mock.calls.map((call) => call[0]);
        expect(new Set(addedIds).size).toBe(3);
        expect(addedIds).toEqual(expect.arrayContaining(["v1", "v2", "v3"]));
        expect(notifyInfoMock).toHaveBeenCalledWith("Added 3 venues to poll");
    });

    it("reports when no venues are available", async () => {
        const { result } = renderHook(() => useAutopopulateAction({
            pollId: "poll-1",
            pollDate: "2026-05-11",
            pollVenues: {},
            venueParameters: {},
            mostVisited: [],
            leastVisited: [],
            randomVenues: [],
        }));

        await result.current.handleAutopopulate();

        expect(addVenueToMeetMock).not.toHaveBeenCalled();
        expect(notifyInfoMock).toHaveBeenCalledWith("No viable venues were available to auto-add.");
    });
});
