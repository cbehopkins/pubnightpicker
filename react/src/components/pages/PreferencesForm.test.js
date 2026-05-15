// @vitest-environment jsdom

import React from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, fireEvent } from "@testing-library/react";

const useWebPushSettingsMock = vi.hoisted(() => vi.fn());

vi.mock("../../hooks/useWebPushSettings", () => ({
    default: useWebPushSettingsMock,
}));

import { PushPreferences } from "./PreferencesForm";

afterEach(() => {
    cleanup();
    vi.clearAllMocks();
});

describe("PushPreferences", () => {
    it("updates checkbox state when pushPreferences are loaded after initial render", () => {
        useWebPushSettingsMock.mockReturnValue({
            busy: false,
            disable: vi.fn(),
            enable: vi.fn(),
            enabled: true,
            error: "",
            featureEnabled: true,
            permission: "granted",
            supported: true,
        });

        const { rerender } = render(
            <PushPreferences
                uid="user-1"
                initialEnabled={true}
                pushPreferences={null}
            />
        );

        const globalChatCheckbox = screen.getByLabelText("A message is sent in global chat");
        expect(globalChatCheckbox.checked).toBe(false);

        rerender(
            <PushPreferences
                uid="user-1"
                initialEnabled={true}
                pushPreferences={{
                    pollOpens: true,
                    pollCompletes: true,
                    globalChat: true,
                    eventChat: false,
                }}
            />
        );

        expect(screen.getByLabelText("A message is sent in global chat").checked).toBe(true);
    });

    it("shows migration defaults when pushPreferences are missing", () => {
        useWebPushSettingsMock.mockReturnValue({
            busy: false,
            disable: vi.fn(),
            enable: vi.fn(),
            enabled: true,
            error: "",
            featureEnabled: true,
            permission: "granted",
            supported: true,
        });

        render(
            <PushPreferences
                uid="user-1"
                initialEnabled={true}
                pushPreferences={null}
            />
        );

        expect(screen.getByLabelText("A poll opens").checked).toBe(true);
        expect(screen.getByLabelText("A poll completes").checked).toBe(true);
        expect(screen.getByLabelText("A message is sent in global chat").checked).toBe(false);
        expect(screen.getByLabelText("A message is sent in an event chat I am attending").checked).toBe(false);
    });

    it("renders Push Preferences with subscription status and buttons when enabled", () => {
        useWebPushSettingsMock.mockReturnValue({
            busy: false,
            disable: vi.fn(),
            enable: vi.fn(),
            enabled: true,
            error: "",
            featureEnabled: true,
            permission: "granted",
            supported: true,
        });

        render(
            <PushPreferences
                uid="user-1"
                initialEnabled={true}
                pushPreferences={{
                    pollOpens: true,
                    pollCompletes: true,
                    globalChat: false,
                    eventChat: false,
                }}
            />
        );

        expect(screen.getByText(/Status: Enabled/)).toBeTruthy();
        expect(screen.getByText(/Permission: Granted/)).toBeTruthy();
        expect(screen.getByText("Enable Push")).toBeTruthy();
        expect(screen.getByText("Disable Push")).toBeTruthy();
    });

    it("hides notification preferences when not enabled", () => {
        useWebPushSettingsMock.mockReturnValue({
            busy: false,
            disable: vi.fn(),
            enable: vi.fn(),
            enabled: false,
            error: "",
            featureEnabled: true,
            permission: "granted",
            supported: true,
        });

        render(
            <PushPreferences
                uid="user-1"
                initialEnabled={false}
                pushPreferences={{
                    pollOpens: true,
                    pollCompletes: true,
                    globalChat: false,
                    eventChat: false,
                }}
            />
        );

        expect(screen.queryByLabelText("A poll opens")).toBeNull();
        expect(screen.queryByText(/Notify me when:/)).toBeNull();
    });

    it("toggles notification preferences when enabled", async () => {
        useWebPushSettingsMock.mockReturnValue({
            busy: false,
            disable: vi.fn(),
            enable: vi.fn(),
            enabled: true,
            error: "",
            featureEnabled: true,
            permission: "granted",
            supported: true,
        });

        render(
            <PushPreferences
                uid="user-1"
                initialEnabled={true}
                pushPreferences={{
                    pollOpens: true,
                    pollCompletes: false,
                    globalChat: false,
                    eventChat: false,
                }}
            />
        );

        const globalChatCheckbox = screen.getByLabelText("A message is sent in global chat");
        expect(globalChatCheckbox.checked).toBe(false);

        fireEvent.click(globalChatCheckbox);
        expect(globalChatCheckbox.checked).toBe(true);
    });

    it("shows error message when web push encounters an error", () => {
        const errorMessage = "Failed to access push service";
        useWebPushSettingsMock.mockReturnValue({
            busy: false,
            disable: vi.fn(),
            enable: vi.fn(),
            enabled: false,
            error: errorMessage,
            featureEnabled: true,
            permission: "granted",
            supported: true,
        });

        render(
            <PushPreferences
                uid="user-1"
                initialEnabled={false}
                pushPreferences={null}
            />
        );

        expect(screen.getByText(errorMessage)).toBeTruthy();
    });

    it("returns null when feature is not enabled", () => {
        useWebPushSettingsMock.mockReturnValue({
            busy: false,
            disable: vi.fn(),
            enable: vi.fn(),
            enabled: false,
            error: "",
            featureEnabled: false,
            permission: "default",
            supported: false,
        });

        const { container } = render(
            <PushPreferences
                uid="user-1"
                initialEnabled={false}
                pushPreferences={null}
            />
        );

        expect(container.firstChild).toBeNull();
    });
});
