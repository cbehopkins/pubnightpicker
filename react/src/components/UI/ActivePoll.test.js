// @vitest-environment jsdom

import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import ActivePoll from "./ActivePoll";
import { addVenueToMeet } from "../../dbtools/polls";

const { useRoleMock, useOnlineStatusMock, useAutoPopulateMock, autopopulateActionMock, pollVotePropsMock } = vi.hoisted(() => ({
    useRoleMock: vi.fn(),
    useOnlineStatusMock: vi.fn(),
    useAutoPopulateMock: vi.fn(),
    autopopulateActionMock: vi.fn(async () => undefined),
    pollVotePropsMock: vi.fn(),
}));

vi.mock("../../dbtools/polls", () => ({
    addVenueToMeet: vi.fn(async () => undefined),
    deleteMeet: vi.fn(async () => undefined),
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

vi.mock("./PollVote", () => ({
    default: (props) => {
        pollVotePropsMock(props);
        return <div data-testid="poll-vote" />;
    },
}));

vi.mock("./VenueOptions", () => ({
    default: ({ venues, selectVenueHandler }) => (
        <select data-testid="venue-options-select" defaultValue="" onChange={selectVenueHandler}>
            <option value="">Select a venue to add here</option>
            {Object.keys(venues).map((id) => (
                <option key={id} value={id}>
                    {venues[id]?.name || id}
                </option>
            ))}
        </select>
    ),
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
            venues={{
                v1: { name: "Venue 1", venueType: "pub", banned: true },
                v2: { name: "Venue 2", venueType: "pub" },
                v3: { name: "Venue 3", venueType: "pub" },
                v4: { name: "Venue 4", venueType: "pub" },
                ...(overrides.venueParameters || {}),
            }}
            poll_data={{ date: "2026-05-11", venues: { ...overrides.currentVenues } }}
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

    it("passes Meet venues to voting and autopopulate", () => {
        const venues = { v1: { name: "Venue 1" } };
        renderComponent({ currentVenues: venues });

        expect(pollVotePropsMock).toHaveBeenLastCalledWith(expect.objectContaining({
            poll_id: "poll-1",
            poll_data: { date: "2026-05-11", venues },
        }));
        expect(useAutoPopulateMock).toHaveBeenLastCalledWith("poll-1", venues);
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

        fireEvent.change(screen.getByTestId("venue-options-select"), { target: { value: "v1" } });
        fireEvent.click(screen.getByRole("button", { name: /add venue to poll/i }));

        await waitFor(() => {
            expect(addVenueToMeet).toHaveBeenCalledWith("v1", "poll-1", expect.any(Object), "2026-05-11");
        });

        expect(screen.getByRole("alert").textContent).toMatch(/marked as banned/i);
    });

    it("filters domain Venue attributes without excluding other Venue types", () => {
        renderComponent({
            venueParameters: {
                v1: { name: "Garden restaurant", venueType: "restaurant", dogFriendly: true, beerGarden: true, outOfTown: false },
                v2: { name: "Out of town event", venueType: "event", dogFriendly: true, beerGarden: true, outOfTown: true },
                v3: { name: "Indoor pub", venueType: "pub", dogFriendly: true, beerGarden: false },
                v4: { name: "No dogs pub", venueType: "pub", dogFriendly: false, beerGarden: true },
            }
        });

        fireEvent.click(screen.getByRole("checkbox", { name: "Dog Friendly" }));
        fireEvent.click(screen.getByRole("checkbox", { name: "Beer Garden" }));
        fireEvent.click(screen.getAllByRole("checkbox", { name: "Out Of Town (Cambridge)" })[1]);

        expect(screen.getByRole("option", { name: "Garden restaurant" })).toBeTruthy();
        expect(screen.queryByRole("option", { name: "Out of town event" })).toBeNull();
        expect(screen.queryByRole("option", { name: "Indoor pub" })).toBeNull();
        expect(screen.queryByRole("option", { name: "No dogs pub" })).toBeNull();
    });

    it("uses the exclusion label to filter out of town venues", () => {
        renderComponent({
            venueParameters: {
                v1: { name: "In town", outOfTown: false },
                v2: { name: "Out of town", outOfTown: true },
            }
        });

        fireEvent.click(screen.getAllByText("Out Of Town (Cambridge)")[1]);

        expect(screen.getByRole("option", { name: "In town" })).toBeTruthy();
        expect(screen.queryByRole("option", { name: "Out of town" })).toBeNull();
    });

    it("filters domain venues and excludes choices already on the meet", () => {
        renderComponent({
            currentVenues: { v1: { name: "Venue 1" } },
            venueParameters: {
                v1: { name: "Venue 1", dogFriendly: true },
                v2: { name: "Venue 2", dogFriendly: true },
            },
        });

        fireEvent.click(screen.getByRole("checkbox", { name: "Dog Friendly" }));
        expect(screen.queryByRole("option", { name: "Venue 1" })).toBeNull();
        expect(screen.getByRole("option", { name: "Venue 2" })).toBeTruthy();
        expect(screen.queryByRole("option", { name: "Venue 3" })).toBeNull();
    });
});
