import { beforeEach, describe, expect, it, vi } from "vitest";
import { action } from "./NewPub";

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
    return new Request("http://localhost/venues", {
        method,
        body: new URLSearchParams(formValues),
    });
}

describe("NewPub action", () => {
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

    it("modifies a pub and redirects on PATCH", async () => {
        const response = await action({
            request: createRequest("PATCH", {
                name: "Updated Pub",
                venueType: "restaurant",
            }),
            params: { pubId: "pub-123" },
        });

        expect(modifyPubMock).toHaveBeenCalledTimes(1);
        expect(modifyPubMock).toHaveBeenCalledWith(
            "pub-123",
            expect.objectContaining({ venueType: "restaurant", food: true })
        );
        expect(response.status).toBe(302);
        expect(response.headers.get("Location")).toBe("/venues");
    });

    it("persists notes when editing a pub", async () => {
        await action({
            request: createRequest("PATCH", {
                name: "Updated Pub",
                notes: "This should be saved but is currently dropped",
            }),
            params: { pubId: "pub-123" },
        });

        expect(modifyPubMock).toHaveBeenCalledTimes(1);
        const payload = modifyPubMock.mock.calls[0][1];
        expect(payload).toHaveProperty("notes", "This should be saved but is currently dropped");
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
