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

    it("allows exception flags and rejects invalid exception values", async () => {
        const ref = doc(testEnv.authenticatedContext("admin-user").firestore(), "config", "diagnostics");
        await assertSucceeds(setDoc(ref, { SilenceNotifications: true, NotifyPollActorWhenSilenced: true, KeepChatNotificationsWhenSilenced: true }));
        for (const field of ["NotifyPollActorWhenSilenced", "KeepChatNotificationsWhenSilenced"]) {
            await assertSucceeds(updateDoc(ref, { [field]: false }));
            for (const value of ["true", 1, null]) await assertFails(updateDoc(ref, { [field]: value }));
        }
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

describe("poll notification actor rules", () => {
    beforeEach(async () => {
        await testEnv.withSecurityRulesDisabled(async (context) => {
            for (const role of ["canCreatePoll", "canCompletePoll"]) {
                await setDoc(doc(context.firestore(), "roles", role), { "actor-user": true });
            }
        });
    });

    it("binds creator identity and keeps it immutable", async () => {
        const ref = doc(testEnv.authenticatedContext("actor-user").firestore(), "polls", "actor-poll");
        await assertFails(setDoc(ref, { date: "2026-10-10", completed: false, createdByUid: "other-user" }));
        await assertSucceeds(setDoc(ref, { date: "2026-10-10", completed: false, createdByUid: "actor-user" }));
        await assertFails(updateDoc(ref, { createdByUid: "other-user" }));
    });

    it("binds completion and requires clearing attribution on reschedule", async () => {
        const ref = doc(testEnv.authenticatedContext("actor-user").firestore(), "polls", "actor-poll");
        await setDoc(ref, { date: "2026-10-10", completed: false, createdByUid: "actor-user" });
        await assertFails(updateDoc(ref, { completed: true, selected: "pub-a", completedByUid: "other-user" }));
        await assertSucceeds(updateDoc(ref, { completed: true, selected: "pub-a", completedByUid: "actor-user" }));
        await assertFails(updateDoc(ref, { selected: "pub-b" }));
        await assertSucceeds(setDoc(ref, { date: "2026-10-10", completed: true, createdByUid: "actor-user", selected: "pub-b" }));
        await assertFails(updateDoc(ref, { completedByUid: "actor-user" }));
    });

    it("preserves legacy writes without granting creator attribution later", async () => {
        const ref = doc(testEnv.authenticatedContext("actor-user").firestore(), "polls", "legacy-poll");
        await assertSucceeds(setDoc(ref, { date: "2026-10-10", completed: false }));
        await assertFails(updateDoc(ref, { createdByUid: "actor-user" }));
        await assertSucceeds(updateDoc(ref, { completed: true, selected: "pub-a" }));
        await assertSucceeds(updateDoc(ref, { selected: "pub-b" }));
    });
});
