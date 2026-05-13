import { readFile } from "node:fs/promises";

import {
    assertFails,
    assertSucceeds,
    initializeTestEnvironment,
} from "@firebase/rules-unit-testing";
import { addDoc, collection, deleteDoc, doc, getDoc, setDoc, updateDoc } from "firebase/firestore";
import { afterAll, beforeAll, beforeEach, describe, it } from "vitest";

const PROJECT_ID = "pubnightpicker-admin-delete-requests-rules";

let testEnv;

function validRequest(overrides = {}) {
    return {
        schemaVersion: 1,
        targetUid: "target-user",
        targetEmail: "target@example.com",
        requestedByUid: "admin-user",
        requestedByEmail: "admin@example.com",
        reason: "admin_user_delete",
        scrubbedAppData: true,
        status: "pending",
        createdAt: new Date("2026-05-12T00:00:00.000Z"),
        updatedAt: new Date("2026-05-12T00:00:00.000Z"),
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
    if (testEnv) {
        await testEnv.cleanup();
    }
});

beforeEach(async () => {
    await testEnv.clearFirestore();
    await testEnv.withSecurityRulesDisabled(async (context) => {
        const adminDb = context.firestore();
        await setDoc(doc(adminDb, "roles", "admin"), { "admin-user": true });
        await setDoc(doc(adminDb, "admin_delete_requests", "existing"), validRequest());
        await setDoc(doc(adminDb, "admin_delete_request_audit", "audit-1"), {
            requestId: "existing",
            outcome: "dry_run_validated",
        });
        await setDoc(doc(adminDb, "admin_delete_request_metrics", "global"), {
            total: 3,
            outcomes: { auth_delete_failed: 1, auth_delete_blocked: 2 },
        });
        await setDoc(doc(adminDb, "system_config", "admin_delete"), {
            paused: false,
            reason: "",
            updatedByUid: "admin-user",
            updatedAt: new Date("2026-05-12T00:00:00.000Z"),
        });
    });
});

describe("admin_delete_requests rules", () => {
    it("allows admin create with valid payload", async () => {
        const db = testEnv.authenticatedContext("admin-user").firestore();
        await assertSucceeds(addDoc(collection(db, "admin_delete_requests"), validRequest()));
    });

    it("denies non-admin create", async () => {
        const db = testEnv.authenticatedContext("plain-user").firestore();
        await assertFails(addDoc(collection(db, "admin_delete_requests"), validRequest({ requestedByUid: "plain-user" })));
    });

    it("denies create when requestedByUid mismatches auth uid", async () => {
        const db = testEnv.authenticatedContext("admin-user").firestore();
        await assertFails(addDoc(collection(db, "admin_delete_requests"), validRequest({ requestedByUid: "someone-else" })));
    });

    it("allows admin read", async () => {
        const db = testEnv.authenticatedContext("admin-user").firestore();
        await assertSucceeds(getDoc(doc(db, "admin_delete_requests", "existing")));
    });

    it("denies non-admin read", async () => {
        const db = testEnv.authenticatedContext("plain-user").firestore();
        await assertFails(getDoc(doc(db, "admin_delete_requests", "existing")));
    });

    it("denies admin update", async () => {
        const db = testEnv.authenticatedContext("admin-user").firestore();
        await assertFails(updateDoc(doc(db, "admin_delete_requests", "existing"), { status: "done" }));
    });

    it("denies admin delete", async () => {
        const db = testEnv.authenticatedContext("admin-user").firestore();
        await assertFails(deleteDoc(doc(db, "admin_delete_requests", "existing")));
    });
});

describe("admin_delete_request_audit rules", () => {
    it("allows admin read", async () => {
        const db = testEnv.authenticatedContext("admin-user").firestore();
        await assertSucceeds(getDoc(doc(db, "admin_delete_request_audit", "audit-1")));
    });

    it("denies non-admin read", async () => {
        const db = testEnv.authenticatedContext("plain-user").firestore();
        await assertFails(getDoc(doc(db, "admin_delete_request_audit", "audit-1")));
    });

    it("denies admin writes", async () => {
        const db = testEnv.authenticatedContext("admin-user").firestore();
        await assertFails(setDoc(doc(db, "admin_delete_request_audit", "audit-2"), { outcome: "x" }));
    });
});

describe("admin_delete_request_metrics rules", () => {
    it("allows admin read", async () => {
        const db = testEnv.authenticatedContext("admin-user").firestore();
        await assertSucceeds(getDoc(doc(db, "admin_delete_request_metrics", "global")));
    });

    it("denies non-admin read", async () => {
        const db = testEnv.authenticatedContext("plain-user").firestore();
        await assertFails(getDoc(doc(db, "admin_delete_request_metrics", "global")));
    });

    it("denies admin writes", async () => {
        const db = testEnv.authenticatedContext("admin-user").firestore();
        await assertFails(updateDoc(doc(db, "admin_delete_request_metrics", "global"), { total: 4 }));
    });
});

describe("system_config admin_delete rules", () => {
    it("allows admin read", async () => {
        const db = testEnv.authenticatedContext("admin-user").firestore();
        await assertSucceeds(getDoc(doc(db, "system_config", "admin_delete")));
    });

    it("denies non-admin read", async () => {
        const db = testEnv.authenticatedContext("plain-user").firestore();
        await assertFails(getDoc(doc(db, "system_config", "admin_delete")));
    });

    it("allows admin update with valid payload", async () => {
        const db = testEnv.authenticatedContext("admin-user").firestore();
        await assertSucceeds(updateDoc(doc(db, "system_config", "admin_delete"), {
            paused: true,
            reason: "on-call pause",
            updatedByUid: "admin-user",
            updatedAt: new Date("2026-05-12T01:00:00.000Z"),
        }));
    });

    it("denies admin write to non-admin_delete system config docs", async () => {
        const db = testEnv.authenticatedContext("admin-user").firestore();
        await assertFails(setDoc(doc(db, "system_config", "other"), {
            paused: true,
            reason: "x",
            updatedByUid: "admin-user",
            updatedAt: new Date("2026-05-12T01:00:00.000Z"),
        }));
    });

    it("denies admin update when updatedByUid mismatches", async () => {
        const db = testEnv.authenticatedContext("admin-user").firestore();
        await assertFails(updateDoc(doc(db, "system_config", "admin_delete"), {
            paused: true,
            updatedByUid: "someone-else",
            updatedAt: new Date("2026-05-12T01:00:00.000Z"),
        }));
    });

    it("denies admin delete", async () => {
        const db = testEnv.authenticatedContext("admin-user").firestore();
        await assertFails(deleteDoc(doc(db, "system_config", "admin_delete")));
    });
});
