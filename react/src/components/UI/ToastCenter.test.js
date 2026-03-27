// @vitest-environment jsdom

import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import ToastCenter from "./ToastCenter";
import { NOTIFY_EVENT_NAME } from "../../utils/notify";

describe("ToastCenter", () => {
    afterEach(() => {
        cleanup();
        vi.useRealTimers();
    });

    it("renders notifications from app notify events", () => {
        render(<ToastCenter />);

        act(() => {
            window.dispatchEvent(new CustomEvent(NOTIFY_EVENT_NAME, {
                detail: {
                    id: "toast-1",
                    level: "info",
                    message: "Saved successfully",
                },
            }));
        });

        expect(screen.getByText("Saved successfully")).toBeTruthy();
    });

    it("dismisses toasts manually and after timeout", () => {
        vi.useFakeTimers();

        render(<ToastCenter />);

        act(() => {
            window.dispatchEvent(new CustomEvent(NOTIFY_EVENT_NAME, {
                detail: {
                    id: "toast-manual",
                    level: "error",
                    message: "Permission denied",
                },
            }));
        });

        const toast = screen.getByText("Permission denied");
        expect(toast).toBeTruthy();

        fireEvent.click(screen.getByRole("button", { name: "Dismiss notification" }));
        expect(screen.queryByText("Permission denied")).toBeNull();

        act(() => {
            window.dispatchEvent(new CustomEvent(NOTIFY_EVENT_NAME, {
                detail: {
                    id: "toast-auto",
                    level: "info",
                    message: "Auto close",
                },
            }));
        });

        expect(screen.getByText("Auto close")).toBeTruthy();

        act(() => {
            vi.advanceTimersByTime(4600);
        });

        expect(screen.queryByText("Auto close")).toBeNull();
    });
});
