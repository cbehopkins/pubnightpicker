export function getRestaurantOptionsForPoll(poll, venues) {
  const pollPubs = poll?.pubs || {};
  const restaurantOptions = [];
  for (const [venueId, pollVenue] of Object.entries(pollPubs)) {
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

  restaurantOptions.sort((a, b) => a.name.localeCompare(b.name));
  return restaurantOptions;
}

export function getAllRestaurantVenues(venues) {
  const results = [];
  for (const [venueId, venue] of Object.entries(venues || {})) {
    if ((venue?.venueType || "pub") === "restaurant") {
      results.push({ id: venueId, name: venue?.name || venueId });
    }
  }
  results.sort((a, b) => a.name.localeCompare(b.name));
  return results;
}

export function createCompletingPollState(key, pubName, pollId, poll, venues) {
  const selectedVenue = venues?.[key];
  const pubHasFood = Boolean(selectedVenue?.food);

  const restaurantOptions = getRestaurantOptionsForPoll(poll, venues);
  const allRestaurantVenues = getAllRestaurantVenues(venues);

  const autoSelectedRestaurant =
    !pubHasFood && restaurantOptions.length === 1 ? restaurantOptions[0].id : "";

  return {
    key,
    pubName,
    poll_id: pollId,
    pubHasFood,
    restaurantOptions,
    allRestaurantVenues,
    restaurantId: autoSelectedRestaurant,
    restaurantTime: autoSelectedRestaurant ? "18:30" : "",
  };
}

export function isRestaurantChoiceRequired(completingPoll) {
  return (completingPoll?.restaurantOptions || []).length > 1;
}

export function getRestaurantIdForCompletion(completingPoll) {
  if (completingPoll?.pubHasFood) {
    return undefined;
  }
  return completingPoll?.restaurantId || undefined;
}
