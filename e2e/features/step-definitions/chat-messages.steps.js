import { strict as assert } from "node:assert";
import { randomUUID } from "node:crypto";
import { When, Then } from "@cucumber/cucumber";

const BASE_URL = process.env.E2E_BASE_URL ?? "http://127.0.0.1:3000";

When("{word} opens Chat Page", async function (actorName) {
    const page = this.actors.get(actorName);
    await page.getByRole("link", { name: "Chat Page", exact: true }).click();
    await page.waitForURL(`${BASE_URL}/chat`);
    await page.getByRole("heading", { name: "Chat Page", exact: true }).waitFor();
});

When("{word} opens Event Chat for {string}", async function (actorName, venueName) {
    const page = this.actors.get(actorName);
    if (!page.url().endsWith("/current_events")) {
        await page.getByRole("link", { name: "Current Events", exact: true }).click();
        await page.waitForURL(`${BASE_URL}/current_events`);
    }
    const displayName = this.journeyVenues.get(venueName);
    const event = page.getByRole("article").filter({
        has: page.getByRole("heading", { name: displayName, exact: true }),
    });
    await event.getByRole("button", { name: "Event Chat", exact: true }).click();
    await page.getByRole("link", { name: "Event Chat", exact: true }).click();
    await page.waitForURL(/\/chat\/event\/[^/]+$/);
    await page.getByRole("heading", { name: "Event Chat", exact: true }).waitFor();
});

async function sendChatMessage(world, actorName, scope) {
    assert.ok(["global", "event"].includes(scope));
    world.chatMessages ??= {};
    const message = `${scope}-journey-${randomUUID()}`;
    world.chatMessages[scope] = message;

    const page = world.actors.get(actorName);
    await page.getByLabel("Chat message", { exact: true }).fill(message);
    await page.getByRole("button", { name: "Send", exact: true }).click();
    await page.getByText(message, { exact: true }).waitFor();
}

When("{word} sends a {word} chat message", async function (actorName, scope) {
    await sendChatMessage(this, actorName, scope);
});

When("{word} sends an event chat message", async function (actorName) {
    await sendChatMessage(this, actorName, "event");
});

Then("{word} sees the {word} chat message", async function (actorName, scope) {
    assert.ok(["global", "event"].includes(scope));
    const message = this.chatMessages?.[scope];
    assert.ok(message, `No ${scope} chat message has been sent`);
    await this.actors.get(actorName).getByText(message, { exact: true }).waitFor();
});

Then("{word} does not see the {word} chat message", async function (actorName, scope) {
    assert.ok(["global", "event"].includes(scope));
    const message = this.chatMessages?.[scope];
    assert.ok(message, `No ${scope} chat message has been sent`);
    assert.equal(
        await this.actors.get(actorName).getByText(message, { exact: true }).count(),
        0,
        `${scope} message must not appear in the other chat scope`
    );
});
