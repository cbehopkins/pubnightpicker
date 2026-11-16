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

vi.mock("react-router-dom", async () => {
    const React = await import("react");
    return {
        Form: React.forwardRef(({ children, ...props }, ref) => <form ref={ref} {...props}>{children}</form>),
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

    it("rebases untouched fields and tokens while preserving drafts until they converge", () => {
        const { rerender } = render(
            <PubForm
                method="patch"
                pub_object={{
                    name: "Original Name",
                    venueType: "pub",
                    address: "Original Road",
                    edit_tokens: { name: "name-v1", address: "address-v1" },
                }}
            />
        );

        fireEvent.change(screen.getByLabelText("Name"), { target: { value: "My Draft" } });

        rerender(
            <PubForm
                method="patch"
                pub_object={{
                    name: "Current Name",
                    venueType: "pub",
                    address: "Current Road",
                    edit_tokens: { name: "name-v2", address: "address-v2" },
                }}
            />
        );

        expect(screen.getByLabelText("Name")).toHaveValue("My Draft");
        expect(screen.getByLabelText("Address")).toHaveValue("Current Road");
        expect(JSON.parse(document.querySelector('input[name="venue_baseline"]').value)).toMatchObject({
            name: "Original Name",
            address: "Current Road",
        });
        expect(JSON.parse(document.querySelector('input[name="edit_tokens"]').value)).toEqual({
            name: "name-v1",
            address: "address-v2",
        });

        rerender(
            <PubForm
                method="patch"
                pub_object={{
                    name: "Newest Name",
                    venueType: "pub",
                    address: "Newest Road",
                    edit_tokens: { name: "name-v3", address: "address-v3" },
                }}
            />
        );

        expect(screen.getByLabelText("Name")).toHaveValue("My Draft");
        expect(screen.getByLabelText("Address")).toHaveValue("Newest Road");
        expect(JSON.parse(document.querySelector('input[name="venue_baseline"]').value)).toMatchObject({
            name: "Original Name",
            address: "Newest Road",
        });
        expect(JSON.parse(document.querySelector('input[name="edit_tokens"]').value)).toEqual({
            name: "name-v1",
            address: "address-v3",
        });

        fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Newest Name" } });

        expect(JSON.parse(document.querySelector('input[name="venue_baseline"]').value).name).toBe("Newest Name");
        expect(JSON.parse(document.querySelector('input[name="edit_tokens"]').value).name).toBe("name-v3");
    });

    it("preserves the original API token when a dirty venue type receives a stream update", () => {
        const { rerender } = render(
            <PubForm
                method="patch"
                pub_object={{
                    name: "The Maypole",
                    venueType: "pub",
                    edit_tokens: { venue_type: "venue-type-v1" },
                }}
            />
        );

        fireEvent.change(screen.getByLabelText("Venue Type"), { target: { value: "restaurant" } });

        rerender(
            <PubForm
                method="patch"
                pub_object={{
                    name: "The Maypole",
                    venueType: "event",
                    edit_tokens: { venue_type: "venue-type-v2" },
                }}
            />
        );

        expect(screen.getByLabelText("Venue Type")).toHaveValue("restaurant");
        expect(JSON.parse(document.querySelector('input[name="venue_baseline"]').value).venueType).toBe("pub");
        expect(JSON.parse(document.querySelector('input[name="edit_tokens"]').value).venue_type).toBe("venue-type-v1");
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
