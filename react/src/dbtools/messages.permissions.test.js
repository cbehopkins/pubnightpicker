import { readFile } from "node:fs/promises";

import {
    assertFails,
    assertSucceeds,
    initializeTestEnvironment,
} from "@firebase/rules-unit-testing";
import { afterAll, beforeAll, beforeEach, describe, it } from "vitest";
import { addDoc, collection, deleteDoc, doc, getDoc, setDoc, updateDoc } from "firebase/firestore";

const PROJECT_ID = "pubnightpicker-messages-rules";
const baseTimestamp = new Date("2026-05-07T12:00:00.000Z");

let testEnv;

function globalMessage(overrides = {}) {
    return {
        text: "Hello world",
        name: "Test User",
        uid: "user-a",
        createdAt: baseTimestamp,
        scopeType: "global",
        scopeId: "main",
        ...overrides,
    };
}

function eventMessage(pollId = "poll-1", overrides = {}) {
    return {
        text: "Event chat message",
        name: "Test User",
        uid: "user-a",
        createdAt: baseTimestamp,
        scopeType: "event",
        scopeId: pollId,
        ...overrides,
    };
}

beforeAll(async () => {
    const rules = await readFile(new URL("../../firestore.rules", import.meta.url), "utf8");
    testEnv = await initializeTestEnvironment({
        projectId: PROJECT_ID,
        firestore: {
            host: "127.0.0.1",
            port: parseInt(process.env.VITEST_FIRESTORE_PORT ?? "8080"),
            rules,
        },
    });
});

afterAll(async () => {
    if (testEnv) await testEnv.cleanup();
});

beforeEach(async () => {
    await testEnv.clearFirestore();
    await testEnv.withSecurityRulesDisabled(async (context) => {
        const adminDb = context.firestore();

        // Set up roles
        await setDoc(doc(adminDb, "roles", "canChat"), { "user-a": true, "user-b": true });
        await setDoc(doc(adminDb, "roles", "canDeleteAnyMessage"), { "moderator": true });
        await setDoc(doc(adminDb, "roles", "admin"), { "adminUser": true });

        // Seed an existing message from user-a for delete tests
        await setDoc(doc(adminDb, "messages", "existing-msg"), globalMessage({ uid: "user-a" }));

        // Seed an existing message from user-b for cross-user delete tests
        await setDoc(doc(adminDb, "messages", "other-msg"), globalMessage({ uid: "user-b" }));
    });
});

describe("messages firestore rules — reads", () => {
    it("allows canChat users to read messages", async () => {
        const db = testEnv.authenticatedContext("user-a").firestore();
        await assertSucceeds(getDoc(doc(db, "messages", "existing-msg")));
    });

    it("denies unauthenticated reads", async () => {
        const db = testEnv.unauthenticatedContext().firestore();
        await assertFails(getDoc(doc(db, "messages", "existing-msg")));
    });

    it("denies reads for users without canChat role", async () => {
        const db = testEnv.authenticatedContext("user-no-role").firestore();
        await assertFails(getDoc(doc(db, "messages", "existing-msg")));
    });
});

