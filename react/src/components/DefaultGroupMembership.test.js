// @vitest-environment jsdom

import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import DefaultGroupMembership from "./DefaultGroupMembership";
import { requestLastOrders } from "../utils/lastOrdersApi";

vi.mock("../utils/lastOrdersApi", () => ({ requestLastOrders: vi.fn() }));

function boundary(uid = "caller", groupId = "default-group", loading = false) {
    return <DefaultGroupMembership key={`${groupId}:${uid}`} groupId={groupId} uid={uid} loading={loading}>
        <div>Application content</div>
    </DefaultGroupMembership>;
}

describe("default Group membership", () => {
    beforeEach(() => {
        vi.mocked(requestLastOrders).mockReset();
        vi.mocked(requestLastOrders).mockResolvedValue(undefined);
    });

    it("keeps anonymous browsing available without joining", () => {
        render(boundary(null));
        expect(screen.getByText("Application content")).toBeTruthy();
        expect(requestLastOrders).not.toHaveBeenCalled();
    });

    it("waits for authentication to resolve", () => {
        render(boundary(null, "default-group", true));
        expect(screen.queryByText("Application content")).toBeNull();
        expect(requestLastOrders).not.toHaveBeenCalled();
    });

    it("waits for self-join before mounting Group actions", async () => {
        let complete;
        vi.mocked(requestLastOrders).mockImplementation(() => new Promise((resolve) => { complete = resolve; }));
        render(boundary());
        expect(screen.queryByText("Application content")).toBeNull();
        expect(requestLastOrders).toHaveBeenCalledWith("/api/groups/default-group/members/caller", expect.objectContaining({ method: "PUT" }));
        complete();
        expect(await screen.findByText("Application content")).toBeTruthy();
    });

    it("offers retry without exposing Group actions after a failed join", async () => {
        vi.mocked(requestLastOrders).mockRejectedValueOnce(new Error("offline"));
        render(boundary());
        expect(await screen.findByRole("alert")).toHaveTextContent("Unable to join the default Group");
        expect(screen.queryByText("Application content")).toBeNull();
        fireEvent.click(screen.getByRole("button", { name: "Retry" }));
        expect(await screen.findByText("Application content")).toBeTruthy();
        expect(requestLastOrders).toHaveBeenCalledTimes(2);
    });

    it("joins after login and again after switching accounts", async () => {
        const { rerender } = render(boundary(null));
        rerender(boundary("first"));
        await screen.findByText("Application content");
        rerender(boundary("second"));
        await waitFor(() => expect(requestLastOrders).toHaveBeenCalledWith("/api/groups/default-group/members/second", expect.anything()));
        await screen.findByText("Application content");
        expect(requestLastOrders).toHaveBeenCalledTimes(2);
    });

    it("aborts an old session and ignores its completion after logout", async () => {
        let complete;
        vi.mocked(requestLastOrders).mockImplementation(() => new Promise((resolve) => { complete = resolve; }));
        const { rerender } = render(boundary());
        const signal = vi.mocked(requestLastOrders).mock.calls[0][1].signal;
        rerender(boundary(null));
        expect(signal.aborted).toBe(true);
        complete();
        expect(screen.getByText("Application content")).toBeTruthy();
        expect(screen.queryByRole("alert")).toBeNull();
    });
});