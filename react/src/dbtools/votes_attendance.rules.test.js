import { readFile } from "node:fs/promises";

import {
    assertFails,
    assertSucceeds,
    initializeTestEnvironment,
} from "@firebase/rules-unit-testing";
import { doc, getDoc, setDoc, updateDoc } from "firebase/firestore";
import { afterAll, beforeAll, beforeEach, describe, expect, it } from "vitest";

const PROJECT_ID = "demo-pubnightpicker-vote-attendance-rules";

let testEnv;

beforeAll(async () => {
    const rules = await readFile(new URL("../../firestore.rules", import.meta.url), "utf8");
    testEnv = await initializeTestEnvironment({
        projectId: PROJECT_ID,
        firestore: {
            host: "127.0.0.1",
            port: parseInt(process.env.VITEST_FIRESTORE_PORT ?? "9080"),
            rules,
        },
    });
});

afterAll(async () => {
    if (testEnv) {
        await testEnv.cleanup();
    }
});

beforeEach(async () => {
    await testEnv.clearFirestore();
    await testEnv.withSecurityRulesDisabled(async (context) => {
        const adminDb = context.firestore();
        await setDoc(doc(adminDb, "votes", "meet-1"), {});
        await setDoc(doc(adminDb, "attendance", "meet-1"), {});
    });
});

describe("votes and attendance rules", () => {
    it("lets an authenticated member make the first vote on an initialised document", async () => {
        const db = testEnv.authenticatedContext("member-1").firestore();
        const votesRef = doc(db, "votes", "meet-1");

        await assertSucceeds(updateDoc(votesRef, { "venue-1": ["member-1"] }));

        const votesSnapshot = await assertSucceeds(getDoc(votesRef));
        expect(votesSnapshot.data()).toEqual({ "venue-1": ["member-1"] });
    });

    it("does not let an ordinary member create a votes document", async () => {
        const db = testEnv.authenticatedContext("member-1").firestore();

        await assertFails(setDoc(doc(db, "votes", "meet-without-votes"), {}));
    });

    it("lets an authenticated member make the first attendance update", async () => {
        const db = testEnv.authenticatedContext("member-1").firestore();
        const attendanceRef = doc(db, "attendance", "meet-1");
        const attendance = { "venue-1": { canCome: ["member-1"], cannotCome: [] } };

        await assertSucceeds(updateDoc(attendanceRef, attendance));

        const attendanceSnapshot = await assertSucceeds(getDoc(attendanceRef));
        expect(attendanceSnapshot.data()).toEqual(attendance);
    });
});