describe("messages firestore rules — creates", () => {
    it("allows canChat user to create a valid global message", async () => {
        const db = testEnv.authenticatedContext("user-a").firestore();
        await assertSucceeds(addDoc(collection(db, "messages"), globalMessage()));
    });

    it("allows canChat user to create a valid event message", async () => {
        const db = testEnv.authenticatedContext("user-a").firestore();
        await assertSucceeds(addDoc(collection(db, "messages"), eventMessage("poll-1")));
    });

    it("denies create without scopeType", async () => {
        const db = testEnv.authenticatedContext("user-a").firestore();
        const { scopeType: _, ...noScopeType } = globalMessage();
        await assertFails(addDoc(collection(db, "messages"), noScopeType));
    });

    it("denies create without scopeId", async () => {
        const db = testEnv.authenticatedContext("user-a").firestore();
        const { scopeId: _, ...noScopeId } = globalMessage();
        await assertFails(addDoc(collection(db, "messages"), noScopeId));
    });

    it("denies create with invalid scopeType", async () => {
        const db = testEnv.authenticatedContext("user-a").firestore();
        await assertFails(
            addDoc(collection(db, "messages"), globalMessage({ scopeType: "unknown" }))
        );
    });

    it("denies create with empty scopeId", async () => {
        const db = testEnv.authenticatedContext("user-a").firestore();
        await assertFails(
            addDoc(collection(db, "messages"), globalMessage({ scopeId: "" }))
        );
    });

    it("denies create for users without canChat role", async () => {
        const db = testEnv.authenticatedContext("user-no-role").firestore();
        await assertFails(addDoc(collection(db, "messages"), globalMessage({ uid: "user-no-role" })));
    });

    it("denies create for unauthenticated users", async () => {
        const db = testEnv.unauthenticatedContext().firestore();
        await assertFails(addDoc(collection(db, "messages"), globalMessage()));
    });
});

describe("messages firestore rules — deletes", () => {
    it("allows a user to delete their own message", async () => {
        const db = testEnv.authenticatedContext("user-a").firestore();
        await assertSucceeds(deleteDoc(doc(db, "messages", "existing-msg")));
    });

    it("denies a user deleting another user's message", async () => {
        const db = testEnv.authenticatedContext("user-a").firestore();
        await assertFails(deleteDoc(doc(db, "messages", "other-msg")));
    });

    it("allows canDeleteAnyMessage to delete any message", async () => {
        const db = testEnv.authenticatedContext("moderator").firestore();
        await assertSucceeds(deleteDoc(doc(db, "messages", "other-msg")));
    });

    it("denies unauthenticated deletes", async () => {
        const db = testEnv.unauthenticatedContext().firestore();
        await assertFails(deleteDoc(doc(db, "messages", "existing-msg")));
    });
});

describe("messages firestore rules — updates", () => {
    it("allows a user to update their own message", async () => {
        const db = testEnv.authenticatedContext("user-a").firestore();
        await assertSucceeds(updateDoc(doc(db, "messages", "existing-msg"), { text: "edited" }));
    });

    it("denies a user updating another user's message", async () => {
        const db = testEnv.authenticatedContext("user-a").firestore();
        await assertFails(updateDoc(doc(db, "messages", "other-msg"), { text: "edited" }));
    });

    it("allows canDeleteAnyMessage to update any message", async () => {
        const db = testEnv.authenticatedContext("moderator").firestore();
        await assertSucceeds(updateDoc(doc(db, "messages", "other-msg"), { text: "moderated" }));
    });
});

describe("chat_push_actions firestore rules", () => {
    it("denies reads by any authenticated user", async () => {
        const db = testEnv.authenticatedContext("user-a").firestore();
        await assertFails(getDoc(doc(db, "chat_push_actions", "msg-1")));
    });

    it("denies writes by any authenticated user", async () => {
        const db = testEnv.authenticatedContext("user-a").firestore();
        await assertFails(
            setDoc(doc(db, "chat_push_actions", "msg-1"), {
                scopeType: "global",
                scopeId: "main",
                notified: [],
                createdAt: baseTimestamp,
            })
        );
    });

    it("denies reads by unauthenticated users", async () => {
        const db = testEnv.unauthenticatedContext().firestore();
        await assertFails(getDoc(doc(db, "chat_push_actions", "msg-1")));
    });

    it("denies writes by unauthenticated users", async () => {
        const db = testEnv.unauthenticatedContext().firestore();
        await assertFails(
            setDoc(doc(db, "chat_push_actions", "msg-1"), {
                scopeType: "global",
                scopeId: "main",
                notified: [],
                createdAt: baseTimestamp,
            })
        );
    });
});
