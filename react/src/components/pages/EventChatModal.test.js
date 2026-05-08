// @vitest-environment jsdom

import React from "react";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import EventChatModal from "./EventChatModal";

const {
    useSelectorMock,
    getDocMock,
    docMock,
    setEventChatMutedMock,
} = vi.hoisted(() => ({
    useSelectorMock: vi.fn(),
    getDocMock: vi.fn(),
    docMock: vi.fn(),
    setEventChatMutedMock: vi.fn(),
}));

vi.mock("react-redux", () => ({
    useSelector: useSelectorMock,
}));

vi.mock("firebase/firestore", () => ({
    doc: docMock,
    getDoc: getDocMock,
}));

vi.mock("../../firebase", () => ({
    db: {},
}));

vi.mock("../../push/webPush", () => ({
    setEventChatMuted: setEventChatMutedMock,
}));

vi.mock("../UI/Modal", () => ({
    default: ({ children }) => <div>{children}</div>,
}));

vi.mock("../chat/ChatBox", () => ({
    default: () => <div data-testid="chat-box" />,
}));

vi.mock("../UI/Button", () => ({
    default: ({ children, ...props }) => <button {...props}>{children}</button>,
}));

vi.mock("../chat/ChatMuteButton", () => ({
    default: ({ muted, busy, onToggle, label }) => (
        <button
            type="button"
            aria-label={label}
            disabled={busy}
            onClick={onToggle}
            data-testid="mute-button"
        >
            {muted ? "muted" : "unmuted"}
        </button>
    ),
}));

describe("EventChatModal", () => {
    beforeEach(() => {
        useSelectorMock.mockReset();
        getDocMock.mockReset();
        docMock.mockReset();
        setEventChatMutedMock.mockReset();

        useSelectorMock.mockImplementation((selector) =>
            selector({ auth: { uid: "user-1" } })
        );
        docMock.mockReturnValue({});
        getDocMock.mockResolvedValue({ data: () => ({ pushPreferences: {} }) });
        setEventChatMutedMock.mockResolvedValue(undefined);
    });

    it("links the modal title to the dedicated event chat page", () => {
        render(
            <MemoryRouter>
                <EventChatModal pollId="poll-42" onClose={() => undefined} />
            </MemoryRouter>,
        );

        const link = screen.getByRole("link", { name: "Event Chat" });
        expect(link.getAttribute("href")).toBe("/chat/event/poll-42");
    });

    it("shows the mute button when user is logged in", async () => {
        const rendered = render(
            <MemoryRouter>
                <EventChatModal pollId="poll-42" onClose={() => undefined} />
            </MemoryRouter>,
        );

        await waitFor(() => {
            expect(within(rendered.container).getByTestId("mute-button")).toBeTruthy();
        });
    });

    it("toggles event chat mute via the mute button", async () => {
        const rendered = render(
            <MemoryRouter>
                <EventChatModal pollId="poll-42" onClose={() => undefined} />
            </MemoryRouter>,
        );

        const button = await waitFor(() =>
            within(rendered.container).getByRole("button", {
                name: "Mute event chat notifications",
            })
        );
        fireEvent.click(button);

        await waitFor(() => {
            expect(setEventChatMutedMock).toHaveBeenCalledWith("user-1", "poll-42", true);
        });
    });
});
