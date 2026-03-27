import { afterEach, describe, expect, it, vi } from "vitest";
import { NOTIFY_EVENT_NAME, notifyError, notifyInfo } from "./notify";

describe("notify utility", () => {
    const originalWindow = globalThis.window;
    const originalCustomEvent = globalThis.CustomEvent;

    afterEach(() => {
        globalThis.window = originalWindow;
        globalThis.CustomEvent = originalCustomEvent;
        vi.restoreAllMocks();
    });

    it("dispatches info and error notifications as window events", () => {
        const dispatchEvent = vi.fn();

        globalThis.window = { dispatchEvent };
        globalThis.CustomEvent = class {
            constructor(name, init) {
                this.type = name;
                this.detail = init?.detail;
            }
        };

        notifyInfo("hello");
        notifyError("boom");

        expect(dispatchEvent).toHaveBeenCalledTimes(2);

        const infoEvent = dispatchEvent.mock.calls[0][0];
        expect(infoEvent.type).toBe(NOTIFY_EVENT_NAME);
        expect(infoEvent.detail.level).toBe("info");
        expect(infoEvent.detail.message).toBe("hello");

        const errorEvent = dispatchEvent.mock.calls[1][0];
        expect(errorEvent.type).toBe(NOTIFY_EVENT_NAME);
        expect(errorEvent.detail.level).toBe("error");
        expect(errorEvent.detail.message).toBe("boom");
    });

    it("falls back to console logging when window events are unavailable", () => {
        const errorSpy = vi.spyOn(console, "error").mockImplementation(() => undefined);
        const logSpy = vi.spyOn(console, "log").mockImplementation(() => undefined);

        globalThis.window = undefined;
        globalThis.CustomEvent = undefined;

        notifyInfo("plain info");
        notifyError("plain error");

        expect(logSpy).toHaveBeenCalledWith("plain info");
        expect(errorSpy).toHaveBeenCalledWith("plain error");
    });
});
