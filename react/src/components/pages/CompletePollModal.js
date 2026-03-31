import ConfirmModal from "../UI/ConfirmModal";

function CompletePollModal({
  pubName,
  restaurantOptions,
  chosenRestaurantId,
  restaurantTime,
  hasRestaurantAssociation,
  restaurantChoiceRequired,
  onRestaurantChange,
  onRestaurantTimeChange,
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
        {hasRestaurantAssociation && (
          <div>
            <label htmlFor="restaurant-time">Restaurant meetup time: </label>
            <input
              id="restaurant-time"
              type="time"
              value={restaurantTime}
              onChange={(event) => {
                onRestaurantTimeChange(event.target.value);
              }}
              required
            />
          </div>
        )}
        {autoRestaurant && (
          <p>Restaurant association (automatic): {pubName} + {autoRestaurant.name}</p>
        )}
        {!autoRestaurant && selectedRestaurant && (
          <p>Restaurant association: {pubName} + {selectedRestaurant.name}</p>
        )}
      </>}
      confirm_disabled={
        (restaurantChoiceRequired && !chosenRestaurantId)
        || (hasRestaurantAssociation && !restaurantTime)
      }
      on_confirm={onConfirm}
      on_cancel={onCancel}
    />
  );
}

export default CompletePollModal;
