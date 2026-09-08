import { expect, test } from "@playwright/test";
import { remoteReloadClientSource } from "../vite.config";
import { installReadNetworkObserver } from "./ui-read-network";
for (const observed of [false, true]) {
  test(`dev reload: outage caught with observer=${String(observed)}`, async ({
    page,
  }) => {
    const errors: string[] = [];
    page.on("pageerror", (error) =>
      errors.push(`${error.name}:${error.message}`),
    );
    if (observed) await installReadNetworkObserver(page, "https://kodex.test");
    let attempts = 0;
    await page.route("https://kodex.test/**", async (route) => {
      const path = new URL(route.request().url()).pathname;
      if (path === "/__kodex_dev_revision") {
        attempts++;
        await route.abort("connectionfailed");
      } else if (path === "/__kodex_dev_reload.js")
        await route.fulfill({
          contentType: "application/javascript",
          body: remoteReloadClientSource(),
        });
      else
        await route.fulfill({
          contentType: "text/html",
          body: '<!doctype html><script type="module" src="/__kodex_dev_reload.js"></script>',
        });
    });
    await page.goto("https://kodex.test/");
    await expect.poll(() => attempts).toBeGreaterThanOrEqual(2);
    await page.close();
    expect(errors).toEqual([]);
  });
}
for (const observed of [false, true]) {
  test(`dev reload: pagehide aborts and pageshow resumes once observer=${String(observed)}`, async ({
    page,
  }) => {
    const errors: string[] = [];
    page.on("pageerror", (error) => errors.push(error.name));
    if (observed) await installReadNetworkObserver(page, "https://kodex.test");
    let attempts = 0;
    await page.route("https://kodex.test/**", async (route) => {
      const path = new URL(route.request().url()).pathname;
      if (path === "/__kodex_dev_revision") {
        attempts++;
        if (attempts === 1) return;
        await route.fulfill({
          contentType: "text/plain",
          body: "00000000-0000-0000-0000-000000000000:1",
        });
      } else if (path === "/__kodex_dev_reload.js")
        await route.fulfill({
          contentType: "application/javascript",
          body: remoteReloadClientSource(),
        });
      else
        await route.fulfill({
          contentType: "text/html",
          body: '<!doctype html><script type="module" src="/__kodex_dev_reload.js"></script>',
        });
    });
    await page.goto("https://kodex.test/", { waitUntil: "domcontentloaded" });
    await expect.poll(() => attempts).toBe(1);
    await page.evaluate(() =>
      window.dispatchEvent(
        new PageTransitionEvent("pagehide", { persisted: true }),
      ),
    );
    await page.waitForTimeout(1200);
    expect(attempts).toBe(1);
    await page.evaluate(() => {
      window.dispatchEvent(
        new PageTransitionEvent("pageshow", { persisted: true }),
      );
      window.dispatchEvent(
        new PageTransitionEvent("pageshow", { persisted: true }),
      );
    });
    await expect.poll(() => attempts).toBe(2);
    await page.goto("https://kodex.test/next", {
      waitUntil: "domcontentloaded",
    });
    await expect.poll(() => attempts).toBeGreaterThanOrEqual(3);
    await page.close();
    expect(errors).toEqual([]);
  });
}
test("dev reload: native timeout recovers and revision causes one reload", async ({
  page,
}) => {
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.name));
  let attempts = 0,
    documents = 0;
  await page.route("https://kodex.test/**", async (route) => {
    const path = new URL(route.request().url()).pathname;
    if (path === "/__kodex_dev_revision") {
      attempts++;
      if (attempts === 1) return;
      await route.fulfill({
        contentType: "text/plain",
        body: `00000000-0000-0000-0000-000000000000:${String(attempts === 2 ? 1 : 2)}`,
      });
    } else if (path === "/__kodex_dev_reload.js")
      await route.fulfill({
        contentType: "application/javascript",
        body: remoteReloadClientSource(),
      });
    else {
      documents++;
      await route.fulfill({
        contentType: "text/html",
        body: '<!doctype html><script type="module" src="/__kodex_dev_reload.js"></script>',
      });
    }
  });
  await page.goto("https://kodex.test/", { waitUntil: "domcontentloaded" });
  await expect
    .poll(() => attempts, { timeout: 7000 })
    .toBeGreaterThanOrEqual(3);
  await expect.poll(() => documents).toBe(2);
  await page.waitForTimeout(1500);
  expect(documents).toBe(2);
  await page.close();
  expect(errors).toEqual([]);
});
