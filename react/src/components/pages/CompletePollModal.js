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
  return (
    <ConfirmModal
      title="Complete this poll?"
      detail={<>
        <p>{pubName}</p>
        {restaurantChoiceRequired && (
          <p>
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
          </p>
        )}
      </>}
      confirm_disabled={restaurantChoiceRequired && !chosenRestaurantId}
      on_confirm={onConfirm}
      on_cancel={onCancel}
    />
  );
}

export default CompletePollModal;
