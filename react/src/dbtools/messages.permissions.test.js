import { readFile } from "node:fs/promises";

import {
    assertFails,
    assertSucceeds,
    initializeTestEnvironment,
} from "@firebase/rules-unit-testing";
import { afterAll, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import { addDoc, collection, deleteDoc, doc, getDoc, getDocs, query, serverTimestamp, setDoc, updateDoc, where } from "firebase/firestore";
import { DELETED_USER_MESSAGE, DELETED_USER_NAME, DELETED_USER_UID } from "./userDeletion";

vi.mock("../firebase", () => ({ db: {} }));

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

describe("admin account deletion message anonymization", () => {
    function anonymization(overrides = {}) {
        return {
            uid: DELETED_USER_UID,
            name: DELETED_USER_NAME,
            text: DELETED_USER_MESSAGE,
            deletedUserUid: "user-a",
            deletedAt: serverTimestamp(),
            ...overrides,
        };
    }

    it("allows an admin without chat or moderation roles to query and anonymize a user's messages", async () => {
        await testEnv.withSecurityRulesDisabled(async (context) => {
            await deleteDoc(doc(context.firestore(), "roles", "canChat"));
            await deleteDoc(doc(context.firestore(), "roles", "canDeleteAnyMessage"));
        });
        const db = testEnv.authenticatedContext("adminUser").firestore();
        const snapshot = await assertSucceeds(getDocs(query(collection(db, "messages"), where("uid", "==", "user-a"))));
        expect(snapshot.size).toBe(1);
        await assertSucceeds(updateDoc(snapshot.docs[0].ref, anonymization()));
        const data = (await getDoc(snapshot.docs[0].ref)).data();
        expect(data).toMatchObject({
            uid: DELETED_USER_UID, name: DELETED_USER_NAME, text: DELETED_USER_MESSAGE,
            deletedUserUid: "user-a", scopeType: "global", scopeId: "main",
        });
        expect(data.createdAt.toDate()).toEqual(baseTimestamp);
        expect(data.deletedAt).toBeDefined();
    });

    it("denies the cleanup query for non-admins without chat permission and anonymous users", async () => {
        for (const context of [testEnv.authenticatedContext("user-no-role"), testEnv.unauthenticatedContext()]) {
            await assertFails(getDocs(query(collection(context.firestore(), "messages"), where("uid", "==", "user-a"))));
        }
    });

    it("denies cross-user anonymization by a non-admin", async () => {
        const db = testEnv.authenticatedContext("user-b").firestore();
        await assertFails(updateDoc(doc(db, "messages", "existing-msg"), anonymization()));
    });

    it("does not give an admin without chat or moderation roles general message write permissions", async () => {
        const db = testEnv.authenticatedContext("adminUser").firestore();
        const ref = doc(db, "messages", "existing-msg");
        await assertFails(updateDoc(ref, { text: "edited" }));
        await assertFails(addDoc(collection(db, "messages"), globalMessage({ uid: "adminUser" })));
        await assertFails(deleteDoc(ref));
    });

    it("denies incomplete or altered anonymization and changes to message scope", async () => {
        const ref = doc(testEnv.authenticatedContext("adminUser").firestore(), "messages", "existing-msg");
        for (const overrides of [
            { uid: "other-user" },
            { name: "Not anonymized" },
            { text: "Not anonymized" },
            { deletedUserUid: "user-b" },
            { deletedAt: baseTimestamp },
            { scopeId: "other-poll" },
        ]) {
            await assertFails(updateDoc(ref, anonymization(overrides)));
        }
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
