// @vitest-environment jsdom

import React from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";

const useWebPushSettingsMock = vi.hoisted(() => vi.fn());

vi.mock("../../hooks/useWebPushSettings", () => ({
    default: useWebPushSettingsMock,
}));

import { PushPreferences } from "./PreferencesForm";

afterEach(() => {
    cleanup();
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
});
