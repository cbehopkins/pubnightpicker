// @vitest-environment jsdom

import { describe, expect, it, vi } from "vitest";

const {
    docMock,
    onSnapshotMock,
    unsubscribeMock,
} = vi.hoisted(() => ({
    docMock: vi.fn(),
    onSnapshotMock: vi.fn(),
    unsubscribeMock: vi.fn(),
}));

vi.mock("firebase/firestore", () => ({
    deleteField: vi.fn(),
    doc: docMock,
    getDoc: vi.fn(),
    onSnapshot: onSnapshotMock,
    setDoc: vi.fn(),
    updateDoc: vi.fn(),
}));

vi.mock("../firebase", () => ({
    db: {},
}));

import { waitForNotificationAck } from "./notificationPings";

describe("waitForNotificationAck", () => {
    it("resolves when a synchronous snapshot already contains the expected value", async () => {
        docMock.mockReturnValue({ path: "notification_ack/diagnostics" });
        onSnapshotMock.mockImplementation((_docRef, onNext) => {
            onNext({
                data: () => ({ manual: 12345 }),
            });
            return unsubscribeMock;
        });

        await expect(
            waitForNotificationAck("diagnostics", "manual", 12345, 5000),
        ).resolves.toEqual({
            acknowledged: true,
            timedOut: false,
        });

        expect(unsubscribeMock).toHaveBeenCalledTimes(1);
    });
});
