import { readFile } from "node:fs/promises";

import {
    assertFails,
    assertSucceeds,
    initializeTestEnvironment,
} from "@firebase/rules-unit-testing";
import { afterAll, beforeAll, beforeEach, describe, expect, it } from "vitest";
import { collection, deleteDoc, doc, getDoc, getDocs, query, setDoc, where } from "firebase/firestore";

const PROJECT_ID = "pubnightpicker-user-profile-delete-rules";

let testEnv;

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
    if (testEnv) {
        await testEnv.cleanup();
    }
});

beforeEach(async () => {
    await testEnv.clearFirestore();
    await testEnv.withSecurityRulesDisabled(async (context) => {
        const adminDb = context.firestore();

        await setDoc(doc(adminDb, "roles", "admin"), { "admin-user": true });

        await setDoc(doc(adminDb, "users", "user-a"), {
            uid: "user-a",
            email: "user-a@example.com",
        });

        await setDoc(doc(adminDb, "users", "user-b"), {
            uid: "user-b",
            email: "user-b@example.com",
        });

        await setDoc(doc(adminDb, "user-public", "user-a"), {
            uid: "user-a",
            name: "User A",
            photoUrl: null,
            votesVisible: true,
        });

        await setDoc(doc(adminDb, "user-public", "user-b"), {
            uid: "user-b",
            name: "User B",
            photoUrl: null,
            votesVisible: true,
        });
    });
});

describe("login profile initialization rules", () => {
    it("allows a new user with no role documents to read and create their own profiles", async () => {
        await testEnv.withSecurityRulesDisabled(async (context) => {
            await deleteDoc(doc(context.firestore(), "roles", "admin"));
        });
        const db = testEnv.authenticatedContext("new-user").firestore();
        const ref = doc(db, "users", "new-user");
        const snapshot = await assertSucceeds(getDoc(ref));
        expect(snapshot.exists()).toBe(false);
        await assertSucceeds(setDoc(ref, {
            uid: "new-user", name: "New User", email: "new@example.com", authProvider: "google",
        }, { merge: true }));
        await assertSucceeds(setDoc(doc(db, "user-public", "new-user"), {
            uid: "new-user", name: "New User", photoUrl: null, votesVisible: true,
        }, { merge: true }));
        await assertSucceeds(getDoc(ref));
    });

    it("keeps private collection queries and other users' documents inaccessible to new users", async () => {
        const db = testEnv.authenticatedContext("new-user").firestore();
        await assertFails(getDocs(query(collection(db, "users"), where("uid", "==", "new-user"))));
        await assertFails(getDoc(doc(db, "users", "user-a")));
    });
});

describe("users doc deletion rules", () => {
    it("allows a user to delete their own users/{uid} doc", async () => {
        const db = testEnv.authenticatedContext("user-a").firestore();
        await assertSucceeds(deleteDoc(doc(db, "users", "user-a")));
    });

    it("denies a user deleting another users/{uid} doc", async () => {
        const db = testEnv.authenticatedContext("user-a").firestore();
        await assertFails(deleteDoc(doc(db, "users", "user-b")));
    });

    it("allows admin to delete another users/{uid} doc", async () => {
        const db = testEnv.authenticatedContext("admin-user").firestore();
        await assertSucceeds(deleteDoc(doc(db, "users", "user-b")));
    });

    it("denies unauthenticated delete on users/{uid}", async () => {
        const db = testEnv.unauthenticatedContext().firestore();
        await assertFails(deleteDoc(doc(db, "users", "user-a")));
    });
});

describe("users testEmail fields rules", () => {
    it("allows a user to write testEmailReq on their own doc", async () => {
        const db = testEnv.authenticatedContext("user-a").firestore();
        await assertSucceeds(setDoc(doc(db, "users", "user-a"), { testEmailReq: "uuid-1" }, { merge: true }));
    });

    it("denies a user writing testEmailAck on their own doc", async () => {
        const db = testEnv.authenticatedContext("user-a").firestore();
        await assertFails(setDoc(doc(db, "users", "user-a"), { testEmailAck: "uuid-1" }, { merge: true }));
    });

    it("denies a user creating their doc with testEmailAck", async () => {
        const db = testEnv.authenticatedContext("user-c").firestore();
        await assertFails(setDoc(doc(db, "users", "user-c"), { uid: "user-c", testEmailAck: "uuid-1" }));
    });

    it("allows unrelated updates when testEmailAck already exists", async () => {
        await testEnv.withSecurityRulesDisabled(async (context) => {
            await setDoc(doc(context.firestore(), "users", "user-a"), { testEmailAck: "uuid-1" }, { merge: true });
        });
        const db = testEnv.authenticatedContext("user-a").firestore();
        await assertSucceeds(setDoc(doc(db, "users", "user-a"), { name: "A" }, { merge: true }));
    });

    it("allows admin to write testEmailAck", async () => {
        const db = testEnv.authenticatedContext("admin-user").firestore();
        await assertSucceeds(setDoc(doc(db, "users", "user-a"), { testEmailAck: "uuid-1" }, { merge: true }));
    });
});

describe("user-public doc deletion rules", () => {
    it("allows a user to delete their own user-public/{uid} doc", async () => {
        const db = testEnv.authenticatedContext("user-a").firestore();
        await assertSucceeds(deleteDoc(doc(db, "user-public", "user-a")));
    });

    it("denies a user deleting another user-public/{uid} doc", async () => {
        const db = testEnv.authenticatedContext("user-a").firestore();
        await assertFails(deleteDoc(doc(db, "user-public", "user-b")));
    });

    it("allows admin to delete another user-public/{uid} doc", async () => {
        const db = testEnv.authenticatedContext("admin-user").firestore();
        await assertSucceeds(deleteDoc(doc(db, "user-public", "user-b")));
    });

    it("denies unauthenticated delete on user-public/{uid}", async () => {
        const db = testEnv.unauthenticatedContext().firestore();
        await assertFails(deleteDoc(doc(db, "user-public", "user-a")));
    });
});
