import { readFile } from "node:fs/promises";
import { assertFails, assertSucceeds, initializeTestEnvironment } from "@firebase/rules-unit-testing";
import { deleteDoc, doc, getDoc, setDoc, updateDoc } from "firebase/firestore";
import { afterAll, beforeAll, beforeEach, describe, it } from "vitest";

let testEnv;

beforeAll(async () => {
    const rules = await readFile(new URL("../../firestore.rules", import.meta.url), "utf8");
    testEnv = await initializeTestEnvironment({
        projectId: "pubnightpicker-diagnostics-config-rules",
        firestore: { host: "127.0.0.1", port: parseInt(process.env.VITEST_FIRESTORE_PORT ?? "8080"), rules },
    });
});

afterAll(async () => { if (testEnv) await testEnv.cleanup(); });

beforeEach(async () => {
    await testEnv.clearFirestore();
    await testEnv.withSecurityRulesDisabled(async (context) => {
        await setDoc(doc(context.firestore(), "roles", "admin"), { "admin-user": true });
    });
});

describe("diagnostics configuration rules", () => {
    it("allows admin reads, creation and boolean updates", async () => {
        const ref = doc(testEnv.authenticatedContext("admin-user").firestore(), "config", "diagnostics");
        await assertSucceeds(getDoc(ref));
        await assertSucceeds(setDoc(ref, { SilenceNotifications: true }));
        await assertSucceeds(updateDoc(ref, { SilenceNotifications: false }));
        await assertSucceeds(getDoc(ref));
    });

    it("supports an absent field default", async () => {
        const ref = doc(testEnv.authenticatedContext("admin-user").firestore(), "config", "diagnostics");
        await assertSucceeds(setDoc(ref, {}));
    });

    it("denies non-admin and unauthenticated access", async () => {
        for (const context of [testEnv.authenticatedContext("ordinary-user"), testEnv.unauthenticatedContext()]) {
            const ref = doc(context.firestore(), "config", "diagnostics");
            await assertFails(getDoc(ref));
            await assertFails(setDoc(ref, { SilenceNotifications: true }));
        }
    });

    it("denies malformed fields on create and update", async () => {
        const ref = doc(testEnv.authenticatedContext("admin-user").firestore(), "config", "diagnostics");
        for (const value of ["true", 1, null]) {
            await assertFails(setDoc(ref, { SilenceNotifications: value }));
        }
        await assertFails(setDoc(ref, { SilenceNotifications: true, unexpected: true }));
        await setDoc(ref, { SilenceNotifications: true });
        await assertFails(updateDoc(ref, { SilenceNotifications: "false" }));
        await assertFails(updateDoc(ref, { unexpected: true }));
    });

    it("denies deletion and other configuration documents", async () => {
        const db = testEnv.authenticatedContext("admin-user").firestore();
        const ref = doc(db, "config", "diagnostics");
        await setDoc(ref, { SilenceNotifications: true });
        await assertFails(deleteDoc(ref));
        await assertFails(getDoc(doc(db, "config", "other")));
        await assertFails(setDoc(doc(db, "config", "other"), { SilenceNotifications: true }));
    });
});
