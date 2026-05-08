// @vitest-environment jsdom

import React from "react";
import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import ChatPage from "./ChatPage";

const {
    useSelectorMock,
    useRoleMock,
    useNavigateMock,
    useParamsMock,
    chatBoxMock,
} = vi.hoisted(() => ({
    useSelectorMock: vi.fn(),
    useRoleMock: vi.fn(),
    useNavigateMock: vi.fn(),
    useParamsMock: vi.fn(),
    chatBoxMock: vi.fn(() => <div data-testid="chat-box" />),
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

describe("ChatPage", () => {
    beforeEach(() => {
        useSelectorMock.mockReset();
        useRoleMock.mockReset();
        useNavigateMock.mockReset();
        useParamsMock.mockReset();
        chatBoxMock.mockClear();

        useSelectorMock.mockImplementation((selector) => selector({ auth: { loggedIn: true } }));
        useRoleMock.mockReturnValue(true);
        useNavigateMock.mockReturnValue(vi.fn());
        useParamsMock.mockReturnValue({});
    });

    it("renders global chat by default", () => {
        render(<ChatPage />);

        expect(screen.getByText("Chat Page")).toBeTruthy();
        expect(chatBoxMock).toHaveBeenCalledWith(
            { scope: undefined },
            expect.anything(),
        );
    });

    it("renders event-scoped chat when pollId is present", () => {
        useParamsMock.mockReturnValue({ pollId: "poll-42" });

        render(<ChatPage />);

        expect(screen.getByText("Event Chat")).toBeTruthy();
        expect(screen.getByText("Back to current events")).toBeTruthy();
        expect(chatBoxMock).toHaveBeenCalledWith(
            { scope: { scopeType: "event", scopeId: "poll-42" } },
            expect.anything(),
        );
    });
});
