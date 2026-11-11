import { beforeEach, describe, expect, it, vi } from "vitest";

const {
    assertCurrentUserPermissionMock,
    requestLastOrdersMock,
} = vi.hoisted(() => {
    return {
        assertCurrentUserPermissionMock: vi.fn(),
        requestLastOrdersMock: vi.fn(async () => undefined),
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

vi.mock("../utils/lastOrdersApi", () => {
    return {
        requestLastOrders: requestLastOrdersMock,
    };
});

import { addNewPub, deletePub, modifyPub } from "./pubs";

describe("pub dbtools permission guards", () => {
    beforeEach(() => {
        assertCurrentUserPermissionMock.mockReset();
        requestLastOrdersMock.mockReset();
        requestLastOrdersMock.mockResolvedValue(undefined);
    });

    it("guards addNewPub with canManagePubs", async () => {
        await addNewPub({ name: "The Test Arms" });

        expect(assertCurrentUserPermissionMock).toHaveBeenCalledWith(
            "canManagePubs",
            "creating a pub",
        );
        expect(requestLastOrdersMock).toHaveBeenCalledWith("api/venues", {
            body: { name: "The Test Arms" },
        });
    });

    it("guards modifyPub with canManagePubs", async () => {
        await modifyPub("pub-1", { name: "Updated Name" });

        expect(assertCurrentUserPermissionMock).toHaveBeenCalledWith(
            "canManagePubs",
            "editing a pub",
        );
        expect(requestLastOrdersMock).toHaveBeenCalledWith("api/venues/pub-1", {
            method: "PATCH",
            contentType: "application/merge-patch+json",
            body: { name: "Updated Name" },
        });
    });

    it("guards deletePub with canManagePubs", async () => {
        await deletePub("pub-2");

        expect(assertCurrentUserPermissionMock).toHaveBeenCalledWith(
            "canManagePubs",
            "deleting a pub",
        );
        expect(requestLastOrdersMock).toHaveBeenCalledWith("api/venues/pub-2", {
            method: "DELETE",
        });
    });
});
