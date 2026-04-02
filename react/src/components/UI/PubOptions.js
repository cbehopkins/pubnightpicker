// @ts-check

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

  /** @type {{ sortBy: string, id: string, pub: PubOptionEntry }[]} */
  const sortableEntries = Object.entries(pub_parameters).map(([id, pub]) => {
    const safePub = pub || {};
    const name = typeof safePub.name === "string" ? safePub.name : "";
    return {
      sortBy: name.toLowerCase().replace("the ", ""),
      id,
      pub: safePub,
    };
  });

  /** @type {[string, PubOptionEntry][]} */
  const sortedPubsByName = sortableEntries
    .sort((a, b) => a.sortBy.localeCompare(b.sortBy))
    .map(({ id, pub }) => [id, pub]);

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
