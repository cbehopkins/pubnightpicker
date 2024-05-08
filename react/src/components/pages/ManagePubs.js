import { useSelector } from "react-redux";
import { NavLink } from "react-router-dom";
import { doc, deleteDoc } from "firebase/firestore";
import { db } from "../../firebase";
import styles from "./ManagePubs.module.css";
import usePubs from "../../hooks/usePubs";
import useAdmin from "../../hooks/useAdmin";

const ManagePubs = (params) => {
  const loggedIn = useSelector((state) => state.auth.loggedIn);
  const deletePub = async (id, name) => {
    await deleteDoc(doc(db, "pubs", id));
  };
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

  const admin = useAdmin() && loggedIn;
  return (
    <div className={styles.navlink}>
      {admin && (
        <NavLink className={styles.navlink} to="/pubs/new">
          New Pub
        </NavLink>
      )}
      <div className={styles.content}>
        {sortedPubsByName.map(([, pubName, key]) => {
          return (
            <div key={key} className={styles.navlink}>
              {admin && (
                <button
                  disabled={!admin}
                  className={styles.delete}
                  onClick={() => {
                    deletePub(key, pubName);
                  }}
                >
                  Delete
                </button>
              )}
              {admin ? (
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
