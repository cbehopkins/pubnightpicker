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

export function createCompletingPollState(key, pubName, pollId, poll, venues) {
  const restaurantOptions = getRestaurantOptionsForPoll(poll, venues);
  const autoSelectedRestaurant = restaurantOptions.length === 1 ? restaurantOptions[0].id : "";

  return {
    key,
    pubName,
    poll_id: pollId,
    restaurantOptions,
    restaurantId: autoSelectedRestaurant,
    restaurantTime: "18:30",
  };
}

export function isRestaurantChoiceRequired(completingPoll) {
  return (completingPoll?.restaurantOptions || []).length > 1;
}

export function getRestaurantIdForCompletion(completingPoll) {
  const restaurantOptions = completingPoll?.restaurantOptions || [];
  if (restaurantOptions.length === 1) {
    return restaurantOptions[0].id;
  }
  if (restaurantOptions.length > 1) {
    return completingPoll?.restaurantId || undefined;
  }
  return undefined;
}
