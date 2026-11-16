
// @ts-check

import { useId } from "react";
import { Form } from "react-bootstrap";
import styles from "./ActivePoll.module.css";

const venueParams = {
  parking: "Parking",
  food: "Food",
  dogFriendly: "Dog Friendly",
  beerGarden: "Beer Garden",
  outOfTown: "Out Of Town (Cambridge)",
  banned: "Banned",
};
export const antiVenueParams = { outOfTown: venueParams.outOfTown };

/** @typedef {Record<string, string>} PubFilterParamsMap */

/**
 * @typedef {Object} PubFilterProps
 * @property {string} title
 * @property {import("react").Dispatch<import("react").SetStateAction<Record<string, boolean>>>} set_pub_filters
 * @property {PubFilterParamsMap | null=} pub_params
 * @property {string=} label_mod
 */

/** @param {PubFilterProps} props */
function PubFilter({ title, set_pub_filters, pub_params = null, label_mod = "" }) {
  const filterId = useId();
  /**
   * @param {import("react").ChangeEvent<HTMLInputElement>} event
   * @param {string} name
   */
  const checkHandler = (event, name) => {
    set_pub_filters((prevPubFilters) => {
      return { ...prevPubFilters, [name]: event.target.checked };
    });
  };
  const theParams = pub_params || venueParams;

  return (
    <div>
      <p>{title}</p>
      <table className={styles.checkboxes}>
        <tbody>
          {Object.entries(theParams).map(([key, value]) => {
            const inputId = `${filterId}-${label_mod}${key}`;
            return (
              <tr key={key}>
                <td className="text-center" style={{ width: "3rem" }}>
                  <Form.Check id={inputId} type="checkbox" name={inputId} aria-label={value}
                    onChange={(event) => checkHandler(event, key)} />
                </td>
                <td><Form.Label htmlFor={inputId} className="mb-0 text-body-emphasis">{value}</Form.Label></td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}
export default PubFilter;
