import { strict as assert } from "node:assert";
import { When, Then } from "@cucumber/cucumber";
import { collection, doc, getDocFromServer, getDocsFromServer, query, setDoc, where } from "firebase/firestore";
import { waitForValue, withEmulatorAdmin } from "../support/emulator.js";

const BASE_URL = process.env.E2E_BASE_URL ?? "http://127.0.0.1:3000";
const MEMBER_PASSWORD = "journey-only-user-password-2026";
const ACCOUNT_DELETION_VOTE_DOC = "account-deletion-e2e";

function memberEmail(world, name) {
    const email = world.actorEmails?.get(name);
    assert.ok(email, `No registered account is available for ${name}`);
    return email;
}

async function openFreshSignIn(world, name, email, password) {
    const page = await world.openActor(`${name} re-login`);
    await page.goto(BASE_URL, { waitUntil: "domcontentloaded" });
    await page.getByRole("link", { name: "Log In", exact: true }).click();
    await page.getByLabel("Email Address").fill(email);
    await page.getByLabel("Password", { exact: true }).fill(password);
    await page.getByRole("button", { name: "Login", exact: true }).click();
    return page;
}

async function assertCanLogInAgain(world, name, email, password) {
    const page = await openFreshSignIn(world, name, email, password);
    await page.waitForURL(`${BASE_URL}/`);
    await page.getByRole("link", { name: "My Preferences", exact: true }).waitFor();
    assert.equal(await page.getByRole("link", { name: "Log In", exact: true }).count(), 0);
}

async function assertCannotLogInAgain(world, name) {
    const email = memberEmail(world, name);
    const page = await openFreshSignIn(world, name, email, MEMBER_PASSWORD);
    await page.getByText("No account exists for that email", { exact: true }).waitFor();
    assert.equal(await page.getByRole("link", { name: "My Preferences", exact: true }).count(), 0);
}

async function registeredUid(world, name) {
    const email = memberEmail(world, name);
    const user = await withEmulatorAdmin((db) => waitForValue(async () => {
        const users = await getDocsFromServer(query(collection(db, "users"), where("email", "==", email)));
        return users.docs[0] ?? null;
    }, (snapshot) => snapshot !== null, `${name}'s profile before account deletion`));
    return user.id;
}

When("Alice and Bob have account-deletion votes", async function () {
    const aliceUid = await registeredUid(this, "Alice");
    const bobUid = await registeredUid(this, "Bob");
    this.accountDeletionAliceUid = aliceUid;
    this.accountDeletionBobUid = bobUid;
    await withEmulatorAdmin((db) => setDoc(doc(db, "votes", ACCOUNT_DELETION_VOTE_DOC), {
        "venue-a": [aliceUid, bobUid, aliceUid],
        "venue-b": [aliceUid],
        "venue-c": [bobUid],
    }));
});

async function waitForAdminAuthDeletion(targetUid) {
    const request = await withEmulatorAdmin((db) => waitForValue(async () => {
        const requests = await getDocsFromServer(query(
            collection(db, "admin_delete_requests"),
            where("targetUid", "==", targetUid),
        ));
        return requests.docs[0] ?? null;
    }, (snapshot) => snapshot !== null && snapshot.data().status !== "pending",
        `admin Auth deletion for ${targetUid}`));
    assert.equal(request.data().status, "auth_deleted", "admin deletion must complete in Firebase Auth");
}

When("{word} deletes their own account", async function (name) {
    const page = this.actors.get(name);
    const email = memberEmail(this, name);
    await page.getByRole("link", { name: "My Preferences", exact: true }).click();
    await page.waitForURL(`${BASE_URL}/preferences`);
    await page.getByRole("button", { name: "Delete My Account", exact: true }).click();
    await page.getByRole("heading", { name: "Delete your account?" }).waitFor();
    await page.getByRole("button", { name: "Continue", exact: true }).click();
    await page.getByRole("heading", { name: "Confirm account deletion" }).waitFor();
    await page.locator('input[name="delete_account_confirm"]').fill(email);
    await page.getByRole("button", { name: "Delete Account", exact: true }).click();
    await page.getByRole("heading", { name: "Reauthenticate" }).waitFor();
    await page.locator('input[name="delete_account_password"]').fill(MEMBER_PASSWORD);
    await page.getByRole("button", { name: "Verify and Delete", exact: true }).click();
    await page.getByRole("link", { name: "Log In", exact: true }).waitFor();
});

When("the admin deletes the account for {word}", async function (name) {
    const email = memberEmail(this, name);
    const targetUid = await registeredUid(this, name);
    const admin = this.actors.get("admin");
    await admin.getByRole("link", { name: "Admin", exact: true }).click();
    await admin.getByRole("heading", { name: "Manage Users", exact: true, level: 1 }).waitFor();
    const userRow = admin.locator("tr").filter({ hasText: email });
    await userRow.getByRole("link", { name, exact: true }).click();
    await admin.getByRole("heading", { name: "Manage User", exact: true }).waitFor();
    await admin.getByRole("button", { name: "Delete User", exact: true }).click();
    await admin.getByRole("heading", { name: "Delete this user?" }).waitFor();
    await admin.getByRole("button", { name: "Continue", exact: true }).click();
    await admin.getByRole("heading", { name: "Type to confirm deletion" }).waitFor();
    await admin.locator('input[name="admin_delete_user_confirm"]').fill(email);
    await admin.locator("#overlay-root").getByRole("button", { name: "Delete User", exact: true }).click();
    await admin.waitForURL("**/admin/users");
    await waitForAdminAuthDeletion(targetUid);
});

Then("{word} can log in again", async function (name) {
    await assertCanLogInAgain(this, name, memberEmail(this, name), MEMBER_PASSWORD);
});

Then("the journey admin can log in again", async function () {
    await assertCanLogInAgain(
        this,
        "journey admin",
        process.env.E2E_ADMIN_EMAIL,
        process.env.E2E_ADMIN_PASSWORD,
    );
});

Then("{word} cannot log in again", async function (name) {
    await assertCannotLogInAgain(this, name);
});

Then("Alice's votes are removed and Bob's votes are preserved", async function () {
    const aliceUid = this.accountDeletionAliceUid;
    const bobUid = this.accountDeletionBobUid;
    await withEmulatorAdmin((db) => waitForValue(async () => {
        const snapshot = await getDocFromServer(doc(db, "votes", ACCOUNT_DELETION_VOTE_DOC));
        return snapshot.data() ?? null;
    }, (data) => data !== null
        && Array.isArray(data["venue-a"])
        && !data["venue-a"].includes(aliceUid)
        && data["venue-a"].includes(bobUid)
        && Array.isArray(data["venue-b"])
        && data["venue-b"].length === 0
        && Array.isArray(data["venue-c"])
        && data["venue-c"].length === 1
        && data["venue-c"].includes(bobUid),
        "vote cleanup for Alice"));
});
