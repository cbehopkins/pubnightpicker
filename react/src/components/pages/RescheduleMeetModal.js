// @ts-check

import VenueAssignmentModal from "./VenueAssignmentModal";

/**
 * @typedef {Object} VenueOption
 * @property {string} id
 * @property {string} name
 */

/**
 * @typedef {Object} RescheduleMeetModalProps
 * @property {string=} selectedVenueId
 * @property {boolean=} selectedVenueHasFood
 * @property {VenueOption[]} mainVenueOptions
 * @property {VenueOption[]} restaurantOptions
 * @property {string=} chosenRestaurantId
 * @property {string=} restaurantTime
 * @property {(value: string) => void} onVenueChange
 * @property {(value: string) => void} onRestaurantChange
 * @property {(value: string) => void} onRestaurantTimeChange
 * @property {() => void | Promise<void>} onConfirm
 * @property {() => void | Promise<void>} onCancel
 */

/**
 * @param {RescheduleMeetModalProps} props
 */
function RescheduleMeetModal({
  selectedVenueId,
  selectedVenueHasFood,
  mainVenueOptions,
  restaurantOptions,
  chosenRestaurantId,
  restaurantTime,
  onVenueChange,
  onRestaurantChange,
  onRestaurantTimeChange,
  onConfirm,
  onCancel,
}) {
  const infoNote = selectedVenueHasFood
    ? "This venue serves food, so a separate restaurant is optional."
    : "Pick the fallback venue and optionally add or change the restaurant.";

  return (
    <VenueAssignmentModal
      title="Reschedule Meet"
      subtitle="Update the fallback plan by choosing a new main venue and, if needed, a restaurant."
      mainVenueLabel="Main venue"
      mainVenueOptions={mainVenueOptions}
      mainVenuePlaceholder="Select a main venue"
      selectedMainVenueId={selectedVenueId}
      onMainVenueChange={onVenueChange}
      mainVenueHelpText="You can move the meet to any non-restaurant venue in the system."
      infoNote={infoNote}
      showRestaurantSection={true}
      restaurantLabel="Restaurant"
      restaurantOptions={restaurantOptions}
      restaurantHelpText="Change the restaurant independently of the main venue, or leave it as No restaurant."
      chosenRestaurantId={chosenRestaurantId}
      restaurantTime={restaurantTime}
      timeHelpText="This is only stored when a restaurant is selected."
      onRestaurantChange={onRestaurantChange}
      onRestaurantTimeChange={onRestaurantTimeChange}
      confirmText="Save Changes"
      footerNote="Only the latest main venue and restaurant choices will be kept on the meet."
      onConfirm={onConfirm}
      onCancel={onCancel}
    />
  );
}

export default RescheduleMeetModal;
