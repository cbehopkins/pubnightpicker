// @vitest-environment jsdom

import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const {
    collectionMock,
    docMock,
    getDocMock,
    limitMock,
    onSnapshotMock,
    orderByMock,
    queryMock,
    whereMock,
    timestampFromDateMock,
} = vi.hoisted(() => ({
    collectionMock: vi.fn(() => "collection-ref"),
    docMock: vi.fn((_db, collectionName, id) => `${collectionName}/${id}`),
    getDocMock: vi.fn(),
    limitMock: vi.fn((n) => ({ type: "limit", n })),
    onSnapshotMock: vi.fn(),
    orderByMock: vi.fn((field, direction) => ({ type: "orderBy", field, direction })),
    queryMock: vi.fn(() => "query-ref"),
    whereMock: vi.fn((field, op, value) => ({ type: "where", field, op, value })),
    timestampFromDateMock: vi.fn((date) => ({ fromDate: date })),
}));

vi.mock("firebase/firestore", () => ({
    Timestamp: {
        fromDate: timestampFromDateMock,
    },
    collection: collectionMock,
    doc: docMock,
    getDoc: getDocMock,
    limit: limitMock,
    onSnapshot: onSnapshotMock,
    orderBy: orderByMock,
    query: queryMock,
    where: whereMock,
}));

vi.mock("../../firebase", () => ({
    db: {},
}));

import { PollActionAuditPanel } from "./DiagnosticsPage";

describe("PollActionAuditPanel", () => {
    beforeEach(() => {
        vi.clearAllMocks();
        getDocMock.mockImplementation(async (path) => {
            if (path === "user-public/user-a") {
                return {
                    exists: () => true,
                    data: () => ({ name: "Alex" }),
                };
            }
            return {
                exists: () => false,
                data: () => ({}),
            };
        });
        onSnapshotMock.mockImplementation((_q, onNext) => {
            onNext({
                docs: [
                    {
                        id: "a1",
                        data: () => ({
                            at: { toDate: () => new Date("2026-05-15T12:00:00.000Z") },
                            actionType: "create",
                            pollId: "poll-1",
                            pollDate: "2026-05-16",
                            actorUid: "user-a",
                        }),
                    },
                    {
                        id: "a2",
                        data: () => ({
                            at: { toDate: () => new Date("2026-05-15T12:01:00.000Z") },
                            actionType: "addVenue",
                            pollId: "poll-1",
                            pollDate: "2026-05-16",
                            actorUid: "user-a",
                            selectedVenueId: "venue-1",
                            venueName: "The Anchor",
                        }),
                    },
                    {
                        id: "a3",
                        data: () => ({
                            at: { toDate: () => new Date("2026-05-15T12:02:00.000Z") },
                            actionType: "complete",
                            pollId: "poll-auto",
                            pollDate: "2026-05-16",
                            actorUid: "backend:auto",
                            selectedVenueId: "event-1",
                        }),
                    },
                ],
            });
            return () => undefined;
        });
    });

    afterEach(() => {
        cleanup();
    });

    it("renders audit rows and filters by selected action", async () => {
        render(<PollActionAuditPanel />);

        expect(screen.getByText("Poll Action Audit")).toBeTruthy();
        expect(screen.getByText("create")).toBeTruthy();
        expect(screen.getByText("addVenue")).toBeTruthy();
        expect(screen.getByText("complete")).toBeTruthy();
        expect(screen.getByText(/venue=venue-1 \(The Anchor\)/)).toBeTruthy();
        expect(screen.getByText("Backend (automatic)")).toBeTruthy();
        expect(screen.getByRole("option", { name: "Last 10 entries" })).toBeTruthy();
        expect(screen.getByRole("option", { name: "Last 50 entries" })).toBeTruthy();
        expect(screen.getByRole("option", { name: "Last 100 entries" })).toBeTruthy();

        await waitFor(() => {
            expect(screen.getAllByText("Alex").length).toBeGreaterThan(0);
        });
        expect(getDocMock).toHaveBeenCalledTimes(1);
        expect(getDocMock).toHaveBeenCalledWith("user-public/user-a");
        expect(screen.getAllByText("user-a").length).toBeGreaterThan(0);

        fireEvent.change(screen.getByLabelText("Action"), { target: { value: "addVenue" } });

        expect(screen.queryByText("create")).toBeNull();
        expect(screen.getByText("addVenue")).toBeTruthy();
        expect(screen.getByText("Showing venue add events")).toBeTruthy();
    });
});
