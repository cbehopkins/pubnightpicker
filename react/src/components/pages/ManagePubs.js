import { NavLink } from "react-router-dom";
import { useState } from "react";
import styles from "./ManagePubs.module.css";
import { usePubsState } from "../../hooks/usePubs";
import useRole from "../../hooks/useRole";
import Button from "../UI/Button";
import { deletePub } from "../../dbtools/pubs";
import { getUserFacingErrorMessage } from "../../permissions";
import { notifyError } from "../../utils/notify";
import { compareVenueNames } from "../../utils/venueSort";
import { DEFAULT_VENUE_TYPE, VENUE_TYPE_OPTIONS, getVenueTypeLabel } from "../../constants/venueTypes";

const ManagePubs = () => {
  const [venueTypeFilter, setVenueTypeFilter] = useState("all");
  const [deleteReviews, setDeleteReviews] = useState({});
  const { venues: pub_parameters, isLoading, isStale, errorMessage, retry } = usePubsState();
  const sortedPubsByName = Object.entries(pub_parameters)
    .filter(([, value]) => {
      if (venueTypeFilter === "all") {
        return true;
      }
      return (value.venueType || DEFAULT_VENUE_TYPE) === venueTypeFilter;
    })
    .sort(([keyA, valueA], [keyB, valueB]) => {
      const byName = compareVenueNames(valueA?.name, valueB?.name);
      if (byName !== 0) {
        return byName;
      }

      return keyA.localeCompare(keyB, undefined, { sensitivity: "base", numeric: true });
    })
    .map(([key, value]) => [value.name, key]);

  const canManagePubs = useRole("canManagePubs");
  return (
    <div className="container py-3">
      <div className="d-flex flex-wrap align-items-center gap-2 mb-3">
        {canManagePubs && (
          <NavLink className="btn btn-primary" to="/venues/new">
            New Venue
          </NavLink>
        )}
      </div>

      {isLoading && <p role="status">Loading venues...</p>}
      {errorMessage && (
        <div className="alert alert-warning" role="alert">
          <p className="mb-2">{errorMessage}</p>
          <Button type="button" variant="secondary" size="sm" onClick={retry}>Retry</Button>
        </div>
      )}
      {isStale && (
        <p className="alert alert-warning py-2" role="status">
          Showing the last loaded venue list. Updates may be delayed while the connection recovers.
        </p>
      )}

      <div className={`${styles.filterRow} text-body mb-3`}>
        <label htmlFor="venue-type-filter" className="fw-semibold">Filter by venue type:</label>
        <select
          id="venue-type-filter"
          className="form-select w-auto"
          value={venueTypeFilter}
          onChange={(event) => {
            setVenueTypeFilter(event.target.value);
          }}
        >
          {VENUE_TYPE_OPTIONS.map((venueType) => {
            return <option key={venueType} value={venueType}>{getVenueTypeLabel(venueType)}</option>;
          })}
        </select>
      </div>

      <div className={`${styles.content} d-flex flex-column gap-2`}>
        {sortedPubsByName.map(([pubName, key]) => {
          const currentVenue = deleteReviews[key] ?? pub_parameters[key];
          const displayName = currentVenue?.name ?? pubName;
          return (
            <div key={key} className="d-flex align-items-center gap-2 flex-wrap">
              {canManagePubs && (
                <Button
                  disabled={!canManagePubs}
                  variant="danger"
                  size="sm"
                  onClick={async () => {
                    try {
                      await deletePub(key, currentVenue?.delete_token);
                      setDeleteReviews((reviews) => {
                        const nextReviews = { ...reviews };
                        delete nextReviews[key];
                        return nextReviews;
                      });
                    } catch (error) {
                      if (error?.code === "precondition_failed" && error.currentVenue?.id === key) {
                        setDeleteReviews((reviews) => ({ ...reviews, [key]: error.currentVenue }));
                        return;
                      }
                      notifyError(getUserFacingErrorMessage(error, "Unable to delete this pub."));
                    }
                  }}
                >
                  {deleteReviews[key] ? "Delete reviewed venue" : "Delete"}
                </Button>
              )}
              {canManagePubs ? (
                <NavLink
                  className="link-primary link-underline-opacity-0 link-underline-opacity-100-hover fw-medium"
                  to={`/venues/${key}`}
                >
                  {displayName}
                </NavLink>
              ) : (
                <span className="text-body">{displayName}</span>
              )}
              {deleteReviews[key] && (
                <div className="alert alert-warning w-100 mb-0" role="alert">
                  <p className="mb-1">This venue changed after it was loaded. Review the current details before choosing Delete reviewed venue.</p>
                  <p className="mb-0">
                    <strong>{displayName}</strong>
                    {currentVenue.venue_type && ` (${currentVenue.venue_type})`}
                    {currentVenue.address && `, ${currentVenue.address}`}
                    {currentVenue.notes && `. ${currentVenue.notes}`}
                  </p>
                </div>
              )}
            </div>
          );
        })}
      </div>
    </div>
  );
};

export default ManagePubs;
