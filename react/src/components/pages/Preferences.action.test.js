// @vitest-environment jsdom

import { beforeEach, describe, expect, it, vi } from "vitest";

const {
    getStateMock,
    updateDocMock,
    setDocMock,
    firestoreDocMock,
    notifyErrorMock,
} = vi.hoisted(() => {
    return {
        getStateMock: vi.fn(),
        updateDocMock: vi.fn(async () => undefined),
        setDocMock: vi.fn(async () => undefined),
        firestoreDocMock: vi.fn((...parts) => ({ path: parts.slice(1).join("/") })),
        notifyErrorMock: vi.fn(),
    };
});

vi.mock("../../store", () => {
    return {
        store: {
            getState: getStateMock,
        },
    };
});

vi.mock("../../firebase", () => {
    return {
        db: {},
    };
});

vi.mock("firebase/firestore", () => {
    return {
        setDoc: setDocMock,
        updateDoc: updateDocMock,
        doc: firestoreDocMock,
    };
});

vi.mock("../../utils/notify", () => {
    return {
        notifyError: notifyErrorMock,
    };
});

import { action } from "./Preferences";

function createRequest(formValues) {
    return new Request("http://localhost/preferences", {
        method: "POST",
        body: new URLSearchParams(formValues),
    });
}

describe("Preferences action push preferences writes", () => {
    beforeEach(() => {
        getStateMock.mockReset();
        updateDocMock.mockReset();
        setDocMock.mockReset();
        firestoreDocMock.mockClear();
        notifyErrorMock.mockReset();

        getStateMock.mockReturnValue({
            auth: {
                loggedIn: true,
                uid: "user-1",
                photoUrl: "https://example.com/avatar.png",
            },
        });
    });

    it("writes pushPreferences when push_prefs_visible is submitted", async () => {
        const response = await action({
            request: createRequest({
                name: "Alice",
                email: "alice@example.com",
                avatar: "https://example.com/avatar.png",
                default_arrival_time: "18:45",
                push_prefs_visible: "1",
                push_poll_opens: "on",
                push_poll_completes: "on",
                push_global_chat: "on",
            }),
            params: {},
        });

        expect(updateDocMock).toHaveBeenCalledTimes(1);
        expect(updateDocMock).toHaveBeenCalledWith(
            expect.anything(),
            expect.objectContaining({
                defaultArrivalTime: "18:45",
                pushPreferences: {
                    pollOpens: true,
                    pollCompletes: true,
                    globalChat: true,
                    eventChat: false,
                },
            })
        );
        expect(response.status).toBe(302);
        expect(response.headers.get("Location")).toBe("/");
    });

    it("does not write pushPreferences when push_prefs_visible is absent", async () => {
        await action({
            request: createRequest({
                name: "Alice",
                email: "alice@example.com",
                avatar: "https://example.com/avatar.png",
            }),
            params: {},
        });

        expect(updateDocMock).toHaveBeenCalledTimes(1);
        const privateWritePayload = updateDocMock.mock.calls[0][1];
        expect(privateWritePayload.defaultArrivalTime).toBe("19:30");
        expect("pushPreferences" in privateWritePayload).toBe(false);
    });

    it("normalizes invalid default_arrival_time to 19:30", async () => {
        await action({
            request: createRequest({
                name: "Alice",
                email: "alice@example.com",
                avatar: "https://example.com/avatar.png",
                default_arrival_time: "invalid",
            }),
            params: {},
        });

        expect(updateDocMock).toHaveBeenCalledTimes(1);
        expect(updateDocMock.mock.calls[0][1].defaultArrivalTime).toBe("19:30");
    });
});
