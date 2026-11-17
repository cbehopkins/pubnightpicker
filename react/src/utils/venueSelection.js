import { sortVenueOptionsByName } from "./venueSort";

export function getRestaurantOptionsForPoll(meet, venues) {
  const meetVenues = meet?.venues || {};
  const restaurantOptions = [];
  for (const [venueId, pollVenue] of Object.entries(meetVenues)) {
    if (venueId === "any") {
      continue;
    }
    const venue = venues[venueId];
    const venueType = venue?.venueType || "pub";
    if (venueType !== "restaurant") {
      continue;
    }
    restaurantOptions.push({
      id: venueId,
      name: venue?.name || pollVenue?.name || venueId,
    });
  }

  return sortVenueOptionsByName(restaurantOptions);
}

export function getAllRestaurantVenues(venues) {
  const results = [];
  for (const [venueId, venue] of Object.entries(venues || {})) {
    if ((venue?.venueType || "pub") === "restaurant") {
      results.push({ id: venueId, name: venue?.name || venueId });
    }
  }
  return sortVenueOptionsByName(results);
}

export function getAllMainVenueOptions(venues) {
  const results = [];
  for (const [venueId, venue] of Object.entries(venues || {})) {
    if ((venue?.venueType || "pub") !== "restaurant") {
      results.push({ id: venueId, name: venue?.name || venueId });
    }
  }
  return sortVenueOptionsByName(results);
}

export function getDefaultRestaurantTime(restaurantId, currentTime) {
  if (!restaurantId) {
    return "";
  }
  return currentTime || "18:30";
}

export function createCompletingPollState(key, venueName, pollId, poll, venues) {
  const selectedVenue = venues?.[key];
  const venueHasFood = Boolean(selectedVenue?.food);

  const restaurantOptions = getRestaurantOptionsForPoll(poll, venues);
  const allRestaurantVenues = getAllRestaurantVenues(venues);

  const autoSelectedRestaurant =
    !venueHasFood && restaurantOptions.length === 1 ? restaurantOptions[0].id : "";

  return {
    key,
    venueName,
    poll_id: pollId,
    venueHasFood,
    restaurantOptions,
    allRestaurantVenues,
    restaurantId: autoSelectedRestaurant,
    restaurantTime: getDefaultRestaurantTime(autoSelectedRestaurant),
  };
}

export function isRestaurantChoiceRequired(completingPoll) {
  return (completingPoll?.restaurantOptions || []).length > 1;
}

export function getRestaurantIdForCompletion(completingPoll) {
  if (completingPoll?.venueHasFood) {
    return undefined;
  }
  return completingPoll?.restaurantId || undefined;
}
