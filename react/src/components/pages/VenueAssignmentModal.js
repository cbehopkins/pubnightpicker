import Modal from "../UI/Modal";
import styles from "./CompletePollModal.module.css";

function VenueAssignmentModal({
  title,
  subtitle,
  mainVenueLabel,
  mainVenueName,
  mainVenueOptions,
  mainVenuePlaceholder = "Select venue",
  selectedMainVenueId,
  onMainVenueChange,
  mainVenueHelpText,
  infoNote,
  showRestaurantSection,
  restaurantLabel,
  restaurantOptions,
  chosenRestaurantId,
  restaurantTime,
  onRestaurantChange,
  onRestaurantTimeChange,
  restaurantHelpText,
  timeHelpText,
  confirmText = "Confirm",
  cancelText = "Cancel",
  footerNote,
  onConfirm,
  onCancel,
}) {
  const confirmDisabled = Boolean(
    (mainVenueOptions && !selectedMainVenueId)
    || (showRestaurantSection && chosenRestaurantId && !restaurantTime)
  );

  return (
    <Modal>
      <div className={styles.dialog}>
        <div className={styles.header}>
          <div>
            <p className={styles.eyebrow}>Event Update</p>
            <p className={styles.title}>{title}</p>
            {subtitle && <p className={styles.subtitle}>{subtitle}</p>}
          </div>
        </div>

        <div className={styles.section}>
          <div className={styles.venueRow}>
            <span className={styles.venueLabel}>{mainVenueLabel}</span>
            {mainVenueOptions ? (
              <select
                className={styles.select}
                value={selectedMainVenueId}
                onChange={(event) => onMainVenueChange(event.target.value)}
              >
                <option value="">{mainVenuePlaceholder}</option>
                {mainVenueOptions.map((venue) => (
                  <option key={venue.id} value={venue.id}>{venue.name}</option>
                ))}
              </select>
            ) : (
              <span className={styles.venueName}>{mainVenueName}</span>
            )}
            {mainVenueHelpText && <p className={styles.helpText}>{mainVenueHelpText}</p>}
          </div>
        </div>

        {infoNote && <p className={styles.infoNote}>{infoNote}</p>}

        {showRestaurantSection && (
          <div className={styles.section}>
            <div className={styles.formSection}>
              <label className={styles.fieldLabel} htmlFor="restaurant-choice">
                {restaurantLabel}
              </label>
              <select
                id="restaurant-choice"
                className={styles.select}
                value={chosenRestaurantId}
                onChange={(event) => onRestaurantChange(event.target.value)}
              >
                <option value="">No restaurant</option>
                {restaurantOptions.map((restaurant) => (
                  <option key={restaurant.id} value={restaurant.id}>{restaurant.name}</option>
                ))}
              </select>
              {restaurantHelpText && <p className={styles.helpText}>{restaurantHelpText}</p>}
            </div>
          </div>
        )}

        {showRestaurantSection && chosenRestaurantId && (
          <div className={styles.section}>
            <div className={styles.formSection}>
              <label className={styles.fieldLabel} htmlFor="restaurant-time">
                Restaurant meetup time
              </label>
              <input
                id="restaurant-time"
                type="time"
                className={styles.timeInput}
                value={restaurantTime}
                onChange={(event) => onRestaurantTimeChange(event.target.value)}
                required
              />
              <p className={styles.helpText}>{timeHelpText || "Set when the group should meet before moving on."}</p>
            </div>
          </div>
        )}

        <div className={styles.footer}>
          {footerNote && <p className={styles.footerNote}>{footerNote}</p>}
          <button className={styles.btnCancel} onClick={onCancel}>{cancelText}</button>
          <button className={styles.btnConfirm} onClick={onConfirm} disabled={confirmDisabled}>
            {confirmText}
          </button>
        </div>
      </div>
    </Modal>
  );
}

export default VenueAssignmentModal;