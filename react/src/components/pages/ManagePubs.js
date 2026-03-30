import { NavLink } from "react-router-dom";
import { useState } from "react";
import styles from "./ManagePubs.module.css";
import usePubs from "../../hooks/usePubs";
import useRole from "../../hooks/useRole";
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
    <div className={styles.navlink}>
      {canManagePubs && (
        <NavLink className={styles.navlink} to="/venues/new">
          New Venue
        </NavLink>
      )}
      <div className={styles.filterRow}>
        <label htmlFor="venue-type-filter">Filter by venue type:</label>
        <select
          id="venue-type-filter"
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
      <div className={styles.content}>
        {sortedPubsByName.map(([, pubName, key]) => {
          return (
            <div key={key} className={styles.navlink}>
              {canManagePubs && (
                <button
                  disabled={!canManagePubs}
                  className={styles.delete}
                  onClick={async () => {
                    try {
                      await deletePub(key);
                    } catch (error) {
                      notifyError(getUserFacingErrorMessage(error, "Unable to delete this pub."));
                    }
                  }}
                >
                  Delete
                </button>
              )}
              {canManagePubs ? (
                <NavLink to={`/venues/${key}`}>{pubName}</NavLink>
              ) : (
                <span>{pubName}</span>
              )}
            </div>
          );
        })}
      </div>
    </div>
  );
};

export default ManagePubs;
