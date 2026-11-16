const apiFieldNames = {
  venue_type: "venueType",
  website_url: "website",
  map_url: "map",
  image_url: "image",
  dog_friendly: "dogFriendly",
  beer_garden: "beerGarden",
  out_of_town: "outOfTown",
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
      .map(([field, value]) => [apiFieldNames[field] || field, value])
  );
  result.venueType = result.venueType || "pub";
  return result;
}