// @vitest-environment jsdom

import { cleanup, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import NotificationPingStatus from "./NotificationPingStatus";

const { pingNotificationToolMock } = vi.hoisted(() => {
    return {
        pingNotificationToolMock: vi.fn(),
    };
});

vi.mock("../../dbtools/notificationPings", () => {
    return {
        pingNotificationTool: pingNotificationToolMock,
        clearNotificationPing: vi.fn(),
    };
});

describe("NotificationPingStatus", () => {
    beforeEach(() => {
        pingNotificationToolMock.mockReset();
    });

    afterEach(() => {
        cleanup();
    });

    it("stays hidden while the ping is checking", async () => {
        pingNotificationToolMock.mockImplementation(() => new Promise(() => { }));

        render(
            <NotificationPingStatus
                documentId="poll-checking"
                eventKey="create"
                timeoutMs={60000}
            />
        );

        await waitFor(() => {
            expect(pingNotificationToolMock).toHaveBeenCalledTimes(1);
        });

        expect(screen.queryByText(/Notification Tool:/)).toBeNull();
    });

    it("stays hidden after an acknowledged ping", async () => {
        pingNotificationToolMock.mockResolvedValue({
            acknowledged: true,
            timedOut: false,
            pingValue: 123,
        });

        render(
            <NotificationPingStatus
                documentId="poll-ok"
                eventKey="create"
                timeoutMs={60000}
            />
        );

        await waitFor(() => {
            expect(pingNotificationToolMock).toHaveBeenCalledTimes(1);
        });

        await waitFor(() => {
            expect(screen.queryByText(/Notification Tool:/)).toBeNull();
        });
    });

    it.each([
        [
            "timeout",
            () => Promise.resolve({ acknowledged: false, timedOut: true, pingValue: 222 }),
            "Notification Tool: Timeout",
        ],
        [
            "error",
            () => Promise.reject(new Error("network down")),
            "Notification Tool: Error",
        ],
    ])("renders the %s badge when the ping fails", async (_status, createResult, label) => {
        pingNotificationToolMock.mockImplementation(createResult);

        render(
            <NotificationPingStatus
                documentId={`poll-${label}`}
                eventKey="create"
                timeoutMs={60000}
            />
        );

        await waitFor(() => {
            expect(screen.getByText(label)).toBeTruthy();
        });
    });

    it("does not re-run the ping on remount after acknowledgement", async () => {
        pingNotificationToolMock.mockResolvedValue({
            acknowledged: true,
            timedOut: false,
            pingValue: 777,
        });

        const props = {
            documentId: "poll-cache",
            eventKey: "create",
            timeoutMs: 60000,
        };

        const { unmount } = render(<NotificationPingStatus {...props} />);

        await waitFor(() => {
            expect(pingNotificationToolMock).toHaveBeenCalledTimes(1);
        });

        await waitFor(() => {
            expect(screen.queryByText(/Notification Tool:/)).toBeNull();
        });

        unmount();

        render(<NotificationPingStatus {...props} />);

        expect(pingNotificationToolMock).toHaveBeenCalledTimes(1);
        expect(screen.queryByText(/Notification Tool:/)).toBeNull();
    });
});
