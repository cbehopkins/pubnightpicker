function getAttendanceState(attendance, venueId, currentUserId) {
  const attendanceForVenue = venueId ? (attendance[venueId] || {}) : {};
  const canCome = attendanceForVenue.canCome || [];
  const cannotCome = attendanceForVenue.cannotCome || [];

  return {
    canCome,
    cannotCome,
    userCanCome: Boolean(currentUserId) && canCome.includes(currentUserId),
    userCannotCome: Boolean(currentUserId) && cannotCome.includes(currentUserId),
    hasAttendanceData: canCome.length > 0 || cannotCome.length > 0,
  };
}

export function getDedupedVotesForVenue(votes, venueId) {
  const allVotes = [];

  if (venueId && votes[venueId]) {
    allVotes.push(...votes[venueId]);
  }
  if (venueId && votes.any) {
    allVotes.push(...votes.any);
  }

  return [...new Set(allVotes)];
}

export function buildCurrentEventViewModel({
  current_pub_id,
  restaurant_id,
  restaurant_time,
  pub_parameters,
  votes,
  attendance,
  currUserId,
  show_voters,
}) {
  const currentVenue = pub_parameters[current_pub_id];
  if (!currentVenue) {
    return null;
  }

  const mainAttendance = getAttendanceState(attendance, current_pub_id, currUserId);
  const mainVotes = getDedupedVotesForVenue(votes, current_pub_id);
  const pubWasVotedFor = current_pub_id in votes || Boolean(votes.any);

  const mainVenue = {
    id: current_pub_id,
    name: currentVenue.name,
    website: currentVenue.web_site,
    image: currentVenue.pubImage,
    address: currentVenue.address,
    dedupedVotes: mainVotes,
    allowShowVoters: show_voters && (pubWasVotedFor || mainAttendance.hasAttendanceData),
    ...mainAttendance,
  };

  const restaurantSource = restaurant_id ? pub_parameters[restaurant_id] : null;
  let restaurantVenue = null;
  if (restaurantSource) {
    const restaurantAttendance = getAttendanceState(attendance, restaurant_id, currUserId);
    const restaurantVotes = getDedupedVotesForVenue(votes, restaurant_id);
    restaurantVenue = {
      id: restaurant_id,
      name: restaurantSource.name,
      restaurantTime: restaurant_time,
      website: restaurantSource.web_site,
      image: restaurantSource.pubImage,
      address: restaurantSource.address,
      dedupedVotes: restaurantVotes,
      allowShowVoters: show_voters && (restaurantVotes.length > 0 || restaurantAttendance.hasAttendanceData),
      ...restaurantAttendance,
    };
  }

  return {
    mainVenue,
    restaurantVenue,
  };
}
