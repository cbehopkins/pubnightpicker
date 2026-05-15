import { beforeEach, describe, expect, it, vi } from "vitest";

const {
    assertCurrentUserPermissionMock,
    deleteDocMock,
    updateDocMock,
    docMock,
    arrayUnionMock,
    deleteFieldMock,
    logPollActionAuditMock,
} = vi.hoisted(() => {
    return {
        assertCurrentUserPermissionMock: vi.fn(),
        deleteDocMock: vi.fn(async () => undefined),
        updateDocMock: vi.fn(async () => undefined),
        docMock: vi.fn(() => ({ id: "doc-ref" })),
        arrayUnionMock: vi.fn((value) => `arrayUnion:${value}`),
        deleteFieldMock: vi.fn(() => "deleteField"),
        logPollActionAuditMock: vi.fn(async () => undefined),
    };
});

vi.mock("./pollActionAudit", () => {
    return {
        POLL_ACTION_ADD_VENUE: "addVenue",
        POLL_ACTION_COMPLETE: "complete",
        POLL_ACTION_DELETE_VENUE: "deleteVenue",
        logPollActionAudit: logPollActionAuditMock,
    };
});

vi.mock("../permissions", () => {
    return {
        assertCurrentUserPermission: assertCurrentUserPermissionMock,
        PERMISSIONS: {
            canCreatePoll: "canCreatePoll",
            canCompletePoll: "canCompletePoll",
            canAddPubToPoll: "canAddPubToPoll",
        },
    };
});

vi.mock("../firebase", () => {
    return {
        db: {},
    };
});

vi.mock("firebase/firestore", () => {
    return {
        doc: docMock,
        updateDoc: updateDocMock,
        deleteDoc: deleteDocMock,
        arrayUnion: arrayUnionMock,
        deleteField: deleteFieldMock,
    };
});

import {
    add_new_pub_to_poll,
    complete_a_poll,
    deletePoll,
    deletePubFromPoll,
    reschedule_a_poll,
} from "./polls";

describe("poll dbtools permission guards", () => {
    beforeEach(() => {
        assertCurrentUserPermissionMock.mockReset();
        deleteDocMock.mockClear();
        updateDocMock.mockClear();
        docMock.mockClear();
        arrayUnionMock.mockClear();
        deleteFieldMock.mockClear();
        logPollActionAuditMock.mockClear();
    });

    it("guards deletePoll with canCreatePoll", async () => {
        await deletePoll("poll-1");
        expect(assertCurrentUserPermissionMock).toHaveBeenCalledWith(
            "canCreatePoll",
            "deleting a poll",
        );
        expect(deleteDocMock).toHaveBeenCalledTimes(4);
    });

    it("guards reschedule and complete with canCompletePoll", async () => {
        await reschedule_a_poll("poll-1", "pub-1", "pub-2", undefined, undefined);
        await complete_a_poll("pub-2", "poll-1", "2026-05-17", undefined, undefined);

        expect(assertCurrentUserPermissionMock).toHaveBeenCalledWith(
            "canCompletePoll",
            "rescheduling a poll",
        );
        expect(assertCurrentUserPermissionMock).toHaveBeenCalledWith(
            "canCompletePoll",
            "completing a poll",
        );
        expect(updateDocMock).toHaveBeenCalledTimes(2);
    });

    it("writes main venue and restaurant fields when rescheduling with a restaurant", async () => {
        await reschedule_a_poll("poll-1", "pub-1", "pub-2", "restaurant-1", "18:30");

        expect(updateDocMock).toHaveBeenCalledWith(
            { id: "doc-ref" },
            {
                previous_pubs: "arrayUnion:pub-1",
                selected: "pub-2",
                restaurant: "restaurant-1",
                restaurant_time: "18:30",
            },
        );
    });

    it("removes restaurant fields when rescheduling without a restaurant", async () => {
        await reschedule_a_poll("poll-1", "pub-1", "pub-1", undefined, undefined);

        expect(updateDocMock).toHaveBeenCalledWith(
            { id: "doc-ref" },
            {
                selected: "pub-1",
                restaurant: "deleteField",
                restaurant_time: "deleteField",
            },
        );
    });

    it("writes restaurant when poll completion includes one", async () => {
        await complete_a_poll(
            "pub-2",
            "poll-1",
            "2026-05-17",
            "pub-restaurant-1",
            "18:30"
        );

        expect(updateDocMock).toHaveBeenCalledWith(
            { id: "doc-ref" },
            {
                completed: true,
                selected: "pub-2",
                restaurant: "pub-restaurant-1",
                restaurant_time: "18:30",
            },
        );
        expect(logPollActionAuditMock).toHaveBeenCalledWith("complete", {
            pollId: "poll-1",
            pollDate: "2026-05-17",
            selectedVenueId: "pub-2",
            restaurantId: "pub-restaurant-1",
            restaurantTime: "18:30",
        });
    });

    it("does not write restaurant_time when no restaurant is stored", async () => {
        await complete_a_poll("pub-2", "poll-1", "2026-05-17", undefined, "18:30");

        expect(updateDocMock).toHaveBeenCalledWith(
            { id: "doc-ref" },
            {
                completed: true,
                selected: "pub-2",
            },
        );
    });

    it("guards poll pub add/delete with canAddPubToPoll", async () => {
        const pubParams = {
            "pub-2": { name: "The Anchor" },
        };

        await add_new_pub_to_poll("pub-2", "poll-1", pubParams, "2026-05-17");
        await deletePubFromPoll("poll-1", "pub-2", "2026-05-17", "The Anchor");

        expect(assertCurrentUserPermissionMock).toHaveBeenCalledWith(
            "canAddPubToPoll",
            "adding a pub to a poll",
        );
        expect(assertCurrentUserPermissionMock).toHaveBeenCalledWith(
            "canAddPubToPoll",
            "deleting a pub from a poll",
        );
        expect(logPollActionAuditMock).toHaveBeenCalledWith("addVenue", {
            pollId: "poll-1",
            pollDate: "2026-05-17",
            selectedVenueId: "pub-2",
            venueName: "The Anchor",
        });
        expect(logPollActionAuditMock).toHaveBeenCalledWith("deleteVenue", {
            pollId: "poll-1",
            pollDate: "2026-05-17",
            selectedVenueId: "pub-2",
            venueName: "The Anchor",
        });
    });

    it("does not assert permission when add_new_pub_to_poll is a no-op", async () => {
        await add_new_pub_to_poll("", "poll-1", {});
        expect(assertCurrentUserPermissionMock).not.toHaveBeenCalled();
        expect(updateDocMock).not.toHaveBeenCalled();
    });
});
