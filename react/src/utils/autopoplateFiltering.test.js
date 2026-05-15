// @ts-check
import { describe, it, expect, beforeEach, vi } from "vitest";
import {
    getDateWeeksAgo,
    filterVenuesByRecentVisits,
    selectRandomFromArray,
    chooseAutopopulateVenueIds,
} from "./autopoplateFiltering";

describe("autopoplateFiltering utilities", () => {
    describe("getDateWeeksAgo", () => {
        it("should return a date 4 weeks in the past", () => {
            // This is a relative test; we verify the date is approximately 28 days ago
            const result = getDateWeeksAgo(4);
            const resultDate = new Date(result);
            const today = new Date();
            const fourWeeksAgo = new Date(today);
            fourWeeksAgo.setDate(fourWeeksAgo.getDate() - 28);

            const resultIso = resultDate.toISOString().slice(0, 10);
            const expectedIso = fourWeeksAgo.toISOString().slice(0, 10);

            expect(resultIso).toBe(expectedIso);
        });

        it("should return today's date for 0 weeks", () => {
            const result = getDateWeeksAgo(0);
            const today = new Date().toISOString().slice(0, 10);
            expect(result).toBe(today);
        });

        it("should return ISO format (YYYY-MM-DD)", () => {
            const result = getDateWeeksAgo(2);
            expect(result).toMatch(/^\d{4}-\d{2}-\d{2}$/);
        });

        it("should handle invalid weekCount by normalizing to 0", () => {
            const resultNegative = getDateWeeksAgo(-5);
            const resultToday = getDateWeeksAgo(0);
            expect(resultNegative).toBe(resultToday);
        });
    });

    describe("filterVenuesByRecentVisits", () => {
        it("should exclude venues with lastDate >= cutoffDate", () => {
            const rows = [
                { id: "v1", label: "Venue 1", count: 5, lastWonDate: null },
                { id: "v2", label: "Venue 2", count: 3, lastWonDate: null },
                { id: "v3", label: "Venue 3", count: 2, lastWonDate: null },
            ];
            const lastDateByVenueId = {
                v1: "2026-05-03", // Recent (after cutoff)
                v2: "2026-04-20", // Old (before cutoff)
                v3: "2026-05-05", // Recent (after cutoff)
            };
            const cutoffDate = "2026-04-27"; // 2 weeks ago

            const result = filterVenuesByRecentVisits(rows, lastDateByVenueId, cutoffDate);

            expect(result).toHaveLength(1);
            expect(result[0].id).toBe("v2");
        });

        it("should include venues with no recorded date", () => {
            const rows = [
                { id: "v1", label: "Venue 1", count: 5, lastWonDate: null },
                { id: "v2", label: "Venue 2", count: 3, lastWonDate: null },
            ];
            const lastDateByVenueId = {
                v1: null,
                // v2 not in map
            };
            const cutoffDate = "2026-04-27";

            const result = filterVenuesByRecentVisits(rows, lastDateByVenueId, cutoffDate);

            expect(result).toHaveLength(2);
        });

        it("should exclude venues with lastDate equal to cutoffDate", () => {
            const rows = [{ id: "v1", label: "Venue 1", count: 5, lastWonDate: null }];
            const lastDateByVenueId = { v1: "2026-04-27" };
            const cutoffDate = "2026-04-27";

            const result = filterVenuesByRecentVisits(rows, lastDateByVenueId, cutoffDate);

            expect(result).toHaveLength(0);
        });

        it("should handle empty rows", () => {
            const result = filterVenuesByRecentVisits([], {}, "2026-04-27");
            expect(result).toHaveLength(0);
        });
    });

    describe("selectRandomFromArray", () => {
        it("should return an item from the array", () => {
            const array = ["a", "b", "c"];
            const result = selectRandomFromArray(array);
            expect(array).toContain(result);
        });

        it("should return null for empty array", () => {
            const result = selectRandomFromArray([]);
            expect(result).toBeNull();
        });

        it("should return null for null input", () => {
            const result = selectRandomFromArray(null);
            expect(result).toBeNull();
        });

        it("should return single item for single-element array", () => {
            const result = selectRandomFromArray([42]);
            expect(result).toBe(42);
        });

        it("should select different items with reasonable distribution", () => {
            const array = ["a", "b", "c"];
            const counts = { a: 0, b: 0, c: 0 };
            for (let i = 0; i < 300; i++) {
                const item = selectRandomFromArray(array);
                counts[item]++;
            }
            // With 300 samples, expect each item roughly 100 times (+/- 50%)
            expect(counts.a).toBeGreaterThan(50);
            expect(counts.b).toBeGreaterThan(50);
            expect(counts.c).toBeGreaterThan(50);
        });
    });

    describe("chooseAutopopulateVenueIds", () => {
        it("picks one unique venue from each category", () => {
            const result = chooseAutopopulateVenueIds(
                [{ id: "v1" }],
                [{ id: "v2" }],
                [{ id: "v3" }],
                {}
            );

            expect(result).toEqual(["v1", "v2", "v3"]);
        });

        it("does not pick existing poll venues", () => {
            const result = chooseAutopopulateVenueIds(
                [{ id: "v1" }],
                [{ id: "v2" }],
                [{ id: "v3" }],
                { v1: {} }
            );

            expect(result).toEqual(["v2", "v3"]);
        });

        it("does not duplicate across overlapping categories", () => {
            const result = chooseAutopopulateVenueIds(
                [{ id: "v1" }],
                [{ id: "v1" }],
                [{ id: "v1" }],
                {}
            );

            expect(result).toEqual(["v1"]);
        });

        it("backfills to three unique venues when overlap occurs", () => {
            const result = chooseAutopopulateVenueIds(
                [{ id: "v1" }],
                [{ id: "v1" }],
                [{ id: "v1" }, { id: "v2" }, { id: "v3" }],
                {}
            );

            expect(result).toHaveLength(3);
            expect(new Set(result).size).toBe(3);
            expect(result).toEqual(expect.arrayContaining(["v1", "v2", "v3"]));
        });

        it("returns all remaining unique venues when fewer than three exist", () => {
            const result = chooseAutopopulateVenueIds(
                [{ id: "v1" }],
                [{ id: "v1" }],
                [{ id: "v2" }],
                {}
            );

            expect(result).toHaveLength(2);
            expect(new Set(result).size).toBe(2);
            expect(result).toEqual(expect.arrayContaining(["v1", "v2"]));
        });
    });
});
