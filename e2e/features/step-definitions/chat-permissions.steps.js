import { strict as assert } from "node:assert";
import { randomUUID } from "node:crypto";
import { When, Then } from "@cucumber/cucumber";

const BASE_URL = process.env.E2E_BASE_URL ?? "http://127.0.0.1:3000";

When("Alice creates an account", async function () {
    this.aliceEmail = `alice-${randomUUID()}@test.local`;
    const alice = await this.openActor("Alice");
    await alice.goto(BASE_URL, { waitUntil: "domcontentloaded" });
    await alice.getByRole("link", { name: "Register", exact: true }).click();
    await alice.getByLabel("Full Name", { exact: true }).fill("Alice");
    await alice.getByLabel("Email Address").fill(this.aliceEmail);
    await alice.getByLabel("Password", { exact: true }).fill("alice-journey-only-password-2026");
    await alice.getByRole("button", { name: "Register", exact: true }).click();
});

Then("Alice is logged in", async function () {
    const alice = this.actors.get("Alice");
    await alice.waitForURL(`${BASE_URL}/`);
    await alice.getByRole("link", { name: "My Preferences", exact: true }).waitFor();
    assert.equal(await alice.getByRole("link", { name: "Login", exact: true }).count(), 0);
});

Then("Alice cannot see the Chat Page", async function () {
    const alice = this.actors.get("Alice");
    assert.equal(await alice.getByRole("link", { name: "Chat Page", exact: true }).count(), 0);
});

When("the journey admin logs in through a separate browser context", async function () {
    const admin = await this.openActor("admin");
    assert.notEqual(admin.context(), this.actors.get("Alice").context());
    await admin.goto(BASE_URL, { waitUntil: "domcontentloaded" });
    await admin.getByRole("link", { name: "Login", exact: true }).click();
    await admin.getByLabel("Email Address").fill(process.env.E2E_ADMIN_EMAIL);
    await admin.getByLabel("Password").fill(process.env.E2E_ADMIN_PASSWORD);
    await admin.getByRole("button", { name: "Login", exact: true }).click();
});

Then("the admin sees Alice on the Manage Users page", async function () {
    const admin = this.actors.get("admin");
    await admin.getByRole("link", { name: "Manage Users", exact: true }).click();
    await admin.getByRole("heading", { name: "Manage Users", exact: true }).waitFor();
    await admin.getByText(this.aliceEmail, { exact: true }).waitFor();
    await admin.getByRole("link", { name: "Alice", exact: true }).click();
    await admin.getByRole("heading", { name: "Manage User", exact: true }).waitFor();
    await admin.getByText(this.aliceEmail, { exact: true }).waitFor();
    const canChat = admin.getByRole("checkbox", { name: "Can Chat", exact: true });
    await canChat.waitFor();
    assert.equal(await canChat.isChecked(), false, "Alice must start without CanChat rights");
});

When("the admin grants Alice CanChat rights", async function () {
    const admin = this.actors.get("admin");
    await admin.getByRole("checkbox", { name: "Can Chat", exact: true }).click();
    await admin.locator('input[name="canChat"]:checked').waitFor();
});

Then("Alice can open the Chat Page without reloading or signing in again", async function () {
    const alice = this.actors.get("Alice");
    assert.equal(alice.url(), `${BASE_URL}/`);
    await alice.getByRole("link", { name: "My Preferences", exact: true }).waitFor();
    await alice.getByRole("link", { name: "Chat Page", exact: true }).click();
    await alice.waitForURL("**/chat");
    await alice.getByRole("heading", { name: "Chat Page", exact: true }).waitFor();
    assert.equal(await alice.getByRole("link", { name: "Manage Users", exact: true }).count(), 0);
});
