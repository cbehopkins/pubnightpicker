// @ts-check

import { compareVenueNames } from "../../utils/venueSort";

/** @typedef {{ name?: string, banned?: boolean }} VenueOptionEntry */
/** @typedef {Record<string, VenueOptionEntry | undefined>} VenueOptionsMap */

/**
 * @typedef {Object} VenueOptionsProps
 * @property {VenueOptionsMap} venues
 * @property {string=} optionText
 * @property {(event: import("react").ChangeEvent<HTMLSelectElement>) => void} selectVenueHandler
 */

/** @param {VenueOptionsProps} props */
function VenueOptions({ venues, optionText, selectVenueHandler }) {
    const sortedVenuesByName = Object.entries(venues)
        .map(([id, venue]) => /** @type {[string, VenueOptionEntry]} */([id, venue || {}]))
        .sort(([idA, venueA], [idB, venueB]) => {
            const byName = compareVenueNames(venueA.name, venueB.name);
            if (byName !== 0) {
                return byName;
            }
            return idA.localeCompare(idB, undefined, { sensitivity: "base", numeric: true });
        });

    const optionTextI = optionText || "Select a venue to add here";
    return (
        <select defaultValue="" onChange={selectVenueHandler}>
            <option value="">{optionTextI}</option>
            {sortedVenuesByName.map(([id, venue]) => (
                <option key={id} value={id} style={venue.banned ? { color: "#8a4b00" } : undefined}>
                    {venue.name || id}
                </option>
            ))}
        </select>
    );
}
export default VenueOptions;
