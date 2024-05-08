
import { PubParams, PubCheckbox } from "../pages/PubForm";
import styles from "./ActivePoll.module.css";

function PubFilter({ title, set_pub_filters, pub_params = null, label_mod = "" }) {
  const checkHandler = (event, name) => {
    set_pub_filters((prevPubFilters) => {
      return { ...prevPubFilters, [name]: event.target.checked };
    });
  };
  const theParams = pub_params || PubParams

  return (
    <div>
      <p>{title}</p>
      <table className={styles.checkboxes}>
        <tbody>
          {Object.entries(theParams).map(([key, value]) => {
            return (
              <PubCheckbox
                key={key}
                name={key}
                label={value}
                label_mod={label_mod}
                onChange={checkHandler}
              />
            );
          })}
        </tbody>
      </table>
    </div>
  );
}
export default PubFilter;