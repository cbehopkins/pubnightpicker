import { beforeEach, describe, expect, it, vi } from "vitest";
import { action } from "./NewVenue";

const {
    addNewPubMock,
    modifyPubMock,
    notifyErrorMock,
} = vi.hoisted(() => {
    return {
        addNewPubMock: vi.fn(),
        modifyPubMock: vi.fn(),
        notifyErrorMock: vi.fn(),
    };
});

vi.mock("../../dbtools/pubs", () => {
    return {
        addNewPub: addNewPubMock,
        modifyPub: modifyPubMock,
    };
});

vi.mock("../../utils/notify", () => {
    return {
        notifyError: notifyErrorMock,
    };
});

function createRequest(method, formValues) {
    const body = new URLSearchParams();
    for (const [key, value] of Object.entries(formValues)) {
        body.set(key, typeof value === "string" ? value : JSON.stringify(value));
    }
    return new Request("http://localhost/venues", {
        method,
        body,
    });
}

describe("NewVenue action", () => {
    beforeEach(() => {
        addNewPubMock.mockReset();
        modifyPubMock.mockReset();
        notifyErrorMock.mockReset();
    });

    it("creates a pub and redirects on POST", async () => {
        const response = await action({
            request: createRequest("POST", {
                name: "Test Pub",
                web_site: "https://example.com",
                map: "https://maps.example.com",
                address: "1 Test Street",
                pubImage: "https://img.example.com/pub.png",
            }),
            params: {},
        });

        expect(addNewPubMock).toHaveBeenCalledTimes(1);
        expect(addNewPubMock).toHaveBeenCalledWith(expect.objectContaining({ venueType: "pub" }));
        expect(modifyPubMock).not.toHaveBeenCalled();
        expect(response.status).toBe(302);
        expect(response.headers.get("Location")).toBe("/venues");
    });

    it("preserves domain form fields in the legacy create request", async () => {
        await action({
            request: createRequest("POST", {
                name: "Venue",
                website: "https://example.com",
                image: "https://example.com/image.png",
                dogFriendly: "on",
                beerGarden: "on",
                outOfTown: "on",
            }),
            params: {},
        });

        expect(addNewPubMock).toHaveBeenCalledWith(expect.objectContaining({
            web_site: "https://example.com",
            pubImage: "https://example.com/image.png",
            dog_friend: true,
            beer_gerden: true,
            out_of_town: true,
        }));
    });

    it("always persists restaurant venues as serving food", async () => {
        await action({
            request: createRequest("POST", {
                name: "Bistro 12",
                venueType: "restaurant",
            }),
            params: {},
        });

        expect(addNewPubMock).toHaveBeenCalledWith(
            expect.objectContaining({ venueType: "restaurant", food: true })
        );
    });

    it("keeps non-restaurant food flag tied to checkbox state", async () => {
        await action({
            request: createRequest("POST", {
                name: "The Maypole",
                venueType: "pub",
            }),
            params: {},
        });

        expect(addNewPubMock).toHaveBeenCalledWith(
            expect.objectContaining({ venueType: "pub", food: false })
        );
    });

    it("persists recurrence fields for event venues - weekday mode", async () => {
        await action({
            request: createRequest("POST", {
                name: "Beer Festival",
                venueType: "event",
                recurrence_frequency: "yearly",
                recurrence_interval: "1",
                recurrence_month: "5",
                recurrence_weekday: "2",
                recurrence_nth: "-1",
            }),
            params: {},
        });

        expect(addNewPubMock).toHaveBeenCalledWith(
            expect.objectContaining({
                venueType: "event",
                recurrence: expect.objectContaining({
                    frequency: "yearly",
                    month: 5,
                    weekday: 2,
                    nth: -1,
                    interval: 1,
                }),
            })
        );
    });

    it("persists recurrence fields for event venues - fixed date mode", async () => {
        await action({
            request: createRequest("POST", {
                name: "New Year Party",
                venueType: "event",
                recurrence_frequency: "yearly",
                recurrence_month: "1",
                recurrence_month_day: "1",
                recurrence_interval: "1",
                // Note: weekday and nth are NOT sent when form is in "fixed" mode
            }),
            params: {},
        });

        expect(addNewPubMock).toHaveBeenCalledWith(
            expect.objectContaining({
                venueType: "event",
                recurrence: expect.objectContaining({
                    frequency: "yearly",
                    month: 1,
                    month_day: 1,
                    interval: 1,
                }),
            })
        );
    });

    it("submits the venue baseline and field tokens and redirects after a successful PATCH", async () => {
        const baseline = {
            name: "Old Pub",
            venueType: "pub",
            website: "https://old.example",
            map: "",
            address: "1 Old Street",
            notes: "",
            image: "",
            parking: false,
            food: false,
            dogFriendly: false,
            beerGarden: false,
            outOfTown: false,
            banned: false,
            recurrence: null,
        };
        const editTokens = { name: "signed-name-baseline", website_url: "signed-website-baseline" };
        const savedVenue = { id: "pub-123", name: "Updated Pub", venue_type: "pub" };
        modifyPubMock.mockResolvedValueOnce(savedVenue);
        const response = await action({
            request: createRequest("PATCH", {
                name: "Updated Pub",
                venueType: "pub",
                website: "https://updated.example",
                map: "",
                address: "1 Old Street",
                notes: "",
                image: "",
                recurrence_frequency: "none",
                venue_baseline: baseline,
                edit_tokens: editTokens,
            }),
            params: { venueId: "pub-123" },
        });

        expect(modifyPubMock).toHaveBeenCalledWith(
            "pub-123",
            expect.objectContaining({ name: "Updated Pub", web_site: "https://updated.example" }),
            expect.objectContaining({ name: "Old Pub", web_site: "https://old.example", dog_friend: false }),
            editTokens,
        );
        expect(response.status).toBe(302);
        expect(response.headers.get("Location")).toBe("/venues");
    });

    it("persists notes when editing a pub", async () => {
        await action({
            request: createRequest("PATCH", {
                name: "Updated Pub",
                venueType: "pub",
                web_site: "",
                map: "",
                address: "",
                notes: "This should be saved but is currently dropped",
                pubImage: "",
                recurrence_frequency: "none",
                venue_baseline: { name: "Old Pub", venueType: "pub", web_site: "", map: "", address: "", notes: "Old note", pubImage: "", parking: false, food: false, dog_friend: false, beer_gerden: false, out_of_town: false, banned: false, recurrence: null },
                edit_tokens: { name: "name-token", notes: "notes-token" },
            }),
            params: { venueId: "pub-123" },
        });

        expect(modifyPubMock).toHaveBeenCalledTimes(1);
        const payload = modifyPubMock.mock.calls[0][1];
        expect(payload).toHaveProperty("notes", "This should be saved but is currently dropped");
        expect(modifyPubMock.mock.calls[0][3]).toHaveProperty("notes", "notes-token");
    });

    it("returns field conflicts to the editor without showing a generic toast", async () => {
        modifyPubMock.mockRejectedValueOnce(Object.assign(new Error("The name changed."), {
            code: "field_conflict",
            fields: ["name"],
            currentVenue: { id: "pub-123", name: "Current Name" },
        }));

        const result = await action({
            request: createRequest("PATCH", {
                name: "Draft Name",
                venueType: "pub",
                web_site: "",
                map: "",
                address: "1 Old Street",
                notes: "",
                pubImage: "",
                recurrence_frequency: "none",
                venue_baseline: { name: "Old Name" },
                edit_tokens: { name: "signed-token" },
            }),
            params: { venueId: "pub-123" },
        });

        expect(result).toEqual({
            conflict: {
                message: "The name changed.",
                fields: ["name"],
                currentVenue: { id: "pub-123", name: "Current Name", venueType: "pub" },
            },
        });
        expect(notifyErrorMock).not.toHaveBeenCalled();
    });

    it("shows a user-facing error and aborts redirect on failure", async () => {
        addNewPubMock.mockRejectedValueOnce(new Error("backend exploded"));

        const result = await action({
            request: createRequest("POST", {
                name: "Bad Pub",
            }),
            params: {},
        });

        expect(notifyErrorMock).toHaveBeenCalledTimes(1);
        expect(result).toBeNull();
    });
});
