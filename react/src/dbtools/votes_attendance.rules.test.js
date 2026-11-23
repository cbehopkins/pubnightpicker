import { readFile } from "node:fs/promises";

import {
    assertFails,
    assertSucceeds,
    initializeTestEnvironment,
} from "@firebase/rules-unit-testing";
import { deleteDoc, doc, getDoc, setDoc, updateDoc } from "firebase/firestore";
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
        await setDoc(doc(adminDb, "roles", "admin"), { "admin-1": true });
        await setDoc(doc(adminDb, "roles", "canCreatePoll"), { "admin-1": true });
    });
});

describe("votes and attendance rules", () => {
    it("denies an authenticated member reading a votes document", async () => {
        const db = testEnv.authenticatedContext("member-1").firestore();
        await assertFails(getDoc(doc(db, "votes", "meet-1")));
    });

    it("denies an authenticated member updating a votes document", async () => {
        const db = testEnv.authenticatedContext("member-1").firestore();
        await assertFails(updateDoc(doc(db, "votes", "meet-1"), { "venue-1": ["member-1"] }));
    });

    it("denies an authenticated member creating a votes document", async () => {
        const db = testEnv.authenticatedContext("member-1").firestore();
        await assertFails(setDoc(doc(db, "votes", "meet-without-votes"), {}));
    });

    it("denies an authenticated member deleting a votes document", async () => {
        const db = testEnv.authenticatedContext("member-1").firestore();
        await assertFails(deleteDoc(doc(db, "votes", "meet-1")));
    });

    it("denies privileged clients every votes operation", async () => {
        const db = testEnv.authenticatedContext("admin-1").firestore();
        await assertFails(getDoc(doc(db, "votes", "meet-1")));
        await assertFails(updateDoc(doc(db, "votes", "meet-1"), { any: ["admin-1"] }));
        await assertFails(setDoc(doc(db, "votes", "meet-without-votes"), {}));
        await assertFails(deleteDoc(doc(db, "votes", "meet-1")));
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
