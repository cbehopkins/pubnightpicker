// @ts-check

import Modal from "../UI/Modal";
import Button from "../UI/Button";

/**
 * @typedef {Object} VenueOption
 * @property {string} id
 * @property {string} name
 */

/**
 * @typedef {Object} VenueAssignmentModalProps
 * @property {string} title
 * @property {string=} subtitle
 * @property {string} mainVenueLabel
 * @property {string=} mainVenueName
 * @property {VenueOption[]=} mainVenueOptions
 * @property {string=} mainVenuePlaceholder
 * @property {string=} selectedMainVenueId
 * @property {(value: string) => void} onMainVenueChange
 * @property {string=} mainVenueHelpText
 * @property {string=} infoNote
 * @property {boolean=} showRestaurantSection
 * @property {string=} restaurantLabel
 * @property {VenueOption[]=} restaurantOptions
 * @property {string=} chosenRestaurantId
 * @property {string=} restaurantTime
 * @property {(value: string) => void=} onRestaurantChange
 * @property {(value: string) => void=} onRestaurantTimeChange
 * @property {string=} restaurantHelpText
 * @property {string=} timeHelpText
 * @property {string=} confirmText
 * @property {string=} cancelText
 * @property {string=} footerNote
 * @property {() => void | Promise<void>} onConfirm
 * @property {() => void | Promise<void>} onCancel
 */

/**
 * @param {VenueAssignmentModalProps} props
 */
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
  onRestaurantChange = () => { },
  onRestaurantTimeChange = () => { },
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
      <div className="p-3 p-md-4 text-body bg-body rounded shadow-sm border">
        <div className="border-bottom pb-3 mb-3">
          <div>
            <p className="text-uppercase fw-semibold small text-secondary mb-1">Event Update</p>
            <h4 className="mb-2">{title}</h4>
            {subtitle && <p className="text-secondary mb-0">{subtitle}</p>}
          </div>
        </div>

        <div className="card mb-3 border-0 bg-light-subtle">
          <div className="card-body">
            <div className="d-flex flex-column gap-2">
              <span className="text-uppercase fw-semibold small text-secondary">{mainVenueLabel}</span>
              {mainVenueOptions ? (
                <select
                  className="form-select"
                  value={selectedMainVenueId}
                  onChange={(event) => onMainVenueChange(event.target.value)}
                >
                  <option value="">{mainVenuePlaceholder}</option>
                  {mainVenueOptions.map((venue) => (
                    <option key={venue.id} value={venue.id}>{venue.name}</option>
                  ))}
                </select>
              ) : (
                <span className="fs-5 fw-semibold">{mainVenueName}</span>
              )}
              {mainVenueHelpText && <p className="small text-secondary mb-0">{mainVenueHelpText}</p>}
            </div>
          </div>
        </div>

        {infoNote && <div className="alert alert-info py-2" role="status">{infoNote}</div>}

        {showRestaurantSection && (
          <div className="card mb-3 border-0 bg-light-subtle">
            <div className="card-body">
              <div className="d-flex flex-column gap-2">
                <label className="form-label mb-0 fw-semibold" htmlFor="restaurant-choice">
                  {restaurantLabel}
                </label>
                <select
                  id="restaurant-choice"
                  className="form-select"
                  value={chosenRestaurantId}
                  onChange={(event) => onRestaurantChange(event.target.value)}
                >
                  <option value="">No restaurant</option>
                  {restaurantOptions.map((restaurant) => (
                    <option key={restaurant.id} value={restaurant.id}>{restaurant.name}</option>
                  ))}
                </select>
                {restaurantHelpText && <p className="small text-secondary mb-0">{restaurantHelpText}</p>}
              </div>
            </div>
          </div>
        )}

        {showRestaurantSection && chosenRestaurantId && (
          <div className="card mb-3 border-0 bg-light-subtle">
            <div className="card-body">
              <div className="d-flex flex-column gap-2">
                <label className="form-label mb-0 fw-semibold" htmlFor="restaurant-time">
                  Restaurant meetup time
                </label>
                <input
                  id="restaurant-time"
                  type="time"
                  className="form-control"
                  value={restaurantTime}
                  onChange={(event) => onRestaurantTimeChange(event.target.value)}
                  required
                />
                <p className="small text-secondary mb-0">{timeHelpText || "Set when the group should meet before moving on."}</p>
              </div>
            </div>
          </div>
        )}

        <div className="d-flex flex-column flex-md-row align-items-stretch align-items-md-center gap-2 pt-2 border-top">
          {footerNote && <p className="small text-secondary mb-0 me-md-auto">{footerNote}</p>}
          <Button type="button" variant="secondary" onClick={onCancel}>{cancelText}</Button>
          <Button type="button" onClick={onConfirm} disabled={confirmDisabled}>{confirmText}</Button>
        </div>
      </div>
    </Modal>
  );
}

export default VenueAssignmentModal;
