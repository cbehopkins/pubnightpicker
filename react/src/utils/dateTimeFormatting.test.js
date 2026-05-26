import { describe, expect, it } from "vitest";
import { formatLocalDateTime } from "./dateTimeFormatting";

describe("formatLocalDateTime", () => {
    it("formats a local date as YYYY-MM-DD HH:mm:ss", () => {
        const value = new Date(2026, 4, 26, 9, 7, 5);

        expect(formatLocalDateTime(value)).toBe("2026-05-26 09:07:05");
    });

    it("returns null for invalid dates", () => {
        expect(formatLocalDateTime(new Date("not-a-real-date"))).toBeNull();
    });
});