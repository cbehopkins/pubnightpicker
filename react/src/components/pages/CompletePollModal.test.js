// @vitest-environment jsdom

import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import CompletePollModal from "./CompletePollModal";

const { useNotificationPingMock } = vi.hoisted(() => {
    return {
        useNotificationPingMock: vi.fn(),
    };
});

vi.mock("../../hooks/useNotificationPing", () => {
    return {
        useNotificationPing: useNotificationPingMock,
    };
});

vi.mock("./VenueAssignmentModal", () => {
    return {
        default: ({ title, footerStatusNode, onConfirm, onCancel }) => {
            return (
                <div>
                    <h1>{title}</h1>
                    <div data-testid="footer-status">{footerStatusNode}</div>
                    <button type="button" onClick={onConfirm}>Confirm</button>
                    <button type="button" onClick={onCancel}>Cancel</button>
                </div>
            );
        },
    };
});

function createProps(overrides = {}) {
    return {
        pollId: "poll-123",
        pubName: "The Maypole",
        pubHasFood: false,
        availableRestaurants: [],
        restaurantSource: "system",
        chosenRestaurantId: "",
        restaurantTime: "",
        onRestaurantChange: vi.fn(),
        onRestaurantTimeChange: vi.fn(),
        onConfirm: vi.fn(async () => undefined),
        onCancel: vi.fn(async () => undefined),
        ...overrides,
    };
}

describe("CompletePollModal", () => {
    beforeEach(() => {
        useNotificationPingMock.mockReset();
        vi.restoreAllMocks();
    });

    afterEach(() => {
        cleanup();
    });

    it("runs the complete ping handshake when mounted", async () => {
        const runPing = vi.fn(async () => ({ acknowledged: true, pingValue: 1 }));
        useNotificationPingMock.mockReturnValue({ status: "checking", runPing });

        render(<CompletePollModal {...createProps()} />);

        await waitFor(() => {
            expect(runPing).toHaveBeenCalledTimes(1);
        });
        expect(useNotificationPingMock).toHaveBeenCalledWith("poll-123", "complete", 60000);
    });

    it("confirms without browser warning when handshake status is ok", async () => {
        const runPing = vi.fn(async () => ({ acknowledged: true, pingValue: 1 }));
        const onConfirm = vi.fn(async () => undefined);
        const confirmSpy = vi.spyOn(window, "confirm");

        useNotificationPingMock.mockReturnValue({ status: "ok", runPing });

        render(<CompletePollModal {...createProps({ pubHasFood: true, onConfirm })} />);

        fireEvent.click(screen.getByRole("button", { name: "Confirm" }));

        await waitFor(() => {
            expect(onConfirm).toHaveBeenCalledTimes(1);
        });
        expect(confirmSpy).not.toHaveBeenCalled();
    });

    it("warns and blocks completion when handshake is not ok and user cancels", async () => {
        const runPing = vi.fn(async () => ({ acknowledged: false, timedOut: true, pingValue: 1 }));
        const onConfirm = vi.fn(async () => undefined);
        vi.spyOn(window, "confirm").mockReturnValue(false);

        useNotificationPingMock.mockReturnValue({ status: "timeout", runPing });

        render(<CompletePollModal {...createProps({ onConfirm })} />);

        fireEvent.click(screen.getByRole("button", { name: "Confirm" }));

        await waitFor(() => {
            expect(window.confirm).toHaveBeenCalledTimes(1);
        });
        expect(onConfirm).not.toHaveBeenCalled();
    });

    it("warns and allows completion when handshake is not ok and user proceeds", async () => {
        const runPing = vi.fn(async () => ({ acknowledged: false, timedOut: true, pingValue: 1 }));
        const onConfirm = vi.fn(async () => undefined);
        vi.spyOn(window, "confirm").mockReturnValue(true);

        useNotificationPingMock.mockReturnValue({ status: "error", runPing });

        render(<CompletePollModal {...createProps({ pubHasFood: true, onConfirm })} />);

        fireEvent.click(screen.getByRole("button", { name: "Confirm" }));

        await waitFor(() => {
            expect(window.confirm).toHaveBeenCalledTimes(1);
            expect(onConfirm).toHaveBeenCalledTimes(1);
        });
    });

    it("asks for explicit confirmation when pub has no food and no restaurant is selected", async () => {
        const runPing = vi.fn(async () => ({ acknowledged: true, pingValue: 1 }));
        const onConfirm = vi.fn(async () => undefined);
        const confirmSpy = vi.spyOn(window, "confirm").mockReturnValue(true);

        useNotificationPingMock.mockReturnValue({ status: "ok", runPing });

        render(
            <CompletePollModal
                {...createProps({
                    pubHasFood: false,
                    chosenRestaurantId: "",
                    onConfirm,
                })}
            />
        );

        fireEvent.click(screen.getByRole("button", { name: "Confirm" }));

        await waitFor(() => {
            expect(confirmSpy).toHaveBeenCalledTimes(1);
        });
        expect(onConfirm).toHaveBeenCalledTimes(1);
    });

    it("asks both warnings in order when handshake is not ok and no food plan is selected", async () => {
        const runPing = vi.fn(async () => ({ acknowledged: false, timedOut: true, pingValue: 1 }));
        const onConfirm = vi.fn(async () => undefined);
        const confirmSpy = vi.spyOn(window, "confirm")
            .mockReturnValueOnce(true)
            .mockReturnValueOnce(false);

        useNotificationPingMock.mockReturnValue({ status: "timeout", runPing });

        render(
            <CompletePollModal
                {...createProps({
                    pubHasFood: false,
                    chosenRestaurantId: "",
                    onConfirm,
                })}
            />
        );

        fireEvent.click(screen.getByRole("button", { name: "Confirm" }));

        await waitFor(() => {
            expect(confirmSpy).toHaveBeenCalledTimes(2);
        });

        expect(confirmSpy).toHaveBeenNthCalledWith(
            1,
            "Notification tool has not acknowledged the completion handshake yet. Completing now may skip notification processing. Do you want to continue?"
        );
        expect(confirmSpy).toHaveBeenNthCalledWith(
            2,
            "This venue does not serve food and no restaurant is selected. Do you want to complete this poll without a food plan?"
        );
        expect(onConfirm).not.toHaveBeenCalled();
    });

    it("renders the notification status badge in modal footer", () => {
        const runPing = vi.fn(async () => ({ acknowledged: true, pingValue: 1 }));
        useNotificationPingMock.mockReturnValue({ status: "checking", runPing });

        render(<CompletePollModal {...createProps()} />);

        expect(screen.getByTestId("footer-status").textContent).toContain("Notification Tool: Checking...");
    });
});
