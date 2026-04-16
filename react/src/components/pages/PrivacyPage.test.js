// @vitest-environment jsdom

import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import PrivacyPage from "./PrivacyPage";

const {
    useSelectorMock,
    getUserDocMock,
    getDocMock,
    getDocsMock,
    collectionMock,
    docMock,
    queryMock,
    whereMock,
    notifyErrorMock,
} = vi.hoisted(() => {
    return {
        useSelectorMock: vi.fn(),
        getUserDocMock: vi.fn(),
        getDocMock: vi.fn(),
        getDocsMock: vi.fn(),
        collectionMock: vi.fn((dbArg, name) => ({ kind: "collection", name })),
        docMock: vi.fn((dbArg, name, id) => ({ kind: "doc", name, id })),
        queryMock: vi.fn((base, ...clauses) => ({ kind: "query", base, clauses })),
        whereMock: vi.fn((field, op, value) => ({ field, op, value })),
        notifyErrorMock: vi.fn(),
    };
});

vi.mock("react-redux", () => {
    return {
        useSelector: useSelectorMock,
    };
});

vi.mock("firebase/firestore", () => {
    return {
        collection: collectionMock,
        doc: docMock,
        getDoc: getDocMock,
        getDocs: getDocsMock,
        query: queryMock,
        where: whereMock,
    };
});

vi.mock("../../firebase", () => {
    return {
        db: { name: "mock-db" },
    };
});

vi.mock("../../dbtools/getUserDoc", () => {
    return {
        default: getUserDocMock,
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

function createSnapshot(docs) {
    return {
        docs,
    };
}

function createDocSnapshot(id, data) {
    return {
        id,
        data: () => data,
    };
}

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
        getUserDocMock.mockReset();
        getDocMock.mockReset();
        getDocsMock.mockReset();
        notifyErrorMock.mockReset();
        collectionMock.mockClear();
        docMock.mockClear();
        queryMock.mockClear();
        whereMock.mockClear();

        useSelectorMock.mockImplementation((selector) => selector({
            auth: {
                uid: "user-123",
                loggedIn: true,
            },
        }));

        console.error = vi.fn();

        getUserDocMock.mockResolvedValue({
            data: () => ({
                uid: "user-123",
                email: "user@example.com",
                notificationEmailEnabled: true,
                votesVisible: true,
            }),
        });

        getDocMock.mockResolvedValue({
            exists: () => true,
            data: () => ({
                uid: "user-123",
                name: "Preferred User",
                votesVisible: false,
            }),
        });

        getDocsMock.mockImplementation(async (target) => {
            if (target.kind === "collection" && target.name === "votes") {
                return createSnapshot([
                    createDocSnapshot("poll-1", {
                        venueA: ["user-123"],
                        any: ["user-123"],
                        venueB: ["someone-else"],
                    }),
                ]);
            }

            if (target.kind === "collection" && target.name === "attendance") {
                return createSnapshot([
                    createDocSnapshot("poll-1", {
                        venueA: { canCome: ["user-123"], cannotCome: [] },
                        venueB: { canCome: [], cannotCome: ["user-123"] },
                        any: { canCome: ["user-123"], cannotCome: [] },
                    }),
                ]);
            }

            if (target.kind === "collection" && target.name === "polls") {
                return createSnapshot([
                    createDocSnapshot("poll-1", {
                        date: "2026-04-04",
                        completed: false,
                        pubs: {
                            venueA: { name: "Venue A" },
                            venueB: { name: "Venue B" },
                        },
                    }),
                ]);
            }

            if (target.kind === "collection" && target.name === "pubs") {
                return createSnapshot([
                    createDocSnapshot("venueA", { name: "Venue A", address: "1 Main St" }),
                    createDocSnapshot("venueB", { name: "Venue B", address: "2 High St" }),
                ]);
            }

            if (target.kind === "query" && target.base?.name === "messages") {
                return createSnapshot([
                    createDocSnapshot("message-1", {
                        uid: "user-123",
                        text: "Hello world",
                        name: "Preferred User",
                        createdAt: {
                            toDate: () => new Date("2026-04-13T14:00:00.000Z"),
                        },
                    }),
                ]);
            }

            return createSnapshot([]);
        });

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

    it("downloads a JSON export with raw, expanded, lookup, and messages data", async () => {
        render(<PrivacyPage />);

        fireEvent.click(screen.getByText("Download My Data (JSON)"));

        await waitFor(() => {
            expect(URL.createObjectURL).toHaveBeenCalled();
            expect(anchorRecord?.click).toHaveBeenCalled();
        });

        await waitFor(() => {
            expect(downloadedJsonText).toBeTruthy();
        });

        const payload = JSON.parse(downloadedJsonText);

        expect(payload.userId).toBe("user-123");
        expect(payload.raw.profile.private.email).toBe("user@example.com");
        expect(payload.raw.votes["poll-1"]).toEqual({ venueA: true, any: true });
        expect(payload.raw.attendance["poll-1"].venueA).toEqual({ canCome: true, cannotCome: false });
        expect(payload.raw.messages).toHaveLength(1);
        expect(payload.raw.messages[0].text).toBe("Hello world");

        expect(payload.lookup.polls["poll-1"].date).toBe("2026-04-04");
        expect(payload.lookup.venues.venueA.name).toBe("Venue A");
        expect(payload.lookup.venues.any.name).toBe("Any venue");

        expect(payload.expanded.profile.preferredName).toBe("Preferred User");
        expect(payload.expanded.profile.notificationPreferences.votesVisible).toBe(false);
        expect(payload.expanded.votes["poll-1"].event.pollId).toBe("poll-1");
        expect(payload.expanded.votes["poll-1"].votes[0].venue.name).toBe("Venue A");
        expect(payload.expanded.attendance["poll-1"].attendance[1].venue.name).toBe("Venue B");
        expect(payload.expanded.messages[0].sentAt).toBe("2026-04-13T14:00:00.000Z");

        expect(anchorRecord?.download).toBe("pub-night-picker-data-user-123.json");
        expect(anchorRecord?.href).toBe("blob:privacy-export");
    });

    it("reports export failures via notifyError", async () => {
        getDocsMock.mockRejectedValueOnce(new Error("votes unavailable"));

        render(<PrivacyPage />);

        fireEvent.click(screen.getByText("Download My Data (JSON)"));

        await waitFor(() => {
            expect(notifyErrorMock).toHaveBeenCalledWith("votes unavailable");
        });
    });
});
