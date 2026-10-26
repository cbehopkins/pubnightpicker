import { beforeEach, describe, expect, it, vi } from "vitest";

const {
    assertCurrentUserPermissionMock,
    addDocMock,
    collectionMock,
    deleteDocMock,
    docMock,
    updateDocMock,
} = vi.hoisted(() => {
    return {
        assertCurrentUserPermissionMock: vi.fn(),
        addDocMock: vi.fn(async () => undefined),
        collectionMock: vi.fn(() => ({ id: "pubs-collection" })),
        deleteDocMock: vi.fn(async () => undefined),
        docMock: vi.fn(() => ({ id: "pub-doc" })),
        updateDocMock: vi.fn(async () => undefined),
    };
});

vi.mock("../permissions", () => {
    return {
        assertCurrentUserPermission: assertCurrentUserPermissionMock,
        PERMISSIONS: {
            canManagePubs: "canManagePubs",
        },
    };
});

vi.mock("../firebase", () => {
    return {
        db: {},
    };
});

vi.mock("firebase/firestore", () => {
    return {
        addDoc: addDocMock,
        collection: collectionMock,
        deleteDoc: deleteDocMock,
        doc: docMock,
        updateDoc: updateDocMock,
    };
});

import { addNewPub, deletePub, modifyPub } from "./pubs";

describe("pub dbtools permission guards", () => {
    beforeEach(() => {
        assertCurrentUserPermissionMock.mockReset();
        addDocMock.mockClear();
        collectionMock.mockClear();
        deleteDocMock.mockClear();
        docMock.mockClear();
        updateDocMock.mockClear();
    });

    it("guards addNewPub with canManagePubs", async () => {
        await addNewPub({ name: "The Test Arms" });

        expect(assertCurrentUserPermissionMock).toHaveBeenCalledWith(
            "canManagePubs",
            "creating a pub",
        );
        expect(addDocMock).toHaveBeenCalledTimes(1);
        expect(collectionMock).toHaveBeenCalledTimes(1);
    });

    it("guards modifyPub with canManagePubs", async () => {
        await modifyPub("pub-1", { name: "Updated Name" });

        expect(assertCurrentUserPermissionMock).toHaveBeenCalledWith(
            "canManagePubs",
            "editing a pub",
        );
        expect(updateDocMock).toHaveBeenCalledTimes(1);
        expect(docMock).toHaveBeenCalledWith({}, "pubs", "pub-1");
    });

    it("guards deletePub with canManagePubs", async () => {
        await deletePub("pub-2");

        expect(assertCurrentUserPermissionMock).toHaveBeenCalledWith(
            "canManagePubs",
            "deleting a pub",
        );
        expect(deleteDocMock).toHaveBeenCalledTimes(1);
        expect(docMock).toHaveBeenCalledWith({}, "pubs", "pub-2");
    });
});
