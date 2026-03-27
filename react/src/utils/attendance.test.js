import { beforeEach, describe, expect, it, vi } from "vitest";

const {
    getUserFacingErrorMessageMock,
    notifyErrorMock,
} = vi.hoisted(() => {
    return {
        getUserFacingErrorMessageMock: vi.fn(),
        notifyErrorMock: vi.fn(),
    };
});

vi.mock("../permissions", () => {
    return {
        getUserFacingErrorMessage: getUserFacingErrorMessageMock,
    };
});

vi.mock("./notify", () => {
    return {
        notifyError: notifyErrorMock,
    };
});

import { runAttendanceAction } from "./attendance";

describe("runAttendanceAction", () => {
    beforeEach(() => {
        getUserFacingErrorMessageMock.mockReset();
        notifyErrorMock.mockReset();
    });

    it("runs action without notifying on success", async () => {
        const actionMock = vi.fn(async () => undefined);

        await runAttendanceAction(actionMock);

        expect(actionMock).toHaveBeenCalledTimes(1);
        expect(getUserFacingErrorMessageMock).not.toHaveBeenCalled();
        expect(notifyErrorMock).not.toHaveBeenCalled();
    });

    it("maps and notifies errors with default fallback", async () => {
        const testError = new Error("boom");
        const actionMock = vi.fn(async () => {
            throw testError;
        });
        getUserFacingErrorMessageMock.mockReturnValue("Mapped message");

        await runAttendanceAction(actionMock);

        expect(getUserFacingErrorMessageMock).toHaveBeenCalledWith(
            testError,
            "Unable to update your attendance.",
        );
        expect(notifyErrorMock).toHaveBeenCalledWith("Mapped message");
    });

    it("uses custom fallback when provided", async () => {
        const testError = new Error("nope");
        const actionMock = vi.fn(async () => {
            throw testError;
        });
        getUserFacingErrorMessageMock.mockReturnValue("Custom mapped message");

        await runAttendanceAction(actionMock, "Unable to clear your attendance.");

        expect(getUserFacingErrorMessageMock).toHaveBeenCalledWith(
            testError,
            "Unable to clear your attendance.",
        );
        expect(notifyErrorMock).toHaveBeenCalledWith("Custom mapped message");
    });
});
