// @vitest-environment jsdom

import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
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

const {
    usePollsMock,
    useAutopopulateVenueSelectorMock,
} = vi.hoisted(() => ({
    usePollsMock: vi.fn(),
    useAutopopulateVenueSelectorMock: vi.fn(),
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

vi.mock("react-redux", () => ({
    useSelector: (select) => select({ auth: { uid: null } }),
}));

vi.mock("../ProtectedRoute", () => ({
    default: (Component) => Component,
}));

vi.mock("../UI/BackendPingPanel", () => ({
    default: () => <section aria-label="Last Orders API">Last Orders API</section>,
}));

vi.mock("../../hooks/usePolls", () => ({
    default: usePollsMock,
}));

vi.mock("../../hooks/useAutopopulateVenueSelector", () => ({
    default: useAutopopulateVenueSelectorMock,
}));

const { watchSilenceMock, setSilenceMock } = vi.hoisted(() => ({ watchSilenceMock: vi.fn(() => () => { }), setSilenceMock: vi.fn() }));

vi.mock("../../dbtools/diagnosticsConfig", () => ({
    watchSilenceNotifications: watchSilenceMock,
    setSilenceNotifications: setSilenceMock,
}));

import DiagnosticsPage, { AutopopulateCandidateListsPanel, EmailSuppressionPanel, PollActionAuditPanel } from "./DiagnosticsPage";

describe("EmailSuppressionPanel", () => {
    beforeEach(() => {
        watchSilenceMock.mockReset();
        watchSilenceMock.mockImplementation((onValue) => { onValue(false); return () => { }; });
        setSilenceMock.mockReset();
        setSilenceMock.mockResolvedValue(undefined);
    });
    afterEach(() => cleanup());

    it("shows loading and cleans up its subscription", () => {
        const unsubscribe = vi.fn();
        watchSilenceMock.mockImplementation(() => unsubscribe);
        const { unmount } = render(<EmailSuppressionPanel />);
        expect(screen.getByRole("checkbox")).toBeDisabled();
        expect(screen.getByRole("status")).toHaveTextContent("Loading");
        unmount();
        expect(unsubscribe).toHaveBeenCalledOnce();
    });

    it("writes both directions and follows subscription changes", async () => {
        let deliver;
        watchSilenceMock.mockImplementation((onValue) => { deliver = onValue; onValue(false); return () => { }; });
        render(<EmailSuppressionPanel />);
        fireEvent.click(screen.getByRole("checkbox"));
        await waitFor(() => expect(setSilenceMock).toHaveBeenCalledWith(true));
        await waitFor(() => expect(screen.getByRole("checkbox")).not.toBeDisabled());
        act(() => deliver(true));
        expect(screen.getByRole("checkbox")).toBeChecked();
        expect(screen.getByRole("status")).toHaveTextContent("email and push notifications silenced, including test notifications");
        expect(screen.getByRole("checkbox", { name: "Silence notifications" })).toBeChecked();
        expect(screen.getByText("Test notification acknowledgements confirm handling, not live delivery.")).toBeTruthy();
        fireEvent.click(screen.getByRole("checkbox"));
        await waitFor(() => expect(setSilenceMock).toHaveBeenLastCalledWith(false));
    });

    it("disables the control while saving", async () => {
        let finish;
        setSilenceMock.mockImplementation(() => new Promise((resolve) => { finish = resolve; }));
        render(<EmailSuppressionPanel />);
        fireEvent.click(screen.getByRole("checkbox"));
        expect(screen.getByRole("checkbox")).toBeDisabled();
        expect(screen.getByRole("status")).toHaveTextContent("Saving");
        finish();
        await waitFor(() => expect(screen.getByRole("checkbox")).not.toBeDisabled());
    });

    it("reports failed writes without changing the database-backed checkbox", async () => {
        setSilenceMock.mockRejectedValue(new Error("Write denied"));
        render(<EmailSuppressionPanel />);
        fireEvent.click(screen.getByRole("checkbox"));
        await waitFor(() => expect(screen.getByRole("alert")).toHaveTextContent("Write denied"));
        expect(screen.getByRole("checkbox")).not.toBeChecked();
    });

    it("does not present a read error as notifications enabled", () => {
        watchSilenceMock.mockImplementation((_onValue, onError) => { onError(new Error("Read denied")); return () => { }; });
        render(<EmailSuppressionPanel />);
        expect(screen.getByRole("checkbox")).toBeDisabled();
        expect(screen.getByRole("status")).toHaveTextContent("unavailable");
        expect(screen.getByRole("alert")).toHaveTextContent("Read denied");
    });
});

it("includes the direct Last Orders API panel on diagnostics", () => {
    onSnapshotMock.mockImplementation(() => () => { });
    render(<DiagnosticsPage />);
    expect(screen.getByRole("region", { name: "Last Orders API" })).toBeInTheDocument();
    cleanup();
});

describe("AutopopulateCandidateListsPanel", () => {
    beforeEach(() => {
        usePollsMock.mockReturnValue({
            sortedByDate: () => [
                ["poll-1", { date: "2026-06-10", pubs: { "venue-1": true } }],
            ],
        });

        useAutopopulateVenueSelectorMock.mockImplementation((pollId) => {
            if (pollId !== "poll-1") {
                return {
                    mostVisited: [],
                    leastVisited: [],
                    random: [],
                    isLoading: false,
                    error: null,
                };
            }

            return {
                mostVisited: [
                    {
                        id: "venue-2",
                        label: "The Oak",
                        count: 4,
                        lastWonDate: "2026-05-01",
                    },
                ],
                leastVisited: [
                    {
                        id: "venue-3",
                        label: "The Crown",
                        count: 1,
                        lastWonDate: "2026-02-14",
                    },
                ],
                random: [
                    {
                        id: "venue-4",
                        label: "The Bell",
                        count: 2,
                        lastWonDate: null,
                    },
                ],
                isLoading: false,
                error: null,
            };
        });
    });

    afterEach(() => {
        cleanup();
    });

    it("loads on demand and can be hidden again", () => {
        render(<AutopopulateCandidateListsPanel />);

        expect(screen.getByText("Autopopulate Candidate Lists")).toBeTruthy();
        expect(screen.getByRole("button", { name: "Load Candidate Lists" })).toBeTruthy();
        expect(usePollsMock).not.toHaveBeenCalled();
        expect(useAutopopulateVenueSelectorMock).not.toHaveBeenCalled();

        fireEvent.click(screen.getByRole("button", { name: "Load Candidate Lists" }));

        expect(screen.getByText("Most visited")).toBeTruthy();
        expect(screen.getByText("Least visited")).toBeTruthy();
        expect(screen.getByText("Random viable")).toBeTruthy();
        expect(screen.getByText(/The Oak/)).toBeTruthy();
        expect(screen.getByText(/The Crown/)).toBeTruthy();
        expect(screen.getByText(/The Bell/)).toBeTruthy();

        fireEvent.click(screen.getByRole("button", { name: "Hide Candidate Lists" }));

        expect(screen.queryByText("Most visited")).toBeNull();
        expect(screen.queryByText("Least visited")).toBeNull();
        expect(screen.queryByText("Random viable")).toBeNull();
        expect(screen.getByRole("button", { name: "Load Candidate Lists" })).toBeTruthy();
    });
});

describe("PollActionAuditPanel", () => {
    beforeEach(() => {
        vi.clearAllMocks();
        usePollsMock.mockReturnValue({
            sortedByDate: () => [],
        });
        useAutopopulateVenueSelectorMock.mockReturnValue({
            mostVisited: [],
            leastVisited: [],
            random: [],
            isLoading: false,
            error: null,
        });
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
