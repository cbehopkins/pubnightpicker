import { assertCurrentUserPermission, PERMISSIONS } from "../permissions";
import { requestLastOrders } from "../utils/lastOrdersApi";
import { normalizeVenue } from "../utils/venueModel";
import { domainVenueApiFieldNames } from "../utils/venueApiFields";

const nullableApiFields = new Set(["website", "map", "image"]);

function toApiFields(venue) {
    return Object.fromEntries(
        Object.entries(domainVenueApiFieldNames)
            .filter(([field]) => Object.hasOwn(venue, field))
            .map(([field, apiField]) => [apiField, nullableApiFields.has(field) && venue[field] === "" ? null : venue[field]])
    );
}

function fromApiVenue(venue) {
    const normalized = normalizeVenue(venue);
    return normalized ? { id: venue.id, ...normalized } : venue;
}

async function withDomainResponse(operation) {
    try {
        return fromApiVenue(await operation());
    } catch (error) {
        if (error?.currentVenue) {
            error.currentVenue = fromApiVenue(error.currentVenue);
        }
        throw error;
    }
}

export async function addNewVenue(venue) {
    assertCurrentUserPermission(PERMISSIONS.canManagePubs, "creating a venue");
    return withDomainResponse(() => requestLastOrders("api/venues", { body: toApiFields(venue) }));
}

export async function modifyVenue(id, venue, baseline, editTokens) {
    assertCurrentUserPermission(PERMISSIONS.canManagePubs, "editing a venue");
    const changed = Object.fromEntries(
        Object.entries(venue).filter(([field, value]) => JSON.stringify(value) !== JSON.stringify(baseline?.[field]))
    );
    const fields = toApiFields(changed);
    if (Object.keys(fields).length === 0) return null;
    const fieldTokens = Object.fromEntries(Object.keys(fields).map((field) => [field, editTokens?.[field]]));
    return withDomainResponse(() => requestLastOrders(`api/venues/${encodeURIComponent(id)}`, {
        method: "PATCH",
        contentType: "application/merge-patch+json",
        body: { ...fields, edit_tokens: fieldTokens },
    }));
}

export async function deleteVenue(id, deleteToken) {
    assertCurrentUserPermission(PERMISSIONS.canManagePubs, "deleting a venue");
    return withDomainResponse(() => requestLastOrders(`api/venues/${encodeURIComponent(id)}`, { method: "DELETE", ifMatch: deleteToken }));
}
