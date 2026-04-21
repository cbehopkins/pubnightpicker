// @ts-check

import { compareVenueNames } from "../../utils/venueSort";

/** @typedef {{ name?: string }} PubOptionEntry */
/** @typedef {Record<string, PubOptionEntry | undefined>} PubOptionsMap */

/**
 * @typedef {Object} PubOptionsProps
 * @property {PubOptionsMap} pub_parameters
 * @property {string=} optionText
 * @property {(event: import("react").ChangeEvent<HTMLSelectElement>) => void} selectPubHandler
 */

/** @param {PubOptionsProps} props */
function PubOptions({ pub_parameters, optionText, selectPubHandler }) {
  /** @type {[string, PubOptionEntry][]} */
  const sortedPubsByName = Object.entries(pub_parameters)
    .map(([id, pub]) => [id, pub || {}])
    .sort(([idA, pubA], [idB, pubB]) => {
      const byName = compareVenueNames(pubA?.name, pubB?.name);
      if (byName !== 0) {
        return byName;
      }
      return idA.localeCompare(idB, undefined, { sensitivity: "base", numeric: true });
    });

  const optionTextI = optionText || "Select a venue to add here";
  return (
    <select defaultValue="" onChange={selectPubHandler}>
      <option value="">{optionTextI}</option>
      {sortedPubsByName.map(([id, pub]) => (
        <option key={id} value={id}>{pub.name || id}</option>
      ))}
    </select>
  );
}
export default PubOptions;
