import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const {
    notifyErrorMock,
} = vi.hoisted(() => {
    return {
        notifyErrorMock: vi.fn(),
    };
});

vi.mock("./notify", () => {
    return {
        notifyError: notifyErrorMock,
    };
});

import { createFirestoreSnapshotErrorHandler } from "./firestoreErrors";

describe("createFirestoreSnapshotErrorHandler", () => {
    beforeEach(() => {
        notifyErrorMock.mockReset();
        vi.useFakeTimers();
        vi.setSystemTime(new Date("2026-03-27T16:45:00Z"));
    });

    afterEach(() => {
        vi.useRealTimers();
    });

    it("maps unavailable errors to a friendly message", () => {
        const handler = createFirestoreSnapshotErrorHandler("Votes");

        handler({ code: "unavailable", message: "listen failed" });

        expect(notifyErrorMock).toHaveBeenCalledWith(
            "Votes: Cannot reach Firestore. Check the emulator or network connection.",
        );
    });

    it("throttles duplicate errors within cooldown window", () => {
        const handler = createFirestoreSnapshotErrorHandler("Attendance");

        handler({ code: "permission-denied", message: "nope" });
        handler({ code: "permission-denied", message: "nope" });

        expect(notifyErrorMock).toHaveBeenCalledTimes(1);

        vi.advanceTimersByTime(15001);
        handler({ code: "permission-denied", message: "nope" });

        expect(notifyErrorMock).toHaveBeenCalledTimes(2);
    });

    it("uses fallback message for unknown errors", () => {
        const handler = createFirestoreSnapshotErrorHandler("Polls data");

        handler({});

        expect(notifyErrorMock).toHaveBeenCalledWith(
            "Polls data: Unable to load live updates from Firestore.",
        );
    });
});
