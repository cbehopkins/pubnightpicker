// @vitest-environment jsdom

import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import PubForm from "./PubForm";

const { navigateMock, navigationStateMock, actionDataMock } = vi.hoisted(() => {
    return {
        navigateMock: vi.fn(),
        navigationStateMock: { state: "idle" },
        actionDataMock: { current: null },
    };
});

vi.mock("react-router-dom", () => {
    return {
        Form: ({ children }) => <form>{children}</form>,
        useNavigate: () => navigateMock,
        useNavigation: () => navigationStateMock,
        useActionData: () => actionDataMock.current,
    };
});

describe("PubForm", () => {
    beforeEach(() => {
        navigateMock.mockReset();
        actionDataMock.current = null;
    });

    it("shows recurrence fields for event venues and preloads existing values", () => {
        render(
            <PubForm
                method="patch"
                pub_object={{
                    name: "Cambridge Beer Festival",
                    venueType: "event",
                    banned: true,
                    recurrence: {
                        frequency: "yearly",
                        interval: 1,
                        month: 5,
                        weekday: 2,
                        nth: -1,
                        month_day: 27,
                    },
                    next_occurrence_date: "2026-05-27",
                }}
            />
        );

        // Verify recurrence type is set
        expect(screen.getByLabelText(/recurrence type/i).value).toBe("yearly");
        expect(screen.getByLabelText(/banned/i).checked).toBe(true);

        // Verify yearly mode is set to "weekday" based on existing weekday data
        const weekdayRadio = screen.getByLabelText(/specific weekday/i);
        expect(weekdayRadio.checked).toBe(true);

        // Verify only weekday-related fields are visible for this mode
        expect(screen.getByLabelText(/^month$/i)).toBeInTheDocument();
        expect(screen.getByLabelText(/which week/i)).toBeInTheDocument();

        // Verify preloaded values
        expect(screen.getByLabelText(/^month$/i).value).toBe("5");
        expect(screen.getByLabelText(/which week/i).value).toBe("-1");

        // Verify next occurrence date is displayed with ordinal day formatting
        expect(screen.getByText(/27th May 2026/i)).toBeInTheDocument();
    });

    it("includes the loaded venue's baseline and signed field tokens in edit submissions", () => {
        render(
            <PubForm
                method="patch"
                pub_object={{
                    id: "venue-1",
                    name: "The Maypole",
                    venueType: "pub",
                    address: "1 High Street",
                    edit_tokens: { name: "signed-name-token" },
                }}
            />
        );

        expect(document.querySelector('input[name="edit_tokens"]').value).toContain("signed-name-token");
        expect(document.querySelector('input[name="venue_baseline"]').value).toContain("The Maypole");
    });

    it("preserves the form draft until the manager loads the current venue after a conflict", () => {
        actionDataMock.current = {
            conflict: {
                message: "The name changed.",
                fields: ["name"],
                currentVenue: {
                    id: "venue-1",
                    name: "Current Name",
                    venue_type: "pub",
                    edit_tokens: { name: "current-token" },
                },
            },
        };
        render(<PubForm method="patch" pub_object={{ id: "venue-1", name: "Draft Name", venueType: "pub" }} />);

        expect(screen.getByRole("alert")).toHaveTextContent("The name changed.");
        expect(screen.getByLabelText("Name")).toHaveValue("Draft Name");
        fireEvent.click(screen.getByRole("button", { name: "Load current venue" }));

        expect(screen.getByLabelText("Name")).toHaveValue("Current Name");
        expect(document.querySelector('input[name="edit_tokens"]').value).toContain("current-token");
    });

    it("adopts the complete venue returned after a successful save", async () => {
        actionDataMock.current = {
            savedVenue: {
                id: "venue-1",
                name: "Saved Name",
                venue_type: "restaurant",
                address: "Merged Road",
                edit_tokens: { name: "fresh-token" },
            },
        };
        render(<PubForm method="patch" pub_object={{ id: "venue-1", name: "Old Name", venueType: "pub" }} />);

        await waitFor(() => expect(screen.getByLabelText("Name")).toHaveValue("Saved Name"));
        expect(screen.getByLabelText("Address")).toHaveValue("Merged Road");
        expect(screen.getByLabelText("Venue Type")).toHaveValue("restaurant");
        expect(navigateMock).not.toHaveBeenCalled();
    });
});
