import { NavLink } from "react-router-dom";
import styles from "./ManagePubs.module.css";
import usePubs from "../../hooks/usePubs";
import useRole from "../../hooks/useRole";
import { deletePub } from "../../dbtools/pubs";
import { getUserFacingErrorMessage } from "../../permissions";
import { notifyError } from "../../utils/notify";

const ManagePubs = (params) => {
  // This mess is the best way I can find to get the pubs printed out
  // ordered by name
  // First get a list that of [pubName, key] sorted by pubname
  const pub_parameters = usePubs();
  const sortedPubsByName = Object.entries(pub_parameters)
    .map(([key, value]) => {
      const sortValue = value.name.replace("The ", "");
      return [sortValue, value.name, key];
    })
    .sort();

  const canManagePubs = useRole("canManagePubs");
  return (
    <div className={styles.navlink}>
      {canManagePubs && (
        <NavLink className={styles.navlink} to="/pubs/new">
          New Pub
        </NavLink>
      )}
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
                <NavLink to={`/pubs/${key}`}>{pubName}</NavLink>
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
