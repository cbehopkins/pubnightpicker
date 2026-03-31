import Modal from "../UI/Modal";
import styles from "./CompletePollModal.module.css";

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
  const confirmDisabled = Boolean(chosenRestaurantId && !restaurantTime);

  const restaurantSectionLabel =
    restaurantSource === "poll"
      ? "Restaurant from this poll"
      : "Add a restaurant to this event";

  return (
    <Modal>
      <div className={styles.dialog}>
        <div className={styles.header}>
          <p className={styles.title}>Complete Poll</p>
        </div>

        <div className={styles.venueRow}>
          <span className={styles.venueLabel}>Selected venue</span>
          <span className={styles.venueName}>{pubName}</span>
        </div>

        {pubHasFood && (
          <p className={styles.infoNote}>
            This venue serves food — no separate restaurant needed.
          </p>
        )}

        {!pubHasFood && (
          <div className={styles.formSection}>
            <label className={styles.fieldLabel} htmlFor="restaurant-choice">
              {restaurantSectionLabel}
            </label>
            <select
              id="restaurant-choice"
              className={styles.select}
              value={chosenRestaurantId}
              onChange={(e) => onRestaurantChange(e.target.value)}
            >
              <option value="">No restaurant</option>
              {availableRestaurants.map((r) => (
                <option key={r.id} value={r.id}>{r.name}</option>
              ))}
            </select>
          </div>
        )}

        {chosenRestaurantId && (
          <div className={styles.formSection}>
            <label className={styles.fieldLabel} htmlFor="restaurant-time">
              Restaurant meetup time
            </label>
            <input
              id="restaurant-time"
              type="time"
              className={styles.timeInput}
              value={restaurantTime}
              onChange={(e) => onRestaurantTimeChange(e.target.value)}
              required
            />
          </div>
        )}

        <div className={styles.footer}>
          <button className={styles.btnCancel} onClick={onCancel}>Cancel</button>
          <button className={styles.btnConfirm} onClick={onConfirm} disabled={confirmDisabled}>
            Confirm
          </button>
        </div>
      </div>
    </Modal>
  );
}

export default CompletePollModal;
