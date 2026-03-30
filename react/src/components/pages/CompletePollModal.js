import ConfirmModal from "../UI/ConfirmModal";

function CompletePollModal({
  pubName,
  restaurantOptions,
  chosenRestaurantId,
  restaurantChoiceRequired,
  onRestaurantChange,
  onConfirm,
  onCancel,
}) {
  const selectedRestaurant = restaurantOptions.find((restaurant) => restaurant.id === chosenRestaurantId);
  const autoRestaurant = !restaurantChoiceRequired && restaurantOptions.length === 1
    ? restaurantOptions[0]
    : null;

  return (
    <ConfirmModal
      title="Complete this poll?"
      detail={<>
        <p>Selected venue: {pubName}</p>
        {restaurantChoiceRequired && (
          <div>
            <label htmlFor="restaurant-choice">Pick the restaurant for this event: </label>
            <select
              id="restaurant-choice"
              value={chosenRestaurantId}
              onChange={(event) => {
                onRestaurantChange(event.target.value);
              }}
            >
              <option value="">Select restaurant</option>
              {restaurantOptions.map((restaurant) => {
                return <option key={restaurant.id} value={restaurant.id}>{restaurant.name}</option>;
              })}
            </select>
          </div>
        )}
        {autoRestaurant && (
          <p>Restaurant association (automatic): {pubName} + {autoRestaurant.name}</p>
        )}
        {!autoRestaurant && selectedRestaurant && (
          <p>Restaurant association: {pubName} + {selectedRestaurant.name}</p>
        )}
      </>}
      confirm_disabled={restaurantChoiceRequired && !chosenRestaurantId}
      on_confirm={onConfirm}
      on_cancel={onCancel}
    />
  );
}

export default CompletePollModal;
