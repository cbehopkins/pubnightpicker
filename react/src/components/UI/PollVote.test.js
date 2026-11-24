// @vitest-environment jsdom

import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import PollVote from "./PollVote";
import ActivePoll from "./ActivePoll";
import { removeVenueFromMeet } from "../../dbtools/polls";

const { showAttendanceMock, useSelectorMock, useAttendanceMock, useRoleMock, useUserPrivateDataMock, useUsersMock, useVotesMock, changeVoteMock, retryVoteMock, onSnapshotMock, updateDocMock, setGlobalAttendanceMock, runTransactionMock } = vi.hoisted(() => ({
    showAttendanceMock: vi.fn(() => <div data-testid="show-attendance" />),
    useSelectorMock: vi.fn(),
    useAttendanceMock: vi.fn(),
    useRoleMock: vi.fn(),
    useUserPrivateDataMock: vi.fn(),
    useUsersMock: vi.fn(),
    useVotesMock: vi.fn(),
    changeVoteMock: vi.fn(async () => true),
    retryVoteMock: vi.fn(async () => true),
    onSnapshotMock: vi.fn(),
    updateDocMock: vi.fn(async () => undefined),
    setGlobalAttendanceMock: vi.fn(),
    runTransactionMock: vi.fn(async (_db, callback) => callback({
        get: vi.fn(async () => ({ exists: () => true })),
        set: vi.fn(async (ref, payload) => updateDocMock(ref, payload)),
    })),
}));

vi.mock("react-redux", () => ({
    useSelector: useSelectorMock,
}));

vi.mock("../../firebase", () => ({ db: {} }));

vi.mock("firebase/firestore", () => ({
    doc: (_db, collection, id) => `${collection}/${id}`,
    onSnapshot: onSnapshotMock,
    updateDoc: updateDocMock,
    arrayUnion: (value) => `arrayUnion:${value}`,
    arrayRemove: (value) => `arrayRemove:${value}`,
    runTransaction: runTransactionMock,
}));

vi.mock("../../hooks/useAttendance", () => ({
    default: useAttendanceMock,
}));

vi.mock("../../hooks/useVotes", () => ({
    default: useVotesMock,
}));

vi.mock("../../hooks/useRole", () => ({
    default: useRoleMock,
}));

vi.mock("../../hooks/useUserPrivateData", () => ({
    default: useUserPrivateDataMock,
}));

vi.mock("../../hooks/useUsers", () => ({
    default: useUsersMock,
}));

vi.mock("../../dbtools/polls", () => ({
    addVenueToMeet: vi.fn(),
    deleteMeet: vi.fn(),
    removeVenueFromMeet: vi.fn(async () => undefined),
}));

vi.mock("../../hooks/useOnlineStatus", () => ({ default: () => ({ isOnline: true }) }));
vi.mock("../../hooks/useAutopopulateVenueSelector", () => ({
    default: () => ({ mostVisited: [], leastVisited: [], random: [], isLoading: false }),
}));
vi.mock("../../hooks/useAutopopulateAction", () => ({ default: () => ({ handleAutopopulate: vi.fn() }) }));

vi.mock("./ShowAttendance", () => ({
    default: showAttendanceMock,
}));

vi.mock("./AttendanceActions", () => ({
    default: () => <div data-testid="attendance-actions" />,
}));

vi.mock("./ETAInput", () => ({
    default: () => <div data-testid="eta-input" />,
}));

vi.mock("./ConfirmModal", () => ({
    QuestionRender: ({ question }) => <button type="button">{question}</button>,
}));

vi.mock("./Button", () => ({
    default: ({ children, ...props }) => <button {...props}>{children}</button>,
}));

vi.mock("./VenueOptions", () => ({
    default: () => <div data-testid="venue-options" />,
}));

vi.mock("./VenueFilter", () => ({
    default: () => <div data-testid="venue-filter" />,
}));

vi.mock("./AutopopulateButton", () => ({
    default: () => <div data-testid="autopopulate-button" />,
}));

vi.mock("./NotificationPingStatus", () => ({
    default: () => <div data-testid="notification-ping-status" />,
}));

vi.mock("../pages/MeetChatModal", () => ({
    default: () => <div data-testid="event-chat-modal" />,
}));

vi.mock("./PollVote.module.css", () => ({
    default: new Proxy({}, { get: (_, key) => String(key) }),
}));

function renderPollVote(mobile, venues = {}) {
    return render(
        <PollVote
            poll_id="poll-1"
            poll_data={{
                date: "2026-05-11",
                venues: {
                    any: {},
                    "venue-1": { name: "Venue 1" },
                },
            }}
            venues={venues}
            on_complete={vi.fn()}
            mobile={mobile}
        />
    );
}

