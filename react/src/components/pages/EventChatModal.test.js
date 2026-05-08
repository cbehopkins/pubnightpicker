// @vitest-environment jsdom

import React from "react";
import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import EventChatModal from "./EventChatModal";

vi.mock("../UI/Modal", () => ({
    default: ({ children }) => <div>{children}</div>,
}));

vi.mock("../chat/ChatBox", () => ({
    default: () => <div data-testid="chat-box" />,
}));

vi.mock("../UI/Button", () => ({
    default: ({ children, ...props }) => <button {...props}>{children}</button>,
}));

describe("EventChatModal", () => {
    it("links the modal title to the dedicated event chat page", () => {
        render(
            <MemoryRouter>
                <EventChatModal pollId="poll-42" onClose={() => undefined} />
            </MemoryRouter>,
        );

        const link = screen.getByRole("link", { name: "Event Chat" });
        expect(link.getAttribute("href")).toBe("/chat/event/poll-42");
    });
});
