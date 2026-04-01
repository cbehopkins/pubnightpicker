// @ts-check

import VenueAssignmentModal from "./VenueAssignmentModal";

/** @typedef {{ id: string, name: string }} VenueOption */

/**
 * @param {{
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
  const restaurantSectionLabel =
    restaurantSource === "poll"
      ? "Restaurant from this poll"
      : "Add a restaurant to this event";

  return (
    <VenueAssignmentModal
      title="Complete Poll"
      subtitle="Confirm the venue for this event and add any final food plan details."
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
      onConfirm={onConfirm}
      onCancel={onCancel}
    />
  );
}

export default CompletePollModal;
