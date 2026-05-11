import { describe, expect, it } from "vitest";
import { buildVenueCountRows, buildWinningVenueRows, splitRankedStatRows } from "./statsRanking";

describe("statsRanking", () => {
    it("ranks winning venues and preserves zero-count venues", () => {
        const rows = buildWinningVenueRows({
            polls: {
                poll1: { selected: "venue-a", date: "2026-01-01" },
                poll2: { selected: "venue-b", date: "2026-01-05" },
                poll3: { selected: "venue-a", date: "2026-02-01" },
            },
            venues: {
                "venue-a": { name: "Alpha" },
                "venue-b": { name: "Beta" },
                "venue-c": { name: "Gamma" },
            },
        });

        expect(rows.map((row) => row.id)).toEqual(["venue-a", "venue-b", "venue-c"]);
        expect(rows[0]).toMatchObject({ id: "venue-a", label: "Alpha", count: 2, lastWonDate: "2026-02-01" });
        expect(rows[2]).toMatchObject({ id: "venue-c", label: "Gamma", count: 0, lastWonDate: null });
    });

    it("splits the top and bottom lists deterministically", () => {
        const rows = buildWinningVenueRows({
            polls: {
                poll1: { selected: "venue-a", date: "2026-01-01" },
                poll2: { selected: "venue-b", date: "2026-01-05" },
            },
            venues: {
                "venue-a": { name: "Alpha" },
                "venue-b": { name: "Beta" },
                "venue-c": { name: "Gamma" },
                "venue-d": { name: "Delta" },
            },
        });

        const { most, least } = splitRankedStatRows(rows, 2);

        expect(most.map((row) => row.id)).toEqual(["venue-a", "venue-b"]);
        expect(least.map((row) => row.id)).toEqual(["venue-d", "venue-c"]);
    });

    it("builds ranked rows from explicit venue counts", () => {
        const rows = buildVenueCountRows({
            countsByVenueId: {
                "venue-a": 3,
                "venue-c": 1,
            },
            lastDateByVenueId: {
                "venue-a": "2026-04-01",
            },
            venues: {
                "venue-a": { name: "Alpha" },
                "venue-b": { name: "Beta" },
                "venue-c": { name: "Gamma" },
            },
        });

        expect(rows.map((row) => row.id)).toEqual(["venue-a", "venue-c", "venue-b"]);
        expect(rows[0]).toMatchObject({ id: "venue-a", label: "Alpha", count: 3, lastWonDate: "2026-04-01" });
        expect(rows[2]).toMatchObject({ id: "venue-b", label: "Beta", count: 0, lastWonDate: null });
    });
});
