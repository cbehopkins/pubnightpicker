// @vitest-environment jsdom

import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const { pubsStateMock, roleMock, deletePubMock, notifyErrorMock } = vi.hoisted(() => ({
    pubsStateMock: vi.fn(),
    roleMock: vi.fn(),
    deletePubMock: vi.fn(),
    notifyErrorMock: vi.fn(),
}));

vi.mock("react-router-dom", () => ({
    NavLink: ({ children, to, ...props }) => <a href={to} {...props}>{children}</a>,
}));
vi.mock("../../hooks/usePubs", () => ({ usePubsState: pubsStateMock }));
vi.mock("../../hooks/useRole", () => ({ default: roleMock }));
vi.mock("../../dbtools/pubs", () => ({ deletePub: deletePubMock }));
vi.mock("../../utils/notify", () => ({ notifyError: notifyErrorMock }));

import ManagePubs from "./ManagePubs";

describe("ManagePubs stale deletion recovery", () => {
    beforeEach(() => {
        pubsStateMock.mockReturnValue({
            venues: {
                "venue-1": {
                    name: "Old name",
                    venueType: "pub",
                    address: "Old Road",
                    delete_token: "old-delete-token",
                },
            },
            isLoading: false,
            isStale: false,
            errorMessage: null,
            retry: vi.fn(),
        });
        roleMock.mockReturnValue(true);
        deletePubMock.mockReset();
        notifyErrorMock.mockReset();
    });

    it("shows the authoritative venue and requires a deliberate retry with its fresh token", async () => {
        deletePubMock
            .mockRejectedValueOnce(Object.assign(new Error("Venue changed"), {
                code: "precondition_failed",
                currentVenue: {
                    id: "venue-1",
                    name: "Current name",
                    venue_type: "restaurant",
                    address: "Current Road",
                    notes: "Current notes",
                    delete_token: "current-delete-token",
                },
            }))
            .mockResolvedValueOnce(undefined);

        render(<ManagePubs />);
        fireEvent.click(screen.getByRole("button", { name: "Delete", exact: true }));

        expect(await screen.findByRole("alert")).toHaveTextContent("Current name");
        expect(screen.getByRole("alert")).toHaveTextContent("Current Road");
        expect(screen.getByRole("alert")).toHaveTextContent("Current notes");
        expect(deletePubMock).toHaveBeenCalledTimes(1);
        expect(deletePubMock).toHaveBeenCalledWith("venue-1", "old-delete-token");

        fireEvent.click(screen.getByRole("button", { name: "Delete reviewed venue", exact: true }));
        expect(deletePubMock).toHaveBeenCalledTimes(2);
        expect(deletePubMock).toHaveBeenLastCalledWith("venue-1", "current-delete-token");
        expect(notifyErrorMock).not.toHaveBeenCalled();
    });
});
