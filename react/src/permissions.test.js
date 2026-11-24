import { describe, expect, it } from "vitest";
import {
    PermissionError,
    ADMIN_DEFAULT_PERMISSIONS,
    CONSOLIDATED_PERMISSION_COLUMNS,
    PERMISSIONS,
    getUserFacingErrorMessage,
    hasPermissionInRoles,
} from "./permissions";

describe("permissions helpers", () => {
    it("exposes the backend voting permission to administrators", () => {
        expect(PERMISSIONS.canVote).toBe("canVote");
        expect(CONSOLIDATED_PERMISSION_COLUMNS).toContainEqual({ key: "canVote", label: "Can Vote" });
        expect(ADMIN_DEFAULT_PERMISSIONS).toContain("canVote");
    });

    it("returns false when roles or uid are missing", () => {
        expect(hasPermissionInRoles(null, "canChat", "user-1")).toBe(false);
        expect(hasPermissionInRoles({}, "canChat", "")).toBe(false);
    });

    it("supports boolean and object-backed role values", () => {
        expect(hasPermissionInRoles({ canChat: true }, "canChat", "user-1")).toBe(true);
        expect(
            hasPermissionInRoles({ canChat: { "user-1": true } }, "canChat", "user-1"),
        ).toBe(true);
        expect(
            hasPermissionInRoles({ canChat: { "user-2": true } }, "canChat", "user-1"),
        ).toBe(false);
    });

    it("formats permission errors into a clean user-facing message", () => {
        const error = new PermissionError("canManagePubs", "deleting a pub");
        expect(getUserFacingErrorMessage(error)).toBe(
            "You do not have permission for deleting a pub.",
        );
    });

    it("falls back to the original error message for non-permission errors", () => {
        expect(getUserFacingErrorMessage(new Error("Boom"))).toBe("Boom");
        expect(getUserFacingErrorMessage(null, "Fallback message")).toBe("Fallback message");
    });
});
