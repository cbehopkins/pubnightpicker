import { beforeEach, describe, expect, it, vi } from "vitest";

const {
    setDocMock,
    docMock,
    serverTimestampMock,
    authState,
} = vi.hoisted(() => ({
    setDocMock: vi.fn(async () => undefined),
    docMock: vi.fn((_db, collectionName, id) => ({ collectionName, id })),
    serverTimestampMock: vi.fn(() => "SERVER_TIMESTAMP"),
    authState: { currentUser: { uid: "user-1" } },
}));

vi.mock("firebase/firestore", () => ({
    doc: docMock,
    setDoc: setDocMock,
    serverTimestamp: serverTimestampMock,
}));

vi.mock("../firebase", () => ({
    auth: authState,
    db: {},
}));

import {
    POLL_ACTION_ADD_VENUE,
    POLL_ACTION_AUDIT_COLLECTION,
    POLL_ACTION_COMPLETE,
    POLL_ACTION_CREATE,
    POLL_ACTION_DELETE_VENUE,
    logPollActionAudit,
} from "./pollActionAudit";

describe("pollActionAudit", () => {
    beforeEach(() => {
        setDocMock.mockClear();
        docMock.mockClear();
        serverTimestampMock.mockClear();
        authState.currentUser = { uid: "user-1" };
    });

    it("writes create audit records with required fields", async () => {
        await logPollActionAudit(POLL_ACTION_CREATE, {
            pollId: "poll-1",
            pollDate: "2026-05-15",
        });

        expect(docMock).toHaveBeenCalledTimes(1);
        expect(docMock.mock.calls[0][1]).toBe(POLL_ACTION_AUDIT_COLLECTION);
        expect(docMock.mock.calls[0][2]).toContain("poll-1_create_");

        expect(setDocMock).toHaveBeenCalledWith(
            expect.anything(),
            {
                pollId: "poll-1",
                actionType: POLL_ACTION_CREATE,
                actorUid: "user-1",
                at: "SERVER_TIMESTAMP",
                pollDate: "2026-05-15",
            }
        );
    });

    it("writes completion-specific fields for complete action", async () => {
        await logPollActionAudit(POLL_ACTION_COMPLETE, {
            pollId: "poll-1",
            pollDate: "2026-05-15",
            selectedVenueId: "venue-1",
            restaurantId: "rest-1",
            restaurantTime: "18:30",
        });

        expect(setDocMock).toHaveBeenCalledWith(
            expect.anything(),
            expect.objectContaining({
                actionType: POLL_ACTION_COMPLETE,
                selectedVenueId: "venue-1",
                restaurantId: "rest-1",
                restaurantTime: "18:30",
            })
        );
    });

    it("writes venue mutation actions with venue name", async () => {
        await logPollActionAudit(POLL_ACTION_ADD_VENUE, {
            pollId: "poll-1",
            pollDate: "2026-05-15",
            selectedVenueId: "venue-1",
            venueName: "The Anchor",
        });

        await logPollActionAudit(POLL_ACTION_DELETE_VENUE, {
            pollId: "poll-1",
            pollDate: "2026-05-15",
            selectedVenueId: "venue-1",
            venueName: "The Anchor",
        });

        expect(setDocMock).toHaveBeenNthCalledWith(
            1,
            expect.anything(),
            expect.objectContaining({
                actionType: POLL_ACTION_ADD_VENUE,
                selectedVenueId: "venue-1",
                venueName: "The Anchor",
            })
        );
        expect(setDocMock).toHaveBeenNthCalledWith(
            2,
            expect.anything(),
            expect.objectContaining({
                actionType: POLL_ACTION_DELETE_VENUE,
                selectedVenueId: "venue-1",
                venueName: "The Anchor",
            })
        );
    });

    it("rejects completion audit entries when selectedVenueId is missing", async () => {
        await expect(
            logPollActionAudit(POLL_ACTION_COMPLETE, {
                pollId: "poll-1",
                pollDate: "2026-05-15",
            })
        ).rejects.toThrow("selectedVenueId is required");
    });

    it("rejects when no authenticated user is available", async () => {
        authState.currentUser = null;
        await expect(
            logPollActionAudit(POLL_ACTION_CREATE, {
                pollId: "poll-1",
                pollDate: "2026-05-15",
            })
        ).rejects.toThrow("No authenticated user available");
    });

    it("rejects unsupported action types", async () => {
        await expect(
            logPollActionAudit("unknown", {
                pollId: "poll-1",
                pollDate: "2026-05-15",
            })
        ).rejects.toThrow("Unsupported poll action type");
    });
});
