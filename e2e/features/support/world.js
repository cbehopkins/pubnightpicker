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
    this.page = await this.browser.newPage();
    this.page.setDefaultTimeout(15_000);
    this.page.setDefaultNavigationTimeout(30_000);
    this.page.on("console", (message) => {
      if (message.type() === "error") {
        console.error(`[Browser console] ${message.text()}`);
      }
    });
    this.page.on("pageerror", (error) => {
      console.error(`[Browser uncaught error] ${error.stack || error.message}`);
    });
    this.page.on("requestfailed", (request) => {
      console.error(`[Browser request failed] ${request.url()}: ${request.failure()?.errorText}`);
    });
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
