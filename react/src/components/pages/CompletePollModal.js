// @ts-check

import { useCallback, useEffect } from "react";
import VenueAssignmentModal from "./VenueAssignmentModal";
import { useNotificationPing } from "../../hooks/useNotificationPing";

/** @typedef {{ id: string, name: string }} VenueOption */

/**
 * @param {{
 *  pollId: string,
 *  pubName: string,
 *  pubHasFood: boolean,
 *  availableRestaurants: VenueOption[],
 *  restaurantSource: "poll" | "system",
 *  chosenRestaurantId: string,
 *  restaurantTime: string,
 *  onRestaurantChange: (restaurantId: string) => void,
 *  onRestaurantTimeChange: (value: string) => void,
 *  onConfirm: () => void,
 *  onCancel: () => void,
 * }} props
 */
function CompletePollModal({
  pollId,
  pubName,
  pubHasFood,
  availableRestaurants,
  restaurantSource,
  chosenRestaurantId,
  restaurantTime,
  onRestaurantChange,
  onRestaurantTimeChange,
  onConfirm,
  onCancel,
}) {
  const { status, runPing } = useNotificationPing(pollId, "complete", 60000);

  useEffect(() => {
    runPing().catch(() => {
      // The status hook already records error/timeout state for the badge.
    });
  }, [runPing]);

  const badgeClassName =
    status === "ok"
      ? "bg-success"
      : status === "checking"
        ? "bg-warning text-dark"
        : status === "timeout" || status === "error"
          ? "bg-danger"
          : "bg-secondary";

  const statusLabel =
    status === "ok"
      ? "Notification Tool: OK"
      : status === "checking"
        ? "Notification Tool: Checking..."
        : status === "timeout"
          ? "Notification Tool: Timeout"
          : status === "error"
            ? "Notification Tool: Error"
            : "Notification Tool: Not Checked";

  const handleConfirm = useCallback(async () => {
    if (status !== "ok") {
      const proceed = window.confirm(
        "Notification tool has not acknowledged the completion handshake yet. Completing now may skip notification processing. Do you want to continue?"
      );
      if (!proceed) {
        return;
      }
    }

    if (!pubHasFood && !chosenRestaurantId) {
      const proceedWithoutFoodPlan = window.confirm(
        "This venue does not serve food and no restaurant is selected. Do you want to complete this poll without a food plan?"
      );
      if (!proceedWithoutFoodPlan) {
        return;
      }
    }

    await onConfirm();
  }, [chosenRestaurantId, onConfirm, pubHasFood, status]);

  const restaurantSectionLabel =
    restaurantSource === "poll"
      ? "Restaurant from this poll"
      : "Add a restaurant to this event";

  return (
    <VenueAssignmentModal
      title="Complete Poll"
      subtitle="Confirm the venue for this event and add any final food plan details."
      footerStatusNode={<span className={`badge ${badgeClassName}`}>{statusLabel}</span>}
      mainVenueLabel="Selected venue"
      mainVenueName={pubName}
      onMainVenueChange={() => { }}
      mainVenueHelpText="This is the venue that will be stored as the event destination."
      infoNote={pubHasFood ? "This venue serves food - no separate restaurant needed." : undefined}
      showRestaurantSection={!pubHasFood}
      restaurantLabel={restaurantSectionLabel}
      restaurantOptions={availableRestaurants}
      restaurantHelpText="Choose a restaurant if the group is eating separately. Leave this as No restaurant to skip it."
      chosenRestaurantId={chosenRestaurantId}
      restaurantTime={restaurantTime}
      timeHelpText="Defaults to 18:30, but you can adjust it before confirming."
      footerNote="Saving without a restaurant leaves the completed poll without restaurant details."
      onRestaurantChange={onRestaurantChange}
      onRestaurantTimeChange={onRestaurantTimeChange}
      onConfirm={handleConfirm}
      onCancel={onCancel}
    />
  );
}

export default CompletePollModal;
