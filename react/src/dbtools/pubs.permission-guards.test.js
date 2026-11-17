import { beforeEach, describe, expect, it, vi } from "vitest";

const {
    assertCurrentUserPermissionMock,
    requestLastOrdersMock,
} = vi.hoisted(() => {
    return {
        assertCurrentUserPermissionMock: vi.fn(),
        requestLastOrdersMock: vi.fn(async () => undefined),
    };
});

vi.mock("../permissions", () => {
    return {
        assertCurrentUserPermission: assertCurrentUserPermissionMock,
        PERMISSIONS: {
            canManagePubs: "canManagePubs",
        },
    };
});

vi.mock("../utils/lastOrdersApi", () => {
    return {
        requestLastOrders: requestLastOrdersMock,
    };
});

import { addNewVenue, deleteVenue, modifyVenue } from "./venues";
import { domainVenueApiFieldNames } from "../utils/venueApiFields";

describe("venue API permission guards", () => {
    beforeEach(() => {
        assertCurrentUserPermissionMock.mockReset();
        requestLastOrdersMock.mockReset();
        requestLastOrdersMock.mockResolvedValue(undefined);
    });

    it("creates a venue from domain fields using the unchanged API contract", async () => {
        await addNewVenue({
            name: "Festival", venueType: "event", website: "", map: "https://maps.example.test",
            image: "https://images.example.test", dogFriendly: false, beerGarden: true,
            outOfTown: false, recurrence: { frequency: "yearly", month: 6, month_day: 12 },
        });

        expect(requestLastOrdersMock).toHaveBeenCalledWith("api/venues", {
            body: {
                name: "Festival", venue_type: "event", website_url: null,
                map_url: "https://maps.example.test", image_url: "https://images.example.test",
                dog_friendly: false, beer_garden: true, out_of_town: false,
                recurrence: { frequency: "yearly", month: 6, month_day: 12 },
            },
        });
        expect(assertCurrentUserPermissionMock).toHaveBeenCalledWith("canManagePubs", "creating a venue");
    });

    it("rejects permission failures without sending a request", async () => {
        assertCurrentUserPermissionMock.mockImplementationOnce(() => { throw new Error("denied"); });
        await expect(addNewVenue({ name: "Festival" })).rejects.toThrow("denied");
        expect(requestLastOrdersMock).not.toHaveBeenCalled();
    });

    it("edits domain fields with API-keyed tokens and deletes with the document token", async () => {
        await modifyVenue("venue/1", {
            website: "", image: "new-image", dogFriendly: false, beerGarden: true, outOfTown: false,
        }, {
            website: "old-site", image: "old-image", dogFriendly: true, beerGarden: true, outOfTown: true,
        }, {
            website_url: "site-token", image_url: "image-token", dog_friendly: "dog-token",
            beer_garden: "unchanged-token", out_of_town: "town-token",
        });
        expect(requestLastOrdersMock).toHaveBeenCalledWith("api/venues/venue%2F1", {
            method: "PATCH",
            contentType: "application/merge-patch+json",
            body: {
                website_url: null, image_url: "new-image", dog_friendly: false, out_of_town: false,
                edit_tokens: {
                    website_url: "site-token", image_url: "image-token", dog_friendly: "dog-token", out_of_town: "town-token",
                },
            },
        });
        await deleteVenue("venue/1", "document-token");
        expect(requestLastOrdersMock).toHaveBeenLastCalledWith("api/venues/venue%2F1", {
            method: "DELETE", ifMatch: "document-token",
        });
        expect(assertCurrentUserPermissionMock).toHaveBeenCalledWith("canManagePubs", "editing a venue");
        expect(assertCurrentUserPermissionMock).toHaveBeenCalledWith("canManagePubs", "deleting a venue");
    });

    it("does not patch unchanged domain fields", async () => {
        await expect(modifyVenue("venue-1", { dogFriendly: false }, { dogFriendly: false }, {})).resolves.toBeNull();
        expect(requestLastOrdersMock).not.toHaveBeenCalled();
    });

    it("returns domain-shaped saved and conflicting venues with their IDs and tokens", async () => {
        const apiVenue = {
            id: "venue-1", name: "Festival", venue_type: "event", website_url: null,
            dog_friendly: false, edit_tokens: { name: "edit-token" }, delete_token: "delete-token",
        };
        const domainVenue = {
            id: "venue-1", name: "Festival", venueType: "event", website: null,
            dogFriendly: false, editTokens: { name: "edit-token" }, deleteToken: "delete-token",
        };
        requestLastOrdersMock.mockResolvedValueOnce(apiVenue);
        await expect(addNewVenue({ name: "Festival" })).resolves.toEqual(domainVenue);

        const conflict = Object.assign(new Error("Venue changed"), {
            code: "field_conflict", fields: ["name"], currentVenue: apiVenue,
        });
        requestLastOrdersMock.mockRejectedValueOnce(conflict);
        await expect(modifyVenue("venue-1", { name: "Festival" }, { name: "Old" }, { name: "old-token" }))
            .rejects.toMatchObject({ code: "field_conflict", fields: ["name"], currentVenue: domainVenue });
    });

    it("returns the current domain venue and fresh delete token after a delete conflict", async () => {
        const conflict = Object.assign(new Error("Venue changed"), {
            code: "precondition_failed",
            currentVenue: {
                id: "venue-1", name: "Festival", venue_type: "event",
                delete_token: "current-token", edit_tokens: { name: "current-edit-token" },
            },
        });
        requestLastOrdersMock.mockRejectedValueOnce(conflict);

        await expect(deleteVenue("venue-1", "old-token")).rejects.toMatchObject({
            code: "precondition_failed",
            currentVenue: {
                id: "venue-1", name: "Festival", venueType: "event",
                deleteToken: "current-token", editTokens: { name: "current-edit-token" },
            },
        });
    });

    it("provides API field names for domain-form conflict and token handling", () => {
        expect(domainVenueApiFieldNames).toMatchObject({
            name: "name", venueType: "venue_type", website: "website_url", map: "map_url",
            image: "image_url", dogFriendly: "dog_friendly", beerGarden: "beer_garden", outOfTown: "out_of_town",
            recurrence: "recurrence",
        });
    });

    it("guards addNewVenue with canManagePubs", async () => {
        await addNewVenue({ name: "The Test Arms" });

        expect(assertCurrentUserPermissionMock).toHaveBeenCalledWith(
            "canManagePubs",
            "creating a venue",
        );
        expect(requestLastOrdersMock).toHaveBeenCalledWith("api/venues", {
            body: { name: "The Test Arms" },
        });
    });

    it("guards modifyVenue with canManagePubs", async () => {
        await modifyVenue(
            "pub-1",
            { name: "Updated Name", address: "Same Road" },
            { name: "Old Name", address: "Same Road" },
            { name: "signed-name-token", address: "unchanged-field-token" },
        );

        expect(assertCurrentUserPermissionMock).toHaveBeenCalledWith(
            "canManagePubs",
            "editing a venue",
        );
        expect(requestLastOrdersMock).toHaveBeenCalledWith("api/venues/pub-1", {
            method: "PATCH",
            contentType: "application/merge-patch+json",
            body: { name: "Updated Name", edit_tokens: { name: "signed-name-token" } },
        });
    });

    it("does not send a PATCH when all venue fields match their baseline", async () => {
        await expect(modifyVenue("pub-1", { name: "The Maypole" }, { name: "The Maypole" }, {})).resolves.toBeNull();
        expect(requestLastOrdersMock).not.toHaveBeenCalled();
    });

    it("guards deleteVenue with canManagePubs", async () => {
        await deleteVenue("pub-2", "signed-document-version");

        expect(assertCurrentUserPermissionMock).toHaveBeenCalledWith(
            "canManagePubs",
            "deleting a venue",
        );
        expect(requestLastOrdersMock).toHaveBeenCalledWith("api/venues/pub-2", {
            method: "DELETE",
            ifMatch: "signed-document-version",
        });
    });
});
