import { After, Before, setDefaultTimeout, setWorldConstructor, World } from "@cucumber/cucumber";
import { chromium } from "playwright";

setDefaultTimeout(30_000);

class BrowserWorld extends World {
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
    const context = await this.browser.newContext();
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

Before(async function () {
  await this.openBrowser();
});

After(async function () {
  await this.closeBrowser();
});
