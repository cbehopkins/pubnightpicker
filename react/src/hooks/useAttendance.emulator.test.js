// @vitest-environment jsdom

import { readFile } from "node:fs/promises";
import { initializeTestEnvironment } from "@firebase/rules-unit-testing";
import { doc, getDoc, setDoc } from "firebase/firestore";
import { act, cleanup, render, renderHook, screen, waitFor } from "@testing-library/react";
import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import useAttendance from "./useAttendance";
import ShowAttendance from "../components/UI/ShowAttendance";

const testDb = vi.hoisted(() => ({ current: null }));

vi.mock("../firebase", () => ({ get db() { return testDb.current; } }));
vi.mock("./useUsers", () => ({ default: () => ({ "member-1": { name: "Test Member" } }) }));

let testEnv;

beforeAll(async () => {
    const rules = await readFile("firestore.rules", "utf8");
    testEnv = await initializeTestEnvironment({
        projectId: "demo-pubnightpicker-attendance-hook",
        firestore: { host: "127.0.0.1", port: parseInt(process.env.VITEST_FIRESTORE_PORT ?? "9080"), rules },
    });
    testDb.current = testEnv.authenticatedContext("member-1").firestore();
});

beforeEach(async () => {
    await testEnv.clearFirestore();
    await testEnv.withSecurityRulesDisabled(async (context) => {
        await setDoc(doc(context.firestore(), "polls", "meet-1"), {});
    });
});

afterEach(cleanup);
afterAll(async () => { await testEnv?.cleanup(); });

describe("attendance on a current Meet", () => {
    it("shows a member after they select can come", async () => {
        const { result } = renderHook(() => useAttendance("meet-1"));

        await act(async () => {
            await result.current[1]("venue-1", "member-1", "canCome");
        });

        const stored = await getDoc(doc(testDb.current, "attendance", "meet-1"));
        expect(stored.data()).toEqual({ "venue-1": { canCome: ["member-1"], cannotCome: [] } });
        await waitFor(() => expect(result.current[0]["venue-1"]?.canCome).toContain("member-1"));
        render(<ShowAttendance inline canCome={result.current[0]["venue-1"]?.canCome} />);
        expect(screen.getByText("Test Member")).toBeTruthy();
    });

    it("updates and clears attendance without disturbing other members", async () => {
        const attendanceRef = doc(testDb.current, "attendance", "meet-1");
        await setDoc(attendanceRef, {
            "venue-1": { canCome: ["member-2"], eta: { "member-2": "19:00" } },
        });
        const { result } = renderHook(() => useAttendance("meet-1"));

        await act(async () => {
            await result.current[1]("venue-1", "member-1", "canCome");
            await result.current[5]("venue-1", "member-1", "18:30");
            await result.current[1]("venue-1", "member-1", "cannotCome");
        });
        expect((await getDoc(attendanceRef)).data()).toEqual({
            "venue-1": { canCome: ["member-2"], cannotCome: ["member-1"], eta: { "member-2": "19:00" } },
        });

        await act(async () => {
            await result.current[2]("venue-1", "member-1");
            await result.current[4](["venue-1", "venue-2"], "member-1", "canCome");
            await result.current[5]("venue-1", "member-1", "18:30");
            await result.current[6]("venue-1", "member-1");
        });
        expect((await getDoc(attendanceRef)).data()).toEqual({
            any: { canCome: ["member-1"], cannotCome: [] },
            "venue-1": { canCome: ["member-2", "member-1"], cannotCome: [], eta: { "member-2": "19:00" } },
            "venue-2": { canCome: ["member-1"], cannotCome: [] },
        });
    });
});
