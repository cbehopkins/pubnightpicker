// @ts-check
// @vitest-environment jsdom

import { renderHook } from "@testing-library/react";
import { describe, beforeEach, it, expect, vi } from "vitest";
import useAutopopulateAction from "./useAutopopulateAction";

const { addNewPubMock, notifyInfoMock, notifyErrorMock } = vi.hoisted(() => ({
    addNewPubMock: vi.fn(async () => undefined),
    notifyInfoMock: vi.fn(),
    notifyErrorMock: vi.fn(),
}));

vi.mock("../dbtools/polls", () => ({
    add_new_pub_to_poll: addNewPubMock,
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
            pollPubs: {},
            pubParameters: { v1: {}, v2: {}, v3: {} },
            mostVisited: [{ id: "v1" }],
            leastVisited: [{ id: "v2" }],
            randomVenues: [{ id: "v3" }],
        }));

        await result.current.handleAutopopulate();

        expect(addNewPubMock).toHaveBeenCalledTimes(3);
        expect(addNewPubMock).toHaveBeenCalledWith("v1", "poll-1", expect.any(Object));
        expect(addNewPubMock).toHaveBeenCalledWith("v2", "poll-1", expect.any(Object));
        expect(addNewPubMock).toHaveBeenCalledWith("v3", "poll-1", expect.any(Object));
        expect(notifyInfoMock).toHaveBeenCalledWith("Added 3 venues to poll");
    });

    it("handles overlapping categories without duplicates", async () => {
        const { result } = renderHook(() => useAutopopulateAction({
            pollId: "poll-1",
            pollPubs: {},
            pubParameters: { v1: {} },
            mostVisited: [{ id: "v1" }],
            leastVisited: [{ id: "v1" }],
            randomVenues: [{ id: "v1" }],
        }));

        await result.current.handleAutopopulate();

        expect(addNewPubMock).toHaveBeenCalledTimes(1);
        expect(addNewPubMock).toHaveBeenCalledWith("v1", "poll-1", expect.any(Object));
        expect(notifyInfoMock).toHaveBeenCalledWith("Added 1 venue to poll");
    });

    it("reports when no venues are available", async () => {
        const { result } = renderHook(() => useAutopopulateAction({
            pollId: "poll-1",
            pollPubs: {},
            pubParameters: {},
            mostVisited: [],
            leastVisited: [],
            randomVenues: [],
        }));

        await result.current.handleAutopopulate();

        expect(addNewPubMock).not.toHaveBeenCalled();
        expect(notifyInfoMock).toHaveBeenCalledWith("No viable venues were available to auto-add.");
    });
});
