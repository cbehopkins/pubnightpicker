// @vitest-environment jsdom

import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import PollVote from "./PollVote";

const { showAttendanceMock, useSelectorMock, useVotesMock, useAttendanceMock, useRoleMock, useUserPrivateDataMock, useUsersMock, usePollRowsMock, useBallotActionsMock, useVotableRowMock } = vi.hoisted(() => ({
    showAttendanceMock: vi.fn(() => <div data-testid="show-attendance" />),
    useSelectorMock: vi.fn(),
    useVotesMock: vi.fn(),
    useAttendanceMock: vi.fn(),
    useRoleMock: vi.fn(),
    useUserPrivateDataMock: vi.fn(),
    useUsersMock: vi.fn(),
    usePollRowsMock: vi.fn(),
    useBallotActionsMock: vi.fn(),
    useVotableRowMock: vi.fn(),
}));

vi.mock("react-redux", () => ({
    useSelector: useSelectorMock,
}));

vi.mock("../../hooks/useVotes", () => ({
    default: useVotesMock,
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

vi.mock("../../hooks/usePollRows", () => ({
    usePollRows: usePollRowsMock,
}));

vi.mock("../../hooks/useBallotActions", () => ({
    useBallotActions: useBallotActionsMock,
}));

vi.mock("../../hooks/useVotableRow", () => ({
    useVotableRow: useVotableRowMock,
}));

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

vi.mock("./PubOptions", () => ({
    default: () => <div data-testid="pub-options" />,
}));

vi.mock("./PubFilter", () => ({
    default: () => <div data-testid="pub-filter" />,
}));

vi.mock("./AutopopulateButton", () => ({
    default: () => <div data-testid="autopopulate-button" />,
}));

vi.mock("./NotificationPingStatus", () => ({
    default: () => <div data-testid="notification-ping-status" />,
}));

vi.mock("../pages/EventChatModal", () => ({
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
                pubs: {
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
        useVotesMock.mockReturnValue([{
            any: [],
            "venue-1": [],
        }, vi.fn(), vi.fn()]);
        useAttendanceMock.mockReturnValue([
            {},
            vi.fn(),
            vi.fn(),
            vi.fn(),
            vi.fn(),
            vi.fn(),
            vi.fn(),
        ]);
        useRoleMock.mockImplementation((roleName) => roleName === "canShowVoters");
        useUserPrivateDataMock.mockReturnValue(null);
        useUsersMock.mockReturnValue({
            "user-1": { uid: "user-1", name: "Alice", votesVisible: true },
            "user-2": { uid: "user-2", name: "Bob", votesVisible: true },
        });
        usePollRowsMock.mockReturnValue([
            ["Global", "any"],
            ["Venue 1", "venue-1"],
        ]);
        useBallotActionsMock.mockReturnValue({
            pollPubIds: ["venue-1"],
            setAllAttendanceToCanCome: vi.fn(),
            setAllAttendanceToCannotCome: vi.fn(),
        });
        useVotableRowMock.mockReturnValue({
            voteCount: 0,
            votedFor: false,
            userCanCome: true,
            userCannotCome: false,
            canCome: ["user-1"],
            cannotCome: [],
            canVote: true,
            allowAttendanceControls: true,
            allowGlobalAttendanceControls: false,
            hasAttendanceData: true,
            userEta: undefined,
            voteHandler: vi.fn(),
            setAttendanceStatusHandler: vi.fn(),
            clearAttendanceHandler: vi.fn(),
            setEtaHandler: vi.fn(),
            clearEtaHandler: vi.fn(),
            deleteHandler: vi.fn(),
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
        renderPollVote(true);

        expect(screen.getAllByRole("button", { name: /show attendance/i })).toHaveLength(2);
        expect(showAttendanceMock).not.toHaveBeenCalled();
    });
});
