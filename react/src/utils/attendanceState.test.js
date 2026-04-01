import { describe, expect, it } from "vitest";
import { getEffectiveAttendanceState } from "./attendanceState";

describe("attendanceState", () => {
    it("applies global attendance to non-global venues", () => {
        const result = getEffectiveAttendanceState({
            any: {
                canCome: ["user-1"],
                cannotCome: [],
            },
        }, "pub-1", "user-1");

        expect(result.canCome).toEqual(["user-1"]);
        expect(result.userCanCome).toBe(true);
        expect(result.userCannotCome).toBe(false);
    });

    it("lets local attendance override opposite global status", () => {
        const result = getEffectiveAttendanceState({
            any: {
                canCome: ["user-1", "user-2"],
                cannotCome: ["user-3"],
            },
            "pub-1": {
                canCome: ["user-3"],
                cannotCome: ["user-1"],
            },
        }, "pub-1", "user-1");

        expect(result.canCome).toEqual(["user-2", "user-3"]);
        expect(result.cannotCome).toEqual(["user-1"]);
        expect(result.userCanCome).toBe(false);
        expect(result.userCannotCome).toBe(true);
    });

    it("does not apply global fallback to the global row itself", () => {
        const result = getEffectiveAttendanceState({
            any: {
                canCome: ["user-1"],
                cannotCome: [],
            },
        }, "any", "user-1");

        expect(result.canCome).toEqual(["user-1"]);
    });
});
