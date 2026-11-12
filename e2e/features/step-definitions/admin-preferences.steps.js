import { strict as assert } from "node:assert";
import { Given, When, Then } from "@cucumber/cucumber";

const BASE_URL = process.env.E2E_BASE_URL ?? "http://127.0.0.1:3000";
const ADMIN_EMAIL = process.env.E2E_ADMIN_EMAIL;
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD;

Given("a fresh emulator database has been bootstrapped with the journey admin", async function () {
  assert.equal(process.env.GOOGLE_CLOUD_PROJECT, "demo-pubnightpicker-journey");
  assert.equal(process.env.FIRESTORE_EMULATOR_HOST, "127.0.0.1:8080");
  assert.equal(process.env.FIREBASE_AUTH_EMULATOR_HOST, "127.0.0.1:9099");
  assert.ok(ADMIN_EMAIL, "journey admin email must be configured by the harness");
  assert.ok(ADMIN_PASSWORD, "journey admin password must be configured by the harness");
});

When("I open the Pub Night Picker homepage", async function () {
  await this.page.goto(BASE_URL, { waitUntil: "domcontentloaded", timeout: 30_000 });
});

Then("I should see the public homepage", async function () {
  await this.page.getByRole("heading", { name: "Pub Night Picker" }).waitFor();
  await this.page.getByRole("link", { name: "Log In" }).waitFor();
});

When("I log in as the journey admin", async function () {
  await this.page.getByRole("link", { name: "Login", exact: true }).click();
  await this.page.getByLabel("Email Address").fill(ADMIN_EMAIL);
  await this.page.getByLabel("Password").fill(ADMIN_PASSWORD);
  await this.page.getByRole("button", { name: "Login", exact: true }).click();
});

Then("I should see My Preferences", async function () {
  const preferencesLink = this.page.getByRole("link", { name: "My Preferences" });
  await preferencesLink.waitFor();
  await preferencesLink.click();
  await this.page.getByLabel("Theme").waitFor();
  await this.page.waitForURL("**/preferences");
});
