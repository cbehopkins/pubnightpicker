// @vitest-environment jsdom

import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes, useLocation, useNavigate } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import AdminLayout from "./AdminLayout";

const { useRoleStatusMock } = vi.hoisted(() => ({ useRoleStatusMock: vi.fn() }));

vi.mock("../../hooks/useRole", () => ({ useRoleStatus: useRoleStatusMock }));

function HistoryControls() {
    const navigate = useNavigate();
    const { pathname } = useLocation();
    return (
        <>
            <button onClick={() => navigate(-1)}>Back</button>
            <button onClick={() => navigate(1)}>Forward</button>
            <output aria-label="Current path">{pathname}</output>
        </>
    );
}

function renderAdmin(path = "/admin/users") {
    return render(
        <MemoryRouter initialEntries={[path]}>
            <HistoryControls />
            <Routes>
                <Route path="/" element={<h1>Home</h1>} />
                <Route path="/admin" element={<AdminLayout />}>
                    <Route path="users" element={<h1>Users content</h1>} />
                    <Route path="users/:userId" element={<h1>User detail content</h1>} />
                    <Route path="diagnostics" element={<h1>Diagnostics content</h1>} />
                    <Route path="logs" element={<h1>Logs content</h1>} />
                    <Route path="config" element={<h1>Config content</h1>} />
                </Route>
            </Routes>
        </MemoryRouter>,
    );
}

describe("Admin layout", () => {
    beforeEach(() => {
        useRoleStatusMock.mockReturnValue({ hasPermission: true, loading: false });
    });
    afterEach(cleanup);

    it("navigates between sections and expands only the active route", () => {
        renderAdmin();
        expect(useRoleStatusMock).toHaveBeenCalledWith("admin");
        expect(screen.getByRole("button", { name: "Manage Users" })).toHaveAttribute("aria-expanded", "true");
        fireEvent.click(screen.getByRole("button", { name: "Logs" }));
        expect(screen.getByRole("heading", { name: "Logs content" })).toBeInTheDocument();
        expect(screen.getByRole("button", { name: "Logs" })).toHaveAttribute("aria-expanded", "true");
        expect(screen.getByRole("button", { name: "Manage Users" })).toHaveAttribute("aria-expanded", "false");
        expect(screen.getByLabelText("Current path")).toHaveTextContent("/admin/logs");
        expect(screen.queryByRole("heading", { name: "Users content" })).not.toBeInTheDocument();
    });

    it("opens the matching section when visiting a direct URL", () => {
        renderAdmin("/admin/config");
        expect(screen.getByRole("heading", { name: "Config content" })).toBeInTheDocument();
        expect(screen.getByRole("button", { name: "Config" })).toHaveAttribute("aria-expanded", "true");
        expect(screen.queryByRole("combobox")).not.toBeInTheDocument();
    });

    it("follows Back and Forward navigation", () => {
        renderAdmin();
        fireEvent.click(screen.getByRole("button", { name: "Diagnostics" }));
        fireEvent.click(screen.getByRole("button", { name: "Config" }));
        fireEvent.click(screen.getByRole("button", { name: "Back" }));
        expect(screen.getByRole("heading", { name: "Diagnostics content" })).toBeInTheDocument();
        expect(screen.getByRole("button", { name: "Diagnostics" })).toHaveAttribute("aria-expanded", "true");
        fireEvent.click(screen.getByRole("button", { name: "Forward" }));
        expect(screen.getByRole("heading", { name: "Config content" })).toBeInTheDocument();
        expect(screen.getByRole("button", { name: "Config" })).toHaveAttribute("aria-expanded", "true");
    });

    it("keeps Manage Users selected for user detail routes", () => {
        renderAdmin("/admin/users/example-user");
        expect(screen.getByRole("heading", { name: "User detail content" })).toBeInTheDocument();
        expect(screen.getByRole("button", { name: "Manage Users" })).toHaveAttribute("aria-expanded", "true");
        fireEvent.click(screen.getByRole("button", { name: "Manage Users" }));
        expect(screen.getByRole("heading", { name: "User detail content" })).toBeInTheDocument();
        expect(screen.getByLabelText("Current path")).toHaveTextContent("/admin/users/example-user");
        expect(screen.getByRole("button", { name: "Manage Users" })).toHaveAttribute("aria-expanded", "true");
    });

    it("does not render admin content while permissions load", () => {
        useRoleStatusMock.mockReturnValue({ hasPermission: false, loading: true });
        renderAdmin();
        expect(screen.getByText("Loading permissions...")).toBeInTheDocument();
        expect(screen.queryByRole("navigation")).not.toBeInTheDocument();
        expect(screen.queryByRole("heading", { name: "Users content" })).not.toBeInTheDocument();
    });

    it("redirects non-admin users without rendering admin content", () => {
        useRoleStatusMock.mockReturnValue({ hasPermission: false, loading: false });
        renderAdmin("/admin/config");
        expect(screen.getByRole("heading", { name: "Home" })).toBeInTheDocument();
        expect(screen.queryByRole("heading", { name: "Config content" })).not.toBeInTheDocument();
    });
});
