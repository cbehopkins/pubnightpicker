// @ts-check

import { useCallback } from "react";
import { addVenueToMeet } from "../dbtools/polls";
import { getUserFacingErrorMessage } from "../permissions";
import { notifyError, notifyInfo } from "../utils/notify";
import { chooseAutopopulateVenueIds } from "../utils/autopoplateFiltering";

/**
 * @param {{
 *  pollId: string,
 *  pollVenues: Record<string, unknown> | undefined,
 *  mostVisited: { id: string }[],
 *  leastVisited: { id: string }[],
 *  randomVenues: { id: string }[],
 * }} params
 */
export default function useAutopopulateAction({
    pollId,
    pollVenues,
    mostVisited,
    leastVisited,
    randomVenues,
}) {
    const handleAutopopulate = useCallback(async () => {
        try {
            const venuesToAdd = chooseAutopopulateVenueIds(
                mostVisited,
                leastVisited,
                randomVenues,
                pollVenues
            );

            if (venuesToAdd.length === 0) {
                notifyInfo("No viable venues were available to auto-add.");
                return;
            }

            let successCount = 0;
            for (const venueId of venuesToAdd) {
                try {
                    await addVenueToMeet(venueId, pollId);
                    successCount++;
                } catch (err) {
                    console.error(`Failed to add venue ${venueId}:`, err);
                }
            }

            if (successCount > 0) {
                notifyInfo(`Added ${successCount} venue${successCount !== 1 ? "s" : ""} to poll`);
            }

            if (successCount < venuesToAdd.length) {
                notifyError(`Only ${successCount} of ${venuesToAdd.length} venues were added. Please try again.`);
            }
        } catch (error) {
            notifyError(getUserFacingErrorMessage(error, "Unable to autopopulate venues."));
        }
    }, [leastVisited, mostVisited, pollId, pollVenues, randomVenues]);

    return { handleAutopopulate };
}
