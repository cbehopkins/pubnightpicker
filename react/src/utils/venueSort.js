const LEADING_THE_REGEX = /^\s*the\s+/i;

/**
 * @param {string | undefined | null} name
 * @returns {string}
 */
export function normalizeVenueNameForSort(name) {
  const safeName = typeof name === "string" ? name.trim() : "";
  return safeName.replace(LEADING_THE_REGEX, "").toLocaleLowerCase();
}

/**
 * Compare venue names alphabetically while ignoring a leading "The".
 * @param {string | undefined | null} nameA
 * @param {string | undefined | null} nameB
 * @returns {number}
 */
export function compareVenueNames(nameA, nameB) {
  const safeA = typeof nameA === "string" ? nameA : "";
  const safeB = typeof nameB === "string" ? nameB : "";

  const normalizedCompare = normalizeVenueNameForSort(safeA).localeCompare(
    normalizeVenueNameForSort(safeB),
    undefined,
    { sensitivity: "base", numeric: true },
  );

  if (normalizedCompare !== 0) {
    return normalizedCompare;
  }

  const rawCompare = safeA.localeCompare(safeB, undefined, {
    sensitivity: "base",
    numeric: true,
  });
  if (rawCompare !== 0) {
    return rawCompare;
  }

  return safeA.localeCompare(safeB, undefined, { numeric: true });
}

/**
 * @template {{ name?: string }} T
 * @param {T[]} options
 * @returns {T[]}
 */
export function sortVenueOptionsByName(options) {
  options.sort((a, b) => compareVenueNames(a?.name, b?.name));
  return options;
}