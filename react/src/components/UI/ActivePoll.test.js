// @vitest-environment jsdom

import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import ActivePoll from "./ActivePoll";
import { add_new_pub_to_poll } from "../../dbtools/polls";

const { useRoleMock, useOnlineStatusMock, useAutoPopulateMock, autopopulateActionMock } = vi.hoisted(() => ({
    useRoleMock: vi.fn(),
    useOnlineStatusMock: vi.fn(),
    useAutoPopulateMock: vi.fn(),
    autopopulateActionMock: vi.fn(async () => undefined),
}));

vi.mock("../../dbtools/polls", () => ({
    add_new_pub_to_poll: vi.fn(async () => undefined),
    deletePoll: vi.fn(async () => undefined),
}));

vi.mock("../../hooks/useRole", () => ({
    default: useRoleMock,
}));

vi.mock("../../hooks/useOnlineStatus", () => ({
    default: useOnlineStatusMock,
}));

vi.mock("../../hooks/useAutopopulateVenueSelector", () => ({
    default: useAutoPopulateMock,
}));

vi.mock("../../hooks/useAutopopulateAction", () => ({
    default: () => ({
        handleAutopopulate: autopopulateActionMock,
    }),
}));

vi.mock("../pages/PubForm", () => ({
    AntiPubParams: {},
}));

vi.mock("./PollVote", () => ({
    default: () => <div data-testid="poll-vote" />,
}));

vi.mock("./PubOptions", () => ({
    default: ({ pub_parameters, selectPubHandler }) => (
        <select data-testid="pub-options-select" defaultValue="" onChange={selectPubHandler}>
            <option value="">Select a venue to add here</option>
            {Object.keys(pub_parameters).map((id) => (
                <option key={id} value={id}>
                    {pub_parameters[id]?.name || id}
                </option>
            ))}
        </select>
    ),
}));

vi.mock("./PubFilter", () => ({
    default: () => <div data-testid="pub-filter" />,
}));

vi.mock("./NotificationPingStatus", () => ({
    default: () => <div data-testid="notification-ping-status" />,
}));

vi.mock("../pages/EventChatModal", () => ({
    default: () => <div data-testid="event-chat-modal" />,
}));

function renderComponent(overrides = {}) {
    return render(
        <ActivePoll
            poll_id="poll-1"
            pub_parameters={{
                v1: { name: "Venue 1", venueType: "pub", banned: true },
                v2: { name: "Venue 2", venueType: "pub" },
                v3: { name: "Venue 3", venueType: "pub" },
                v4: { name: "Venue 4", venueType: "pub" },
                ...(overrides.pubParameters || {}),
            }}
            poll_data={{ date: "2026-05-11", pubs: { ...overrides.currentPubs } }}
            on_complete={vi.fn()}
        />
    );
}

describe("ActivePoll autopopulate", () => {
    beforeEach(() => {
        vi.clearAllMocks();

        useRoleMock.mockImplementation((role) => {
            if (role === "canAddPubToPoll") return true;
            return false;
        });

        useOnlineStatusMock.mockReturnValue({ isOnline: true });

        useAutoPopulateMock.mockReturnValue({
            mostVisited: [{ id: "v1", label: "Venue 1", count: 10, lastWonDate: "2026-04-10" }],
            leastVisited: [{ id: "v2", label: "Venue 2", count: 1, lastWonDate: "2026-04-09" }],
            random: [{ id: "v3", label: "Venue 3", count: 0, lastWonDate: null }],
            isLoading: false,
            error: null,
        });
    });

    afterEach(() => {
        cleanup();
    });

    it("renders autopopulate button when add-pub permission is available", () => {
        renderComponent();
        expect(screen.getByRole("button", { name: /autopopulate/i })).toBeTruthy();
    });

    it("adds up to three distinct venues when autopopulate is clicked", async () => {
        renderComponent();

        fireEvent.click(screen.getByRole("button", { name: /autopopulate/i }));

        await waitFor(() => {
            expect(autopopulateActionMock).toHaveBeenCalledTimes(1);
        });
    });

    it("passes through click for overlapping categories", async () => {
        useAutoPopulateMock.mockReturnValue({
            mostVisited: [{ id: "v1", label: "Venue 1", count: 10, lastWonDate: "2026-04-10" }],
            leastVisited: [{ id: "v1", label: "Venue 1", count: 10, lastWonDate: "2026-04-10" }],
            random: [{ id: "v1", label: "Venue 1", count: 10, lastWonDate: "2026-04-10" }],
            isLoading: false,
            error: null,
        });

        renderComponent();

        fireEvent.click(screen.getByRole("button", { name: /autopopulate/i }));

        await waitFor(() => {
            expect(autopopulateActionMock).toHaveBeenCalledTimes(1);
        });
    });

    it("disables autopopulate button when no viable venues exist", () => {
        useAutoPopulateMock.mockReturnValue({
            mostVisited: [],
            leastVisited: [],
            random: [],
            isLoading: false,
            error: null,
        });

        renderComponent();

        expect(screen.getByRole("button", { name: /autopopulate/i }).hasAttribute("disabled")).toBe(true);
    });

    it("adds banned venue manually and shows warning", async () => {
        renderComponent();

        fireEvent.change(screen.getByTestId("pub-options-select"), { target: { value: "v1" } });
        fireEvent.click(screen.getByRole("button", { name: /add venue to poll/i }));

        await waitFor(() => {
            expect(add_new_pub_to_poll).toHaveBeenCalledWith("v1", "poll-1", expect.any(Object));
        });

        expect(screen.getByRole("alert").textContent).toMatch(/marked as banned/i);
    });
});
