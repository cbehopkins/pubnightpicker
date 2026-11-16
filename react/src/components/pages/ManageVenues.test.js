// @vitest-environment jsdom

import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const { venuesStateMock, roleMock, deleteVenueMock, notifyErrorMock } = vi.hoisted(() => ({
    venuesStateMock: vi.fn(),
    roleMock: vi.fn(),
    deleteVenueMock: vi.fn(),
    notifyErrorMock: vi.fn(),
}));

vi.mock("react-router-dom", () => ({
    NavLink: ({ children, to, ...props }) => <a href={to} {...props}>{children}</a>,
}));
vi.mock("../../hooks/useVenues", () => ({ useVenuesState: venuesStateMock }));
vi.mock("../../hooks/useRole", () => ({ default: roleMock }));
vi.mock("../../dbtools/venues", () => ({ deleteVenue: deleteVenueMock }));
vi.mock("../../utils/notify", () => ({ notifyError: notifyErrorMock }));

import ManageVenues from "./ManageVenues";

describe("ManageVenues stale deletion recovery", () => {
    beforeEach(() => {
        venuesStateMock.mockReturnValue({
            venues: {
                "venue-1": {
                    name: "Old name",
                    venueType: "pub",
                    address: "Old Road",
                    deleteToken: "old-delete-token",
                },
            },
            isLoading: false,
            isStale: false,
            errorMessage: null,
            retry: vi.fn(),
        });
        roleMock.mockReturnValue(true);
        deleteVenueMock.mockReset();
        notifyErrorMock.mockReset();
    });

    it("shows the authoritative venue and requires a deliberate retry with its fresh token", async () => {
        deleteVenueMock
            .mockRejectedValueOnce(Object.assign(new Error("Venue changed"), {
                code: "precondition_failed",
                currentVenue: {
                    id: "venue-1",
                    name: "Current name",
                    venueType: "restaurant",
                    address: "Current Road",
                    notes: "Current notes",
                    deleteToken: "current-delete-token",
                },
            }))
            .mockResolvedValueOnce(undefined);

        render(<ManageVenues />);
        fireEvent.click(screen.getByRole("button", { name: "Delete", exact: true }));

        expect(await screen.findByRole("alert")).toHaveTextContent("Current name");
        expect(screen.getByRole("alert")).toHaveTextContent("restaurant");
        expect(screen.getByRole("alert")).toHaveTextContent("Current Road");
        expect(screen.getByRole("alert")).toHaveTextContent("Current notes");
        expect(deleteVenueMock).toHaveBeenCalledTimes(1);
        expect(deleteVenueMock).toHaveBeenCalledWith("venue-1", "old-delete-token");

        fireEvent.click(screen.getByRole("button", { name: "Delete reviewed venue", exact: true }));
        expect(deleteVenueMock).toHaveBeenCalledTimes(2);
        expect(deleteVenueMock).toHaveBeenLastCalledWith("venue-1", "current-delete-token");
        expect(notifyErrorMock).not.toHaveBeenCalled();
    });
});
