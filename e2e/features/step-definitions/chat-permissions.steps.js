import { strict as assert } from "node:assert";
import { randomUUID } from "node:crypto";
import { When, Then } from "@cucumber/cucumber";

const BASE_URL = process.env.E2E_BASE_URL ?? "http://127.0.0.1:3000";

When("{word} creates an account", async function (name) {
    this.actorEmails ??= new Map();
    const email = `${name.toLowerCase()}-${randomUUID()}@test.local`;
    this.actorEmails.set(name, email);
    const page = await this.openActor(name);
    await page.goto(BASE_URL, { waitUntil: "domcontentloaded" });
    await page.getByRole("link", { name: "Register", exact: true }).click();
    await page.getByLabel("Full Name", { exact: true }).fill(name);
    await page.getByLabel("Email Address").fill(email);
    await page.getByLabel("Password", { exact: true }).fill("journey-only-user-password-2026");
    await page.getByRole("button", { name: "Register", exact: true }).click();
});

Then("{word} is logged in", async function (name) {
    const page = this.actors.get(name);
    await page.waitForURL(`${BASE_URL}/`);
    await page.getByRole("link", { name: "My Preferences", exact: true }).waitFor();
    assert.equal(await page.getByRole("link", { name: "Login", exact: true }).count(), 0);
});

Then("Alice cannot see the Chat Page", async function () {
    const alice = this.actors.get("Alice");
    assert.equal(await alice.getByRole("link", { name: "Chat Page", exact: true }).count(), 0);
});

When("the journey admin logs in through a separate browser context", async function () {
    const admin = await this.openActor("admin");
    for (const [name, page] of this.actors) {
        if (name !== "admin") {
            assert.notEqual(admin.context(), page.context());
        }
    }
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
    await admin.getByText(this.actorEmails.get("Alice"), { exact: true }).waitFor();
    await admin.getByRole("link", { name: "Alice", exact: true }).click();
    await admin.getByRole("heading", { name: "Manage User", exact: true }).waitFor();
    await admin.getByText(this.actorEmails.get("Alice"), { exact: true }).waitFor();
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