describe("PollVote attendance display mode", () => {
    beforeEach(() => {
        vi.clearAllMocks();
        useSelectorMock.mockReturnValue("user-1");
        useVotesMock.mockReturnValue({
            optionsById: {
                any: { total: 0, anonymousCount: 0, voterUids: [] },
                "venue-1": { total: 0, anonymousCount: 0, voterUids: [] },
            },
            voterProfiles: {
                "user-1": { uid: "user-1", name: "Alice" },
                "user-2": { uid: "user-2", name: "Bob" },
            },
            selectedOptionIds: [],
            pendingOptionIds: [],
            retrySelections: {},
            changeVote: changeVoteMock,
            retryVote: retryVoteMock,
            errorMessage: "",
        });
        useAttendanceMock.mockReturnValue([
            {},
            vi.fn(),
            vi.fn(),
            vi.fn(),
            setGlobalAttendanceMock,
            vi.fn(),
            vi.fn(),
        ]);
        useRoleMock.mockImplementation((roleName) => roleName === "canShowVoters" || roleName === "canVote");
        useUserPrivateDataMock.mockReturnValue(null);
        useUsersMock.mockReturnValue({
            "user-1": { uid: "user-1", name: "Alice", votesVisible: true },
            "user-2": { uid: "user-2", name: "Bob", votesVisible: true },
        });
    });

    afterEach(() => {
        cleanup();
    });

    it("shows attendance inline when the view is wide enough", () => {
        renderPollVote(false);

        expect(screen.queryByRole("button", { name: /show attendance/i })).toBeNull();
        expect(screen.getByRole("columnheader", { name: /^voted$/i })).toBeTruthy();
        expect(screen.getByRole("columnheader", { name: /^can come$/i })).toBeTruthy();
        expect(screen.getByRole("columnheader", { name: /^cannot come$/i })).toBeTruthy();
        expect(screen.getByRole("columnheader", { name: /^eta$/i })).toBeTruthy();
        expect(showAttendanceMock).not.toHaveBeenCalled();
    });

    it("keeps the modal trigger on compact screens", () => {
        useVotesMock.mockReturnValue({
            optionsById: {
                any: { total: 1, anonymousCount: 0, voterUids: ["user-1"] },
                "venue-1": { total: 1, anonymousCount: 0, voterUids: ["user-1"] },
            },
            voterProfiles: { "user-1": { uid: "user-1", name: "Alice" } },
            selectedOptionIds: ["any", "venue-1"],
            pendingOptionIds: [],
            retrySelections: {},
            changeVote: changeVoteMock,
            retryVote: retryVoteMock,
            errorMessage: "",
        });
        renderPollVote(true);

        expect(screen.getAllByRole("button", { name: /show attendance/i })).toHaveLength(2);
        expect(showAttendanceMock).not.toHaveBeenCalled();
    });

    it("renders the Meet's venue choices in the voting table", () => {
        renderPollVote(false);

        expect(screen.getByText("Venue 1")).toBeTruthy();
        expect(screen.getByText("Global")).toBeTruthy();
    });

    it("keeps Any-Venue attendance off restaurant voting rows but shows explicit responses", () => {
        const venue = { "venue-1": { name: "Bistro 12", venueType: "restaurant" } };
        useAttendanceMock.mockReturnValue([
            { any: { canCome: ["user-1"], cannotCome: [] } },
            vi.fn(), vi.fn(), vi.fn(), setGlobalAttendanceMock, vi.fn(), vi.fn(),
        ]);
        const { rerender } = renderPollVote(false, venue);
        const restaurantRow = screen.getByText("Bistro 12").closest("tr");
        expect(within(restaurantRow).queryByTitle("You said you can come")).toBeNull();

        useAttendanceMock.mockReturnValue([
            {
                any: { canCome: ["user-1"], cannotCome: [] },
                "venue-1": { canCome: ["user-1"], cannotCome: [] },
            },
            vi.fn(), vi.fn(), vi.fn(), setGlobalAttendanceMock, vi.fn(), vi.fn(),
        ]);
        rerender(
            <PollVote
                poll_id="poll-1"
                poll_data={{ date: "2026-05-11", venues: { any: {}, "venue-1": { name: "Venue 1" } } }}
                venues={venue}
                on_complete={vi.fn()}
                mobile={false}
            />
        );

        expect(within(screen.getByText("Bistro 12").closest("tr")).getByTitle("You said you can come")).toBeTruthy();
    });

    it("lets members withdraw their standing attendance response", async () => {
        const clearAttendanceMock = vi.fn();
        useAttendanceMock.mockReturnValue([
            { any: { canCome: ["user-1"], cannotCome: [] } },
            vi.fn(),
            clearAttendanceMock,
            vi.fn(),
            setGlobalAttendanceMock,
            vi.fn(),
            vi.fn(),
        ]);
        renderPollVote(false);

        const globalRow = screen.getByText("Global").closest("tr");
        fireEvent.click(within(globalRow).getByRole("button", { name: "Respond" }));
        fireEvent.click(await within(globalRow).findByRole("button", { name: "Clear response" }));

        expect(clearAttendanceMock).toHaveBeenCalledWith("any", "user-1");
    });

    it("renders a streamed candidate using its catalogue name", () => {
        const { rerender } = render(<ActivePoll
            poll_id="poll-1"
            poll_data={{ date: "2026-05-11", venues: { "venue-1": true } }}
            venues={{}}
            on_complete={vi.fn()}
        />);

        expect(within(screen.getByRole("table")).getByText("Unknown venue")).toBeTruthy();
        rerender(<ActivePoll
            poll_id="poll-1"
            poll_data={{ date: "2026-05-11", venues: { "venue-1": true } }}
            venues={{ "venue-1": { name: "Venue 1" } }}
            on_complete={vi.fn()}
        />);

        expect(within(screen.getByRole("table")).getByText("Venue 1")).toBeTruthy();
    });

    it("votes for a Meet venue", async () => {
        renderPollVote(false);
        const row = screen.getByText("Venue 1").closest("tr");
        fireEvent.click(within(row).getByRole("button", { name: "Respond" }));
        fireEvent.click(within(row).getByRole("button", { name: "Vote Up" }));
        await waitFor(() => expect(changeVoteMock).toHaveBeenCalledWith("venue-1", true));
    });

    it("keeps the any option independently selectable", async () => {
        renderPollVote(false);
        const row = screen.getByText("Global").closest("tr");
        fireEvent.click(within(row).getByRole("button", { name: "Respond" }));
        fireEvent.click(within(row).getByRole("button", { name: "Vote Up" }));

        await waitFor(() => expect(changeVoteMock).toHaveBeenCalledWith("any", true));
    });

    it("shows a disabled pending state while a vote is being saved", () => {
        useVotesMock.mockReturnValue({
            optionsById: { "venue-1": { total: 0, anonymousCount: 0, voterUids: [] } },
            voterProfiles: {},
            selectedOptionIds: [],
            pendingOptionIds: ["venue-1"],
            retrySelections: {},
            changeVote: changeVoteMock,
            retryVote: retryVoteMock,
            errorMessage: "",
        });
        renderPollVote(false);
        const row = screen.getByText("Venue 1").closest("tr");
        fireEvent.click(within(row).getByRole("button", { name: "Respond" }));

        expect(within(row).getByRole("button", { name: "Saving..." }).disabled).toBe(true);
    });

    it("lets voters withdraw a preserved vote for a removed option", async () => {
        useVotesMock.mockReturnValue({
            optionsById: { "venue-1": { total: 0, anonymousCount: 0, voterUids: [] } },
            voterProfiles: {},
            selectedOptionIds: ["removed-venue"],
            pendingOptionIds: [],
            retrySelections: {},
            changeVote: changeVoteMock,
            retryVote: retryVoteMock,
            errorMessage: "",
        });
        renderPollVote(false);

        fireEvent.click(screen.getByRole("button", { name: "Withdraw vote for removed-venue (removed)" }));

        await waitFor(() => expect(changeVoteMock).toHaveBeenCalledWith("removed-venue", false));
    });

    it("keeps a removed-option withdrawal available for retry", async () => {
        useVotesMock.mockReturnValue({
            optionsById: { "venue-1": { total: 0, anonymousCount: 0, voterUids: [] } },
            voterProfiles: {},
            selectedOptionIds: [],
            pendingOptionIds: [],
            retrySelections: { "removed-venue": false },
            changeVote: changeVoteMock,
            retryVote: retryVoteMock,
            errorMessage: "Your vote could not be confirmed.",
        });
        renderPollVote(false);

        fireEvent.click(screen.getByRole("button", { name: "Retry withdrawal for a removed option" }));

        await waitFor(() => expect(retryVoteMock).toHaveBeenCalledWith("removed-venue"));
        expect(screen.getByRole("status").textContent).toContain("Your vote could not be confirmed.");
    });

    it("removes a Meet venue when permitted", async () => {
        useRoleMock.mockImplementation((roleName) => roleName === "canAddPubToPoll");
        renderPollVote(false);
        const row = screen.getByText("Venue 1").closest("tr");

        fireEvent.click(within(row).getByRole("button", { name: "Delete" }));

        await waitFor(() => expect(removeVenueFromMeet).toHaveBeenCalledWith("poll-1", "venue-1"));
    });

    it("sets attendance for every Meet venue", async () => {
        renderPollVote(false);
        const row = screen.getByText("Global").closest("tr");
        fireEvent.click(within(row).getByRole("button", { name: "Respond" }));
        fireEvent.click(within(row).getByRole("button", { name: "I'm coming (any venue)" }));
        await waitFor(() => expect(setGlobalAttendanceMock).toHaveBeenCalledWith(["venue-1"], "user-1", "canCome"));
    });
});
