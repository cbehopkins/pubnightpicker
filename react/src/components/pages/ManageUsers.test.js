// @vitest-environment jsdom

import { render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import ManageUsers, { ManageUserDetail } from "./ManageUsers";

const {
    useUsersMock,
    useAllRolesMock,
    useIsMobileViewMock,
} = vi.hoisted(() => {
    return {
        useUsersMock: vi.fn(),
        useAllRolesMock: vi.fn(),
        useIsMobileViewMock: vi.fn(),
    };
});

vi.mock("../../hooks/useUsers", () => {
    return {
        default: useUsersMock,
    };
});

vi.mock("../../hooks/useRoles", () => {
    return {
        useAllRoles: useAllRolesMock,
    };
});

vi.mock("../../hooks/useIsMobileView", () => {
    return {
        useIsMobileView: useIsMobileViewMock,
    };
});

vi.mock("../ProtectedRoute", () => {
    return {
        default: (Component) => Component,
    };
});

vi.mock("../../firebase", () => {
    return {
        db: {},
    };
});

vi.mock("firebase/firestore", () => {
    return {
        doc: vi.fn(() => ({})),
        deleteField: vi.fn(() => "DELETE_FIELD"),
        updateDoc: vi.fn(async () => undefined),
        setDoc: vi.fn(async () => undefined),
    };
});

describe("ManageUsers responsive layout", () => {
    beforeEach(() => {
        useUsersMock.mockReset();
        useAllRolesMock.mockReset();
        useIsMobileViewMock.mockReset();

        useUsersMock.mockReturnValue({
            "uid-b": { name: "Bravo User", email: "bravo@example.com" },
            "uid-a": { name: "Alpha User", email: "alpha@example.com" },
        });

        useAllRolesMock.mockReturnValue({
            admin: { "uid-a": true },
            known: { "uid-b": true },
        });
    });

    it("shows an alphabetical name list on narrow screens", () => {
        useIsMobileViewMock.mockReturnValue(true);

        render(
            <MemoryRouter>
                <ManageUsers />
            </MemoryRouter>
        );

        const links = screen.getAllByRole("link");
        expect(links[0].textContent).toContain("Alpha User");
        expect(links[0].getAttribute("href")).toBe("/manage_users/uid-a");
        expect(links[1].textContent).toContain("Bravo User");
        expect(links[1].getAttribute("href")).toBe("/manage_users/uid-b");
        expect(useIsMobileViewMock).toHaveBeenCalledWith(1200);
    });

    it("keeps table layout on wide screens and links names to detail pages", () => {
        useIsMobileViewMock.mockReturnValue(false);

        render(
            <MemoryRouter>
                <ManageUsers />
            </MemoryRouter>
        );

        expect(screen.getByRole("table")).toBeTruthy();
        const alphaLink = screen.getByRole("link", { name: "Alpha User" });
        expect(alphaLink.getAttribute("href")).toBe("/manage_users/uid-a");
    });

    it("renders the dedicated manage-user detail page with the same permission controls", () => {
        useIsMobileViewMock.mockReturnValue(false);

        render(
            <MemoryRouter initialEntries={["/manage_users/uid-a"]}>
                <Routes>
                    <Route path="/manage_users/:userId" element={<ManageUserDetail />} />
                </Routes>
            </MemoryRouter>
        );

        expect(screen.getByRole("heading", { name: "Manage User" })).toBeTruthy();
        expect(screen.getByRole("link", { name: "Back to Manage Users" }).getAttribute("href")).toBe("/manage_users");
        expect(screen.getByRole("checkbox", { name: "Admin" })).toBeTruthy();
        expect(screen.getByRole("checkbox", { name: "Known User" })).toBeTruthy();
        expect(screen.getByRole("checkbox", { name: "Can Chat" })).toBeTruthy();
    });
});
