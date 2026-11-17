import { domainVenueApiFieldNames } from "./venueApiFields";

const apiFieldNames = {
  ...Object.fromEntries(
    Object.entries(domainVenueApiFieldNames).map(([domainField, apiField]) => [apiField, domainField])
  ),
};

const responseFieldNames = {
  location_id: "locationId",
  next_occurrence_date: "nextOccurrenceDate",
  visible_group_ids: "visibleGroupIds",
  previous_venue_ids: "previousVenueIds",
  edit_tokens: "editTokens",
  delete_token: "deleteToken",
};

export function normalizeVenue(venue) {
  if (!venue || typeof venue !== "object" || typeof venue.id !== "string" || !venue.id || typeof venue.name !== "string") {
    return null;
  }
  const result = Object.fromEntries(
    Object.entries(venue)
      .filter(([field]) => field !== "id")
      .map(([field, value]) => [apiFieldNames[field] || responseFieldNames[field] || field, value])
  );
  result.venueType = result.venueType || "pub";
  return result;
}