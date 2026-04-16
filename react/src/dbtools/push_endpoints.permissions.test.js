import { readFile } from "node:fs/promises";

import {
    assertFails,
    assertSucceeds,
    initializeTestEnvironment,
} from "@firebase/rules-unit-testing";
import { afterAll, beforeAll, beforeEach, describe, expect, it } from "vitest";
import { deleteDoc, doc, getDoc, setDoc, updateDoc } from "firebase/firestore";

const PROJECT_ID = "pubnightpicker-push-rules";
const baseTimestamp = new Date("2026-04-16T12:00:00.000Z");

let testEnv;

function endpointPayload(overrides = {}) {
    return {
        endpoint: "https://push.example.test/subscription/123",
        p256dh: "p256dh-key",
        auth: "auth-secret",
        active: true,
        createdAt: baseTimestamp,
        lastSeenAt: baseTimestamp,
        disabledAt: null,
        userAgent: "Vitest Browser",
        platform: "test",
        appVersion: "1.0.0-test",
        ...overrides,
    };
}

beforeAll(async () => {
    const rules = await readFile(new URL("../../firestore.rules", import.meta.url), "utf8");
    testEnv = await initializeTestEnvironment({
        projectId: PROJECT_ID,
        firestore: {
            host: "127.0.0.1",
            port: 8080,
            rules,
        },
    });
});

afterAll(async () => {
    await testEnv.cleanup();
});

beforeEach(async () => {
    await testEnv.clearFirestore();
    await testEnv.withSecurityRulesDisabled(async (context) => {
        const adminDb = context.firestore();
        await setDoc(doc(adminDb, "roles", "admin"), { adminUser: true });
        await setDoc(doc(adminDb, "users", "user-a"), { uid: "user-a" });
        await setDoc(doc(adminDb, "users", "user-b"), { uid: "user-b" });
        await setDoc(
            doc(adminDb, "users", "user-b", "push_endpoints", "endpoint-b"),
            endpointPayload(),
        );
    });
});

describe("push endpoint firestore rules", () => {
    it("allows a user to create, update, and delete their own endpoint", async () => {
        const userDb = testEnv.authenticatedContext("user-a").firestore();
        const ref = doc(userDb, "users", "user-a", "push_endpoints", "endpoint-a");

        await assertSucceeds(setDoc(ref, endpointPayload()));
        await assertSucceeds(updateDoc(ref, {
            active: false,
            disabledAt: baseTimestamp,
            lastSeenAt: baseTimestamp,
        }));
        await assertSucceeds(deleteDoc(ref));
    });

    it("denies cross-user writes to another user's endpoint", async () => {
        const userDb = testEnv.authenticatedContext("user-a").firestore();
        const ref = doc(userDb, "users", "user-b", "push_endpoints", "endpoint-b");

        await assertFails(updateDoc(ref, { active: false }));
        await assertFails(setDoc(doc(userDb, "users", "user-b", "push_endpoints", "endpoint-c"), endpointPayload()));
        await assertFails(deleteDoc(ref));
    });

    it("allows a user to read their own endpoint and denies cross-user reads", async () => {
        const ownDb = testEnv.authenticatedContext("user-b").firestore();
        const otherDb = testEnv.authenticatedContext("user-a").firestore();

        const ownSnapshot = await assertSucceeds(
            getDoc(doc(ownDb, "users", "user-b", "push_endpoints", "endpoint-b")),
        );
        expect(ownSnapshot.exists()).toBe(true);

        await assertFails(
            getDoc(doc(otherDb, "users", "user-b", "push_endpoints", "endpoint-b")),
        );
    });

    it("allows admin-style reads of push endpoints", async () => {
        const adminDb = testEnv.authenticatedContext("adminUser").firestore();

        const snapshot = await assertSucceeds(
            getDoc(doc(adminDb, "users", "user-b", "push_endpoints", "endpoint-b")),
        );

        expect(snapshot.exists()).toBe(true);
        expect(snapshot.data()?.endpoint).toContain("push.example.test");
    });
});
