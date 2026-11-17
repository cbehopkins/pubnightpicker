// @vitest-environment jsdom

import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import VenueForm from "./VenueForm";

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

describe("VenueForm", () => {
    beforeEach(() => {
        navigateMock.mockReset();
        actionDataMock.current = null;
    });

    it("loads domain-named venue details and attributes into the editor", () => {
        render(<VenueForm method="patch" venueObject={{
            id: "venue-1",
            name: "The Maypole",
            website: "https://venue.example",
            image: "https://venue.example/image.png",
            dogFriendly: true,
            beerGarden: true,
            outOfTown: true,
        }} />);

        expect(screen.getByLabelText("Venue Web Site")).toHaveValue("https://venue.example");
        expect(screen.getByLabelText("Link to Venue Image")).toHaveValue("https://venue.example/image.png");
        expect(screen.getByRole("checkbox", { name: "Dog Friendly" })).toBeChecked();
        expect(screen.getByRole("checkbox", { name: "Beer Garden" })).toBeChecked();
        expect(screen.getByRole("checkbox", { name: "Out Of Town (Cambridge)" })).toBeChecked();
    });

    it("shows recurrence fields for event venues and preloads existing values", () => {
        render(
            <VenueForm
                method="patch"
                venueObject={{
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
                    nextOccurrenceDate: "2026-05-27",
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
            <VenueForm
                method="patch"
                venueObject={{
                    id: "venue-1",
                    name: "The Maypole",
                    venueType: "pub",
                    address: "1 High Street",
                    editTokens: { name: "signed-name-token" },
                }}
            />
        );

        expect(document.querySelector('input[name="edit_tokens"]').value).toContain("signed-name-token");
        expect(document.querySelector('input[name="venue_baseline"]').value).toContain("The Maypole");
    });

    it("rebases untouched fields and tokens while preserving drafts until they converge", () => {
        const { rerender } = render(
            <VenueForm
                method="patch"
                venueObject={{
                    name: "Original Name",
                    venueType: "pub",
                    address: "Original Road",
                    editTokens: { name: "name-v1", address: "address-v1" },
                }}
            />
        );

        fireEvent.change(screen.getByLabelText("Name"), { target: { value: "My Draft" } });

        rerender(
            <VenueForm
                method="patch"
                venueObject={{
                    name: "Current Name",
                    venueType: "pub",
                    address: "Current Road",
                    editTokens: { name: "name-v2", address: "address-v2" },
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
            <VenueForm
                method="patch"
                venueObject={{
                    name: "Newest Name",
                    venueType: "pub",
                    address: "Newest Road",
                    editTokens: { name: "name-v3", address: "address-v3" },
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
            <VenueForm
                method="patch"
                venueObject={{
                    name: "The Maypole",
                    venueType: "pub",
                    editTokens: { venue_type: "venue-type-v1" },
                }}
            />
        );

        fireEvent.change(screen.getByLabelText("Venue Type"), { target: { value: "restaurant" } });

        rerender(
            <VenueForm
                method="patch"
                venueObject={{
                    name: "The Maypole",
                    venueType: "event",
                    editTokens: { venue_type: "venue-type-v2" },
                }}
            />
        );

        expect(screen.getByLabelText("Venue Type")).toHaveValue("restaurant");
        expect(JSON.parse(document.querySelector('input[name="venue_baseline"]').value).venueType).toBe("pub");
        expect(JSON.parse(document.querySelector('input[name="edit_tokens"]').value).venue_type).toBe("venue-type-v1");
    });

    it("rebases a domain website draft using the API field token", () => {
        const { rerender } = render(
            <VenueForm method="patch" venueObject={{ id: "venue-1", name: "Venue", website: "https://old.example", editTokens: { website_url: "website-v1" } }} />
        );

        fireEvent.change(screen.getByLabelText("Venue Web Site"), { target: { value: "https://draft.example" } });
        rerender(<VenueForm method="patch" venueObject={{ id: "venue-1", name: "Venue", website: "https://new.example", editTokens: { website_url: "website-v2" } }} />);

        expect(screen.getByLabelText("Venue Web Site")).toHaveValue("https://draft.example");
        expect(JSON.parse(document.querySelector('input[name="venue_baseline"]').value).website).toBe("https://old.example");
        expect(JSON.parse(document.querySelector('input[name="edit_tokens"]').value).website_url).toBe("website-v1");

        fireEvent.change(screen.getByLabelText("Venue Web Site"), { target: { value: "https://new.example" } });
        expect(JSON.parse(document.querySelector('input[name="venue_baseline"]').value).website).toBe("https://new.example");
        expect(JSON.parse(document.querySelector('input[name="edit_tokens"]').value).website_url).toBe("website-v2");
    });

    it("preserves the form draft until the manager loads the current venue after a conflict", () => {
        actionDataMock.current = {
            conflict: {
                message: "The name changed.",
                fields: ["name"],
                currentVenue: {
                    id: "venue-1",
                    name: "Current Name",
                    venueType: "pub",
                    editTokens: { name: "current-token" },
                },
            },
        };
        render(<VenueForm method="patch" venueObject={{ id: "venue-1", name: "Draft Name", venueType: "pub" }} />);

        expect(screen.getByRole("alert")).toHaveTextContent("The name changed.");
        expect(screen.getByLabelText("Name")).toHaveValue("Draft Name");
        fireEvent.click(screen.getByRole("button", { name: "Load current venue" }));

        expect(screen.getByLabelText("Name")).toHaveValue("Current Name");
        expect(document.querySelector('input[name="edit_tokens"]').value).toContain("current-token");
    });

    it("keeps the reviewed conflict baseline when a draft matches the older stream snapshot", () => {
        actionDataMock.current = {
            conflict: {
                currentVenue: { id: "venue-1", name: "Current Name", editTokens: { name: "current-token" } },
            },
        };
        render(<VenueForm method="patch" venueObject={{ id: "venue-1", name: "Old Name", editTokens: { name: "old-token" } }} />);

        fireEvent.click(screen.getByRole("button", { name: "Load current venue" }));
        fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Old Name" } });

        expect(JSON.parse(document.querySelector('input[name="venue_baseline"]').value).name).toBe("Current Name");
        expect(JSON.parse(document.querySelector('input[name="edit_tokens"]').value).name).toBe("current-token");
    });

});
