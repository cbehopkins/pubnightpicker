// @ts-check
// @vitest-environment jsdom

import { renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import useAutopopulateVenueSelector from "./useAutopopulateVenueSelector";

vi.mock("./useWinningVenueStats", () => ({
    default: vi.fn(),
}));

vi.mock("./usePubs", () => ({
    default: vi.fn(),
}));

vi.mock("../utils/statsRanking", () => ({
    buildWinningVenueRows: vi.fn(),
    splitRankedStatRows: vi.fn(),
}));

import useWinningVenueStats from "./useWinningVenueStats";
import usePubs from "./usePubs";
import { buildWinningVenueRows, splitRankedStatRows } from "../utils/statsRanking";

const MOCK_PUBS = {
    v1: { name: "Venue 1", venueType: "pub" },
    v2: { name: "Venue 2", venueType: "pub" },
    v3: { name: "Venue 3", venueType: "pub" },
    v4: { name: "Venue 4", venueType: "pub" },
};

const EMPTY_CURRENT_PUBS = {};
const CURRENT_PUBS_WITH_V1 = { v1: {} };

describe("useAutopopulateVenueSelector", () => {
    beforeEach(() => {
        vi.clearAllMocks();
        vi.mocked(usePubs).mockReturnValue(MOCK_PUBS);
    });

    it("returns empty arrays while loading", () => {
        vi.mocked(useWinningVenueStats).mockReturnValue({
            polls: {},
            isLoading: true,
            errorMessage: null,
            startDate: "2026-04-13",
            endDate: "2026-05-11",
        });

        const { result } = renderHook(() => useAutopopulateVenueSelector("poll-1", EMPTY_CURRENT_PUBS));

        expect(result.current.mostVisited).toEqual([]);
        expect(result.current.leastVisited).toEqual([]);
        expect(result.current.random).toEqual([]);
        expect(result.current.isLoading).toBe(true);
        expect(result.current.error).toBeNull();
    });

    it("filters out venues that are already on the poll", async () => {
        vi.mocked(useWinningVenueStats).mockReturnValue({
            polls: {
                "poll-1": { selected: "v1", date: "2026-05-01" },
            },
            isLoading: false,
            errorMessage: null,
            startDate: "2026-04-13",
            endDate: "2026-05-11",
        });

        vi.mocked(buildWinningVenueRows).mockReturnValue([
            { id: "v1", label: "Venue 1", count: 10, lastWonDate: "2026-04-15" },
            { id: "v2", label: "Venue 2", count: 8, lastWonDate: "2026-04-14" },
            { id: "v3", label: "Venue 3", count: 5, lastWonDate: null },
        ]);

        vi.mocked(splitRankedStatRows).mockImplementation((rows, limit) => ({
            most: rows.slice(0, limit),
            least: [...rows].reverse().slice(0, limit),
        }));

        const { result } = renderHook(() => useAutopopulateVenueSelector("poll-1", CURRENT_PUBS_WITH_V1));

        await waitFor(() => {
            const ids = result.current.random.map((row) => row.id);
            expect(ids).not.toContain("v1");
        });
    });

    it("surfaces upstream loading errors", () => {
        vi.mocked(useWinningVenueStats).mockReturnValue({
            polls: {},
            isLoading: false,
            errorMessage: "Failed to load polls",
            startDate: "2026-04-13",
            endDate: "2026-05-11",
        });

        const { result } = renderHook(() => useAutopopulateVenueSelector("poll-1", EMPTY_CURRENT_PUBS));

        expect(result.current.error).toBe("Failed to load polls");
        expect(result.current.mostVisited).toEqual([]);
        expect(result.current.leastVisited).toEqual([]);
        expect(result.current.random).toEqual([]);
    });

    it("expands ranking window to find viable most and least categories", async () => {
        vi.mocked(useWinningVenueStats).mockReturnValue({
            polls: {
                "poll-1": { selected: "v1", date: "2026-05-01" },
                "poll-2": { selected: "v2", date: "2026-04-01" },
            },
            isLoading: false,
            errorMessage: null,
            startDate: "2026-04-13",
            endDate: "2026-05-11",
        });

        const rows = [
            { id: "v1", label: "Venue 1", count: 20, lastWonDate: "2026-05-10" },
            { id: "v2", label: "Venue 2", count: 10, lastWonDate: "2026-05-09" },
            { id: "v3", label: "Venue 3", count: 2, lastWonDate: "2026-04-01" },
            { id: "v4", label: "Venue 4", count: 1, lastWonDate: null },
        ];

        vi.mocked(buildWinningVenueRows).mockReturnValue(rows);
        vi.mocked(splitRankedStatRows).mockImplementation((allRows, limit) => ({
            most: allRows.slice(0, limit),
            least: [...allRows].reverse().slice(0, limit),
        }));

        const { result } = renderHook(() => useAutopopulateVenueSelector("poll-1", EMPTY_CURRENT_PUBS));

        await waitFor(() => {
            expect(result.current.mostVisited.length).toBeGreaterThan(0);
            expect(result.current.leastVisited.length).toBeGreaterThan(0);
            expect(result.current.random.map((row) => row.id)).toContain("v3");
            expect(result.current.random.map((row) => row.id)).toContain("v4");
        });
    });

    it("excludes recently visited venues from random pool", async () => {
        vi.mocked(useWinningVenueStats).mockReturnValue({
            polls: {
                "poll-1": { selected: "v1", date: "2026-05-01" },
            },
            isLoading: false,
            errorMessage: null,
            startDate: "2026-04-13",
            endDate: "2026-05-11",
        });

        vi.mocked(buildWinningVenueRows).mockReturnValue([
            { id: "v1", label: "Venue 1", count: 10, lastWonDate: "2026-05-10" },
            { id: "v2", label: "Venue 2", count: 5, lastWonDate: "2026-04-20" },
            { id: "v3", label: "Venue 3", count: 0, lastWonDate: null },
        ]);

        vi.mocked(splitRankedStatRows).mockImplementation((rows, limit) => ({
            most: rows.slice(0, limit),
            least: [...rows].reverse().slice(0, limit),
        }));

        const { result } = renderHook(() => useAutopopulateVenueSelector("poll-1", EMPTY_CURRENT_PUBS));

        await waitFor(() => {
            const ids = result.current.random.map((row) => row.id);
            expect(ids).not.toContain("v1");
            expect(ids).not.toContain("v2");
            expect(ids).toContain("v3");
        });
    });

    it("returns pub-only candidates when venue data includes mixed types", async () => {
        vi.mocked(usePubs).mockReturnValue({
            v1: { name: "Venue 1", venueType: "pub" },
            v2: { name: "Venue 2", venueType: "restaurant" },
            v3: { name: "Venue 3", venueType: "event" },
            v4: { name: "Venue 4", venueType: "pub" },
        });

        vi.mocked(useWinningVenueStats).mockReturnValue({
            polls: {
                "poll-1": { selected: "v1", date: "2026-04-01" },
                "poll-2": { selected: "v2", date: "2026-03-01" },
                "poll-3": { selected: "v3", date: "2026-03-05" },
            },
            isLoading: false,
            errorMessage: null,
            startDate: "2026-04-13",
            endDate: "2026-05-11",
        });

        vi.mocked(buildWinningVenueRows).mockReturnValue([
            { id: "v2", label: "Venue 2", count: 9, lastWonDate: "2026-03-01", venueType: "restaurant" },
            { id: "v3", label: "Venue 3", count: 8, lastWonDate: "2026-03-05", venueType: "event" },
            { id: "v1", label: "Venue 1", count: 7, lastWonDate: "2026-04-01", venueType: "pub" },
            { id: "v4", label: "Venue 4", count: 1, lastWonDate: null, venueType: "pub" },
        ]);

        vi.mocked(splitRankedStatRows).mockImplementation((rows, limit) => ({
            most: rows.slice(0, limit),
            least: [...rows].reverse().slice(0, limit),
        }));

        const { result } = renderHook(() => useAutopopulateVenueSelector("poll-1", EMPTY_CURRENT_PUBS));

        await waitFor(() => {
            const randomIds = result.current.random.map((row) => row.id);
            const mostIds = result.current.mostVisited.map((row) => row.id);
            const leastIds = result.current.leastVisited.map((row) => row.id);

            expect(randomIds).toContain("v1");
            expect(randomIds).toContain("v4");
            expect(randomIds).not.toContain("v2");
            expect(randomIds).not.toContain("v3");
            expect(mostIds).not.toContain("v2");
            expect(mostIds).not.toContain("v3");
            expect(leastIds).not.toContain("v2");
            expect(leastIds).not.toContain("v3");
        });
    });

    it("excludes venues marked banned from autopopulate categories", async () => {
        vi.mocked(usePubs).mockReturnValue({
            v1: { name: "Venue 1", venueType: "pub", banned: false },
            v2: { name: "Venue 2", venueType: "pub", banned: false },
            v3: { name: "Venue 3", venueType: "pub", banned: false },
            v4: { name: "Venue 4", venueType: "pub", banned: true },
        });

        vi.mocked(useWinningVenueStats).mockReturnValue({
            polls: {
                "poll-1": { selected: "v1", date: "2026-04-01" },
                "poll-2": { selected: "v2", date: "2026-03-01" },
            },
            isLoading: false,
            errorMessage: null,
            startDate: "2026-04-13",
            endDate: "2026-05-11",
        });

        vi.mocked(buildWinningVenueRows).mockReturnValue([
            { id: "v1", label: "Venue 1", count: 12, lastWonDate: "2026-03-01", venueType: "pub" },
            { id: "v2", label: "Venue 2", count: 9, lastWonDate: "2026-03-15", venueType: "pub" },
            { id: "v3", label: "Venue 3", count: 4, lastWonDate: null, venueType: "pub" },
            { id: "v4", label: "Venue 4", count: 1, lastWonDate: null, venueType: "pub" },
        ]);

        vi.mocked(splitRankedStatRows).mockImplementation((rows, limit) => ({
            most: rows.slice(0, limit),
            least: [...rows].reverse().slice(0, limit),
        }));

        const { result } = renderHook(() => useAutopopulateVenueSelector("poll-1", EMPTY_CURRENT_PUBS));

        await waitFor(() => {
            const randomIds = result.current.random.map((row) => row.id);
            const mostIds = result.current.mostVisited.map((row) => row.id);
            const leastIds = result.current.leastVisited.map((row) => row.id);

            expect(randomIds).not.toContain("v4");
            expect(mostIds).not.toContain("v4");
            expect(leastIds).not.toContain("v4");
        });
    });
});
