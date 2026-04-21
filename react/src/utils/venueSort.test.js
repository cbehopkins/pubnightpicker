import { describe, expect, it } from "vitest";
import {
  compareVenueNames,
  normalizeVenueNameForSort,
  sortVenueOptionsByName,
} from "./venueSort";

describe("normalizeVenueNameForSort", () => {
  it("removes a leading The (case-insensitive) before sorting", () => {
    expect(normalizeVenueNameForSort("The Maypole")).toBe("maypole");
    expect(normalizeVenueNameForSort(" the Anchor")).toBe("anchor");
  });
});

describe("compareVenueNames", () => {
  it("sorts names while ignoring leading The", () => {
    expect(compareVenueNames("The Anchor", "Beer House")).toBeLessThan(0);
  });
});

describe("sortVenueOptionsByName", () => {
  it("sorts options using the shared venue comparator", () => {
    const options = [
      { id: "3", name: "Beer House" },
      { id: "1", name: "The Anchor" },
      { id: "2", name: "The Maypole" },
    ];

    expect(sortVenueOptionsByName(options)).toEqual([
      { id: "1", name: "The Anchor" },
      { id: "3", name: "Beer House" },
      { id: "2", name: "The Maypole" },
    ]);
  });
});