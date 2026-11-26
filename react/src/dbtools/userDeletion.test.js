import { beforeEach, describe, expect, it, vi } from "vitest";

const { collectionMock, deleteDocMock, getDocsMock, requestLastOrdersMock } = vi.hoisted(() => ({
    collectionMock: vi.fn((...parts) => parts),
    deleteDocMock: vi.fn(async () => { }),
    getDocsMock: vi.fn(async () => ({ docs: [] })),
    requestLastOrdersMock: vi.fn(async () => ({ updatedDocuments: 2 })),
}));

vi.mock("firebase/firestore", () => ({
    collection: collectionMock,
    deleteDoc: deleteDocMock,
    deleteField: vi.fn(),
    doc: vi.fn((...parts) => parts),
    getDocs: getDocsMock,
    query: vi.fn((reference) => reference),
    serverTimestamp: vi.fn(),
    setDoc: vi.fn(),
    updateDoc: vi.fn(),
    where: vi.fn(),
}));
vi.mock("../firebase", () => ({ db: "database" }));
vi.mock("../utils/lastOrdersApi", () => ({ requestLastOrders: requestLastOrdersMock }));

import { deleteUserAppData, deletionSummaryLines, summarizeDeletionResult } from "./userDeletion";

describe("account attendance cleanup", () => {
    beforeEach(() => {
        vi.clearAllMocks();
        requestLastOrdersMock.mockReset();
        requestLastOrdersMock.mockResolvedValue({ updatedDocuments: 2 });
    });

    it("waits for the self-service backend cleanup and reports its count", async () => {
        const result = await deleteUserAppData("user");
        expect(requestLastOrdersMock).toHaveBeenCalledWith("api/account/attendance/cleanup", { method: "POST" });
        expect(collectionMock.mock.calls.some((parts) => parts.includes("attendance"))).toBe(false);
        expect(deletionSummaryLines(summarizeDeletionResult(result))).toContain("Attendance docs updated: 2");
    });

    it("marks admin attendance cleanup as queued without accessing it directly", async () => {
        const result = await deleteUserAppData("user", { removeRoles: true });
        expect(requestLastOrdersMock).not.toHaveBeenCalled();
        expect(collectionMock.mock.calls.some((parts) => parts.includes("attendance"))).toBe(false);
        expect(deletionSummaryLines(summarizeDeletionResult(result))).toContain("Attendance cleanup: queued for trusted backend");
    });

    it("does not delete profiles when backend attendance cleanup fails", async () => {
        requestLastOrdersMock.mockImplementation(async (path) => {
            if (path === "api/account/attendance/cleanup") throw new Error("unavailable");
            return { updatedDocuments: 0 };
        });
        await expect(deleteUserAppData("user")).rejects.toThrow("unavailable");
        expect(deleteDocMock).not.toHaveBeenCalled();
    });
});
