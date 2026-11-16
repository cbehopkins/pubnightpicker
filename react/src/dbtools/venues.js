import { addNewPub, deletePub, modifyPub } from "./pubs";
import { normalizeVenue } from "../utils/venueModel";

const legacyFieldNames = {
    website: "web_site",
    image: "pubImage",
    dogFriendly: "dog_friend",
    beerGarden: "beer_gerden",
    outOfTown: "out_of_town",
};

function toLegacyFields(venue) {
    return Object.fromEntries(
        Object.entries(venue).map(([field, value]) => [legacyFieldNames[field] || field, value])
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

export function addNewVenue(venue) {
    return withDomainResponse(() => addNewPub(toLegacyFields(venue)));
}

export function modifyVenue(id, venue, baseline, editTokens) {
    return withDomainResponse(() => modifyPub(id, toLegacyFields(venue), baseline == null ? baseline : toLegacyFields(baseline), editTokens));
}

export function deleteVenue(id, deleteToken) {
    return withDomainResponse(() => deletePub(id, deleteToken));
}