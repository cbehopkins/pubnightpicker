import { After, Before, setDefaultTimeout, setWorldConstructor, Status, World } from "@cucumber/cucumber";
import { chromium } from "playwright";
import { advanceApplicationTime as advanceClock } from "./application-clock.js";
import { installPushBoundary } from "./push-fixture.js";

setDefaultTimeout(30_000);

class BrowserWorld extends World {
  async advanceApplicationTime(instant) {
    return advanceClock(instant);
  }

  async openBrowser() {
    this.browser = await chromium.launch({
      headless: true,
      ...(process.env.E2E_BROWSER_CHANNEL
        ? { channel: process.env.E2E_BROWSER_CHANNEL }
        : {}),
    });
    this.actors = new Map();
    this.page = await this.openActor("default");
  }

  async openActor(name) {
    if (this.actors.has(name)) {
      return this.actors.get(name);
    }
    const context = await this.browser.newContext({ locale: "en-GB", timezoneId: "Europe/London" });
    if (this.defaultGroups) {
      await context.route("http://127.0.0.1:8081/api/groups?name=ampubnight", (route) => route.fulfill({
        json: { groups: this.defaultGroups },
      }));
    }
    this.pushIdentities ??= new Map();
    this.pushIdentities.set(name, await installPushBoundary(context, this.pushPermissionResponses?.get(name) ?? "denied"));
    const page = await context.newPage();
    page.setDefaultTimeout(15_000);
    page.setDefaultNavigationTimeout(30_000);
    page.on("console", (message) => {
      if (message.type() === "error") {
        console.error(`[${name} browser console] ${message.text()}`);
      }
    });
    page.on("pageerror", (error) => {
      console.error(`[${name} browser uncaught error] ${error.stack || error.message}`);
    });
    page.on("requestfailed", (request) => {
      console.error(`[${name} browser request failed] ${request.url()}: ${request.failure()?.errorText}`);
    });
    this.actors.set(name, page);
    return page;
  }

  async closeBrowser() {
    await this.browser?.close();
  }
}

setWorldConstructor(BrowserWorld);

Before(async function ({ pickle }) {
  if (pickle.tags.some((tag) => tag.name === "@meet_read_api_07")) return "skipped";
  if (!pickle.tags.some((tag) => tag.name === "@last_orders")) {
    this.defaultGroups = [{ id: "frontend-journey-group", name: "ampubnight", visibility: "public" }];
  }
  await this.openBrowser();
});

After(async function ({ result }) {
  try {
    if (result?.status === Status.FAILED) {
      for (const [name, page] of this.actors ?? []) {
        if (!page.isClosed()) {
          await this.attach(`${name}: ${page.url()}`, "text/plain");
          await this.attach(await page.screenshot({ fullPage: true }), "image/png");
        }
      }
    }
  } finally {
    await this.closeBrowser();
  }
});
