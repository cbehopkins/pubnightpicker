// @vitest-environment jsdom

import React from "react";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import ChatPage from "./ChatPage";

const {
    useSelectorMock,
    useRoleMock,
    useNavigateMock,
    useParamsMock,
    chatBoxMock,
    getDocMock,
    docMock,
    setEventChatMutedMock,
    setGlobalChatMutedMock,
} = vi.hoisted(() => ({
    useSelectorMock: vi.fn(),
    useRoleMock: vi.fn(),
    useNavigateMock: vi.fn(),
    useParamsMock: vi.fn(),
    chatBoxMock: vi.fn(() => <div data-testid="chat-box" />),
    getDocMock: vi.fn(),
    docMock: vi.fn(),
    setEventChatMutedMock: vi.fn(),
    setGlobalChatMutedMock: vi.fn(),
}));

vi.mock("react-redux", () => ({
    useSelector: useSelectorMock,
}));

vi.mock("../../hooks/useRole", () => ({
    default: useRoleMock,
}));

vi.mock("../chat/ChatBox", () => ({
    default: chatBoxMock,
}));

vi.mock("react-router-dom", async () => {
    const actual = await vi.importActual("react-router-dom");
    return {
        ...actual,
        Link: ({ children, to }) => <a href={to}>{children}</a>,
        useNavigate: useNavigateMock,
        useParams: useParamsMock,
    };
});

vi.mock("firebase/firestore", () => ({
    doc: docMock,
    getDoc: getDocMock,
}));

vi.mock("../../firebase", () => ({
    db: {},
}));

vi.mock("../../push/webPush", () => ({
    setEventChatMuted: setEventChatMutedMock,
    setGlobalChatMuted: setGlobalChatMutedMock,
}));

describe("ChatPage", () => {
    beforeEach(() => {
        useSelectorMock.mockReset();
        useRoleMock.mockReset();
        useNavigateMock.mockReset();
        useParamsMock.mockReset();
        chatBoxMock.mockClear();
        getDocMock.mockReset();
        docMock.mockReset();
        setEventChatMutedMock.mockReset();
        setGlobalChatMutedMock.mockReset();

        useSelectorMock.mockImplementation((selector) => selector({ auth: { loggedIn: true, uid: "user-1" } }));
        useRoleMock.mockReturnValue(true);
        useNavigateMock.mockReturnValue(vi.fn());
        useParamsMock.mockReturnValue({});
        getDocMock.mockResolvedValue({ data: () => ({ webPushEnabled: true, pushPreferences: {} }) });
        docMock.mockReturnValue({});
        setEventChatMutedMock.mockResolvedValue(undefined);
        setGlobalChatMutedMock.mockResolvedValue(undefined);
    });

    it("renders global chat by default", () => {
        render(<ChatPage />);

        expect(screen.getByText("Chat Page")).toBeTruthy();
        expect(chatBoxMock).toHaveBeenCalledWith(
            { scope: undefined },
            expect.anything(),
        );
    });

    it("renders event-scoped chat when pollId is present", async () => {
        useParamsMock.mockReturnValue({ pollId: "poll-42" });

        render(<ChatPage />);

        expect(screen.getByText("Event Chat")).toBeTruthy();
        expect(screen.getByText("Back to current events")).toBeTruthy();
        expect(chatBoxMock).toHaveBeenCalledWith(
            { scope: { scopeType: "event", scopeId: "poll-42" } },
            expect.anything(),
        );
        await waitFor(() => {
            expect(within(document.body).getByText("Event chat notifications are enabled for this event.")).toBeTruthy();
        });
    });

    it("hides mute button when push notifications are not enabled", async () => {
        getDocMock.mockResolvedValue({ data: () => ({ webPushEnabled: false, pushPreferences: {} }) });
        useParamsMock.mockReturnValue({ pollId: "poll-42" });

        const rendered = render(<ChatPage />);

        // Wait for the async getDoc to settle
        await waitFor(() => {
            expect(getDocMock).toHaveBeenCalled();
        });
        expect(within(rendered.container).queryByRole("button", { name: /mute/i })).toBeNull();
    });

    it("toggles event chat mute for the current poll", async () => {
        useParamsMock.mockReturnValue({ pollId: "poll-42" });

        const rendered = render(<ChatPage />);

        const button = await waitFor(() =>
            within(rendered.container).getByRole("button", {
                name: "Mute event chat notifications",
            })
        );
        fireEvent.click(button);

        await waitFor(() => {
            expect(setEventChatMutedMock).toHaveBeenCalledWith("user-1", "poll-42", true);
        });
        expect(rendered.getByText("Event chat notifications are muted for this event.")).toBeTruthy();
    });

    it("toggles global chat mute on the main chat page", async () => {
        const rendered = render(<ChatPage />);

        await waitFor(() => {
            expect(within(rendered.container).getByText("Global chat notifications are enabled.")).toBeTruthy();
        });

        const button = within(rendered.container).getByRole("button", {
            name: "Mute global chat notifications",
        });
        fireEvent.click(button);

        await waitFor(() => {
            expect(setGlobalChatMutedMock).toHaveBeenCalledWith("user-1", true);
        });
        expect(within(rendered.container).getByText("Global chat notifications are muted.")).toBeTruthy();
    });
});
