import { assertCurrentUserPermission, PERMISSIONS } from "../permissions";
import { requestLastOrders } from "../utils/lastOrdersApi";

const apiFieldNames = {
    name: "name",
    venueType: "venue_type",
    web_site: "website_url",
    map: "map_url",
    address: "address",
    notes: "notes",
    pubImage: "image_url",
    recurrence: "recurrence",
    parking: "parking",
    food: "food",
    dog_friend: "dog_friendly",
    beer_gerden: "beer_garden",
    out_of_town: "out_of_town",
    banned: "banned",
};

const nullableApiFields = new Set(["web_site", "map", "pubImage"]);

function toApiFields(pubParams) {
    return Object.fromEntries(
        Object.entries(apiFieldNames)
            .filter(([fieldName]) => Object.hasOwn(pubParams, fieldName))
            .map(([fieldName, apiFieldName]) => {
                const value = pubParams[fieldName];
                return [apiFieldName, nullableApiFields.has(fieldName) && value === "" ? null : value];
            })
    );
}

export async function addNewPub(pubParams) {
    assertCurrentUserPermission(PERMISSIONS.canManagePubs, "creating a pub");
    return requestLastOrders("api/venues", { body: toApiFields(pubParams) });
}

export async function modifyPub(id, pubParams) {
    assertCurrentUserPermission(PERMISSIONS.canManagePubs, "editing a pub");
    return requestLastOrders(`api/venues/${encodeURIComponent(id)}`, {
        method: "PATCH",
        contentType: "application/merge-patch+json",
        body: toApiFields(pubParams),
    });
}

export async function deletePub(id) {
    assertCurrentUserPermission(PERMISSIONS.canManagePubs, "deleting a pub");
    return requestLastOrders(`api/venues/${encodeURIComponent(id)}`, { method: "DELETE" });
}
