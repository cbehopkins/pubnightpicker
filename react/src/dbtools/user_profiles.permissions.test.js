import { readFile } from "node:fs/promises";

import {
    assertFails,
    assertSucceeds,
    initializeTestEnvironment,
} from "@firebase/rules-unit-testing";
import { afterAll, beforeAll, beforeEach, describe, it } from "vitest";
import { deleteDoc, doc, setDoc } from "firebase/firestore";

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
