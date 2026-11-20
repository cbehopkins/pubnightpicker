// @vitest-environment jsdom

import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import PollVote from "./PollVote";
import ActivePoll from "./ActivePoll";
import { removeVenueFromMeet } from "../../dbtools/polls";

const { showAttendanceMock, useSelectorMock, useAttendanceMock, useRoleMock, useUserPrivateDataMock, useUsersMock, onSnapshotMock, updateDocMock, setGlobalAttendanceMock } = vi.hoisted(() => ({
    showAttendanceMock: vi.fn(() => <div data-testid="show-attendance" />),
    useSelectorMock: vi.fn(),
    useAttendanceMock: vi.fn(),
    useRoleMock: vi.fn(),
    useUserPrivateDataMock: vi.fn(),
    useUsersMock: vi.fn(),
    onSnapshotMock: vi.fn(),
    updateDocMock: vi.fn(async () => undefined),
    setGlobalAttendanceMock: vi.fn(),
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
}));

vi.mock("../../hooks/useAttendance", () => ({
    default: useAttendanceMock,
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

function renderPollVote(mobile) {
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
            on_complete={vi.fn()}
            mobile={mobile}
        />
    );
}

describe("PollVote attendance display mode", () => {
    beforeEach(() => {
        vi.clearAllMocks();
        useSelectorMock.mockReturnValue("user-1");
        onSnapshotMock.mockImplementation((_ref, onNext) => {
            onNext({ data: () => ({ any: [], "venue-1": [] }) });
            return () => { };
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
        useRoleMock.mockImplementation((roleName) => roleName === "canShowVoters");
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
        onSnapshotMock.mockImplementation((_ref, onNext) => {
            onNext({ data: () => ({ any: ["user-1"], "venue-1": ["user-1"] }) });
            return () => { };
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
        await waitFor(() => expect(updateDocMock).toHaveBeenCalledWith("votes/poll-1", { "venue-1": "arrayUnion:user-1" }));
    });

    it("removes a Meet venue when permitted", async () => {
        useRoleMock.mockImplementation((roleName) => roleName === "canAddPubToPoll");
        renderPollVote(false);
        const row = screen.getByText("Venue 1").closest("tr");

        fireEvent.click(within(row).getByRole("button", { name: "Delete" }));

        await waitFor(() => expect(removeVenueFromMeet).toHaveBeenCalledWith("poll-1", "venue-1", "2026-05-11", "Venue 1"));
    });

    it("sets attendance for every Meet venue", async () => {
        renderPollVote(false);
        const row = screen.getByText("Global").closest("tr");
        fireEvent.click(within(row).getByRole("button", { name: "Respond" }));
        fireEvent.click(within(row).getByRole("button", { name: "I'm coming (all venues)" }));
        await waitFor(() => expect(setGlobalAttendanceMock).toHaveBeenCalledWith(["venue-1"], "user-1", "canCome"));
    });
});
