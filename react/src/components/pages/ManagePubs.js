import { NavLink } from "react-router-dom";
import { useState } from "react";
import styles from "./ManagePubs.module.css";
import usePubs from "../../hooks/usePubs";
import useRole from "../../hooks/useRole";
import Button from "../UI/Button";
import { deletePub } from "../../dbtools/pubs";
import { getUserFacingErrorMessage } from "../../permissions";
import { notifyError } from "../../utils/notify";

const venueTypeOptions = ["all", "pub", "restaurant", "event"];

const ManagePubs = (params) => {
  const [venueTypeFilter, setVenueTypeFilter] = useState("all");
  // This mess is the best way I can find to get the pubs printed out
  // ordered by name
  // First get a list that of [pubName, key] sorted by pubname
  const pub_parameters = usePubs();
  const sortedPubsByName = Object.entries(pub_parameters)
    .filter(([, value]) => {
      if (venueTypeFilter === "all") {
        return true;
      }
      return (value.venueType || "pub") === venueTypeFilter;
    })
    .map(([key, value]) => {
      const sortValue = value.name.replace("The ", "");
      return [sortValue, value.name, key];
    })
    .sort();

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

      <div className={`${styles.filterRow} text-dark mb-3`}>
        <label htmlFor="venue-type-filter" className="fw-semibold">Filter by venue type:</label>
        <select
          id="venue-type-filter"
          className="form-select w-auto"
          value={venueTypeFilter}
          onChange={(event) => {
            setVenueTypeFilter(event.target.value);
          }}
        >
          {venueTypeOptions.map((venueType) => {
            const label = venueType === "all"
              ? "All Types"
              : venueType.charAt(0).toUpperCase() + venueType.slice(1);
            return <option key={venueType} value={venueType}>{label}</option>;
          })}
        </select>
      </div>

      <div className={`${styles.content} d-flex flex-column gap-2`}>
        {sortedPubsByName.map(([, pubName, key]) => {
          return (
            <div key={key} className="d-flex align-items-center gap-2 flex-wrap">
              {canManagePubs && (
                <Button
                  disabled={!canManagePubs}
                  variant="danger"
                  size="sm"
                  onClick={async () => {
                    try {
                      await deletePub(key);
                    } catch (error) {
                      notifyError(getUserFacingErrorMessage(error, "Unable to delete this pub."));
                    }
                  }}
                >
                  Delete
                </Button>
              )}
              {canManagePubs ? (
                <NavLink className="link-light link-underline-opacity-0 link-underline-opacity-100-hover" to={`/venues/${key}`}>
                  {pubName}
                </NavLink>
              ) : (
                <span className="text-light">{pubName}</span>
              )}
            </div>
          );
        })}
      </div>
    </div>
  );
};

export default ManagePubs;
