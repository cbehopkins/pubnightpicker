// @vitest-environment jsdom

import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import PrivacyPage from "./PrivacyPage";

const {
    useSelectorMock,
    requestLastOrdersMock,
    notifyErrorMock,
} = vi.hoisted(() => {
    return {
        useSelectorMock: vi.fn(),
        requestLastOrdersMock: vi.fn(),
        notifyErrorMock: vi.fn(),
    };
});

vi.mock("react-redux", () => {
    return {
        useSelector: useSelectorMock,
    };
});

vi.mock("../../utils/lastOrdersApi", () => {
    return {
        requestLastOrders: requestLastOrdersMock,
    };
});

vi.mock("../../utils/notify", () => {
    return {
        notifyError: notifyErrorMock,
    };
});

vi.mock("../../../docs/privacy-notice.md?raw", () => {
    return {
        default: "# Privacy Notice\n\nExample content",
    };
});

describe("PrivacyPage", () => {
    const originalCreateObjectURL = URL.createObjectURL;
    const originalRevokeObjectURL = URL.revokeObjectURL;
    const originalCreateElement = document.createElement.bind(document);
    const originalAppendChild = document.body.appendChild.bind(document.body);
    const originalConsoleError = console.error;

    /** @type {{ click: ReturnType<typeof vi.fn>, remove: ReturnType<typeof vi.fn>, href: string, download: string } | null} */
    let anchorRecord = null;
    /** @type {string | null} */
    let downloadedJsonText = null;

    beforeEach(() => {
        anchorRecord = null;
        downloadedJsonText = null;

        useSelectorMock.mockReset();
        requestLastOrdersMock.mockReset();
        notifyErrorMock.mockReset();

        useSelectorMock.mockImplementation((selector) => selector({
            auth: {
                uid: "user-123",
                loggedIn: true,
            },
        }));

        console.error = vi.fn();

        URL.createObjectURL = vi.fn((blob) => {
            blob.text().then((text) => {
                downloadedJsonText = text;
            });
            return "blob:privacy-export";
        });
        URL.revokeObjectURL = vi.fn();

        document.body.appendChild = vi.fn((element) => originalAppendChild(element));
        document.createElement = vi.fn((tagName) => {
            const element = originalCreateElement(tagName);
            if (tagName === "a") {
                const click = vi.fn();
                const remove = vi.fn();
                element.click = click;
                element.remove = remove;
                anchorRecord = {
                    click,
                    remove,
                    href: "",
                    download: "",
                };
                Object.defineProperty(element, "href", {
                    get() {
                        return anchorRecord?.href || "";
                    },
                    set(value) {
                        if (anchorRecord) {
                            anchorRecord.href = value;
                        }
                    },
                });
                Object.defineProperty(element, "download", {
                    get() {
                        return anchorRecord?.download || "";
                    },
                    set(value) {
                        if (anchorRecord) {
                            anchorRecord.download = value;
                        }
                    },
                });
            }
            return element;
        });
    });

    afterEach(() => {
        cleanup();
        URL.createObjectURL = originalCreateObjectURL;
        URL.revokeObjectURL = originalRevokeObjectURL;
        document.createElement = originalCreateElement;
        document.body.appendChild = originalAppendChild;
        console.error = originalConsoleError;
    });

    it("shows a sign-in message when the user is logged out", () => {
        useSelectorMock.mockImplementation((selector) => selector({
            auth: {
                uid: null,
                loggedIn: false,
            },
        }));

        render(<PrivacyPage />);

        expect(screen.getByText("Sign in to export the data we hold about your account.")).toBeTruthy();
        expect(screen.queryByText("Download My Data (JSON)")).toBeNull();
    });

    it("downloads the complete account export returned by Last Orders", async () => {
        const exportPayload = {
            exported_at: "2026-04-13T14:00:00.000Z",
            user_id: "user-123",
            notice: { scope: "Account data" },
            account: { name: "Preferred User" },
            roles: [],
            groups: [],
            votes: [{ meet_id: "meet-1", venue_id: "venueA" }],
            attendance_responses: [],
            chat_messages: [{ text: "Hello world" }],
            administrative_activity: [],
            notification_activity: [],
            notification_subscriptions: [],
        };
        requestLastOrdersMock.mockResolvedValueOnce(exportPayload);

        render(<PrivacyPage />);

        fireEvent.click(screen.getByText("Download My Data (JSON)"));

        await waitFor(() => {
            expect(URL.createObjectURL).toHaveBeenCalled();
            expect(anchorRecord?.click).toHaveBeenCalled();
        });

        await waitFor(() => {
            expect(downloadedJsonText).toBeTruthy();
        });

        expect(JSON.parse(downloadedJsonText)).toEqual(exportPayload);
        expect(anchorRecord?.download).toBe("pub-night-picker-data.json");
        expect(anchorRecord?.href).toBe("blob:privacy-export");
        expect(anchorRecord?.remove).toHaveBeenCalled();
        expect(URL.revokeObjectURL).toHaveBeenCalledWith("blob:privacy-export");
        expect(requestLastOrdersMock).toHaveBeenCalledOnce();
        expect(requestLastOrdersMock).toHaveBeenCalledWith("/api/account/export", { method: "GET" });
    });

    it("does not download an invalid account export", async () => {
        requestLastOrdersMock.mockResolvedValueOnce({ votes: [] });

        render(<PrivacyPage />);

        fireEvent.click(screen.getByText("Download My Data (JSON)"));

        await waitFor(() => {
            expect(notifyErrorMock).toHaveBeenCalledWith("Last Orders returned an unexpected account export.");
        });

        expect(URL.createObjectURL).not.toHaveBeenCalled();
        expect(anchorRecord?.click).toBeUndefined();
    });

    it("reports export failures via notifyError", async () => {
        requestLastOrdersMock.mockRejectedValueOnce(new Error("votes unavailable"));

        render(<PrivacyPage />);

        fireEvent.click(screen.getByText("Download My Data (JSON)"));

        await waitFor(() => {
            expect(notifyErrorMock).toHaveBeenCalledWith("votes unavailable");
        });
        expect(URL.createObjectURL).not.toHaveBeenCalled();
        expect(anchorRecord?.click).toBeUndefined();
        expect(requestLastOrdersMock).toHaveBeenCalledWith("/api/account/export", { method: "GET" });
    });
});
