import { assertCurrentUserPermission, PERMISSIONS } from "../permissions";
import { requestLastOrders } from "../utils/lastOrdersApi";

export async function deleteMeet(meetId) {
    assertCurrentUserPermission(PERMISSIONS.canCreatePoll, "deleting a poll");
    await requestLastOrders(`api/meet/${encodeURIComponent(meetId)}`, {
        method: "DELETE",
    });
}

export async function rescheduleMeet(meetId, currentVenueId, nextVenueId, restaurantId, restaurantTime) {
    assertCurrentUserPermission(PERMISSIONS.canCompletePoll, "rescheduling a poll");
    await requestLastOrders(`api/meet/${encodeURIComponent(meetId)}/reschedule`, {
        method: "POST",
        body: {
            ...(nextVenueId ? { venue_id: nextVenueId } : {}),
            ...(restaurantId ? { restaurant_id: restaurantId, restaurant_time: restaurantTime } : { restaurant_id: null }),
        },
    });
}

export async function addVenueToMeet(selectedVenueId, meetId) {
    if (!selectedVenueId) {
        return;
    }
    assertCurrentUserPermission(PERMISSIONS.canAddPubToPoll, "adding a venue to a poll");
    await requestLastOrders(`api/meet/${encodeURIComponent(meetId)}/candidates/${encodeURIComponent(selectedVenueId)}`, {
        method: "PUT",
    });
}

export async function removeVenueFromMeet(meetId, venueId) {
    assertCurrentUserPermission(PERMISSIONS.canAddPubToPoll, "deleting a venue from a poll");
    await requestLastOrders(`api/meet/${encodeURIComponent(meetId)}/candidates/${encodeURIComponent(venueId)}`, {
        method: "DELETE",
    });
}

export async function completeMeetVoting(selectedVenueId, meetId, pollDate, restaurantId, restaurantTime) {
    assertCurrentUserPermission(PERMISSIONS.canCompletePoll, "completing a poll");
    await requestLastOrders(`api/meet/${encodeURIComponent(meetId)}/complete`, {
        method: "POST",
        body: {
            venue_id: selectedVenueId,
            ...(restaurantId ? { restaurant_id: restaurantId, restaurant_time: restaurantTime } : {}),
        },
    });
};
