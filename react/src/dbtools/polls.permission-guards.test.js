import { beforeEach, describe, expect, it, vi } from "vitest";

const { assertCurrentUserPermissionMock, requestLastOrdersMock } = vi.hoisted(() => ({
    assertCurrentUserPermissionMock: vi.fn(),
    requestLastOrdersMock: vi.fn(async () => undefined),
}));

vi.mock("../permissions", () => ({
    assertCurrentUserPermission: assertCurrentUserPermissionMock,
    PERMISSIONS: {
        canCreatePoll: "canCreatePoll",
        canCompletePoll: "canCompletePoll",
        canAddPubToPoll: "canAddPubToPoll",
    },
}));

vi.mock("../utils/lastOrdersApi", () => ({
    requestLastOrders: requestLastOrdersMock,
}));

import {
    addVenueToMeet,
    completeMeetVoting,
    deleteMeet,
    removeVenueFromMeet,
    rescheduleMeet,
} from "./polls";

describe("poll dbtools API adapters", () => {
    beforeEach(() => {
        vi.clearAllMocks();
    });

    it("deletes a Meet through the API with delete permission", async () => {
        await deleteMeet("poll-1");
        expect(assertCurrentUserPermissionMock).toHaveBeenCalledWith("canCreatePoll", "deleting a poll");
        expect(requestLastOrdersMock).toHaveBeenCalledWith("api/meet/poll-1", { method: "DELETE" });
    });

    it("completes and reschedules Meets through authoritative API endpoints", async () => {
        await completeMeetVoting("pub-2", "poll-1", "2026-05-17", "pub-restaurant-1", "18:30");
        await rescheduleMeet("poll-1", "pub-1", "pub-2", "restaurant-1", "18:30");

        expect(requestLastOrdersMock).toHaveBeenNthCalledWith(1, "api/meet/poll-1/complete", {
            method: "POST",
            body: { venue_id: "pub-2", restaurant_id: "pub-restaurant-1", restaurant_time: "18:30" },
        });
        expect(requestLastOrdersMock).toHaveBeenNthCalledWith(2, "api/meet/poll-1/reschedule", {
            method: "POST",
            body: { venue_id: "pub-2", restaurant_id: "restaurant-1", restaurant_time: "18:30" },
        });
    });

    it("clears a restaurant through an explicit null reschedule payload", async () => {
        await rescheduleMeet("poll-1", "pub-1", "pub-1", undefined, undefined);
        expect(requestLastOrdersMock).toHaveBeenCalledWith("api/meet/poll-1/reschedule", {
            method: "POST",
            body: { venue_id: "pub-1", restaurant_id: null },
        });
    });

    it("adds and removes candidates through separate API endpoints", async () => {
        await addVenueToMeet("pub-2", "poll-1");
        await removeVenueFromMeet("poll-1", "pub-2");

        expect(requestLastOrdersMock).toHaveBeenNthCalledWith(1, "api/meet/poll-1/candidates/pub-2", { method: "PUT" });
        expect(requestLastOrdersMock).toHaveBeenNthCalledWith(2, "api/meet/poll-1/candidates/pub-2", { method: "DELETE" });
    });

    it("does not assert permission or call the API for an empty candidate", async () => {
        await addVenueToMeet("", "poll-1");
        expect(assertCurrentUserPermissionMock).not.toHaveBeenCalled();
        expect(requestLastOrdersMock).not.toHaveBeenCalled();
    });
});
