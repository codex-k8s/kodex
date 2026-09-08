import { expect, test } from "@playwright/test";
for (const width of [390, 768, 1280, 1440, 1920, 2560, 2900]) {
  for (const locale of ["ru", "en"]) {
    test(`Интерфейс MVP: ${String(width)}px ${locale}`, async ({ page }) => {
      await page.setViewportSize({ width, height: 1000 });
      const failures: string[] = [];
      page.on("pageerror", (error) => failures.push(error.message));
      await page.route("**/*", async (route) => {
        const url = new URL(route.request().url());
        if (
          url.origin !== "https://kodex.test" ||
          url.pathname.startsWith("/api/")
        ) {
          failures.push(`Unexpected request ${url.pathname}`);
          await route.abort();
          return;
        }
        await route.fulfill({
          response: await route.fetch({
            url: `http://127.0.0.1:43122${url.pathname}${url.search}`,
          }),
        });
      });
      await page.goto(`/e2e/fixtures/ui-proof.html?locale=${locale}`);
      await expect(page.locator("[data-history-requests]")).toHaveText("0");
      await page.locator(".async-picker__trigger").click();
      const options = page.locator(".async-picker__options:visible");
      await expect(options.getByRole("option")).toHaveCount(30);
      await options.evaluate((element) => {
        element.scrollTop = element.scrollHeight;
      });
      await expect(options.getByRole("option")).toHaveCount(60);
      await expect(page.locator("[data-picker-requests]")).toHaveText("2");
      await page.keyboard.press("Escape");
      await expect(page.locator(".async-picker__trigger")).toBeFocused();
      await page.locator(".assistant-fab").click();
      const dialog = page.locator("#assistant-workspace");
      await expect(dialog).toBeVisible();
      const geometry = await dialog.boundingBox();
      if (!geometry) throw new Error("Missing dialog geometry");
      expect(geometry.width).toBeGreaterThan(width * 0.9);
      expect(geometry.x).toBeGreaterThanOrEqual(0);
      expect(geometry.x + geometry.width).toBeLessThanOrEqual(width + 1);
      if (width <= 1000)
        await dialog.locator(".assistant-history__toggle").click();
      const history = page.locator(
        width <= 1000
          ? ".assistant-history__menu"
          : ".assistant-conversation-sidebar",
      );
      await history.evaluate((element) => {
        element.scrollTop = element.scrollHeight;
      });
      await expect(page.locator("[data-history-requests]")).toHaveText("1");
      if (width <= 1000) await page.keyboard.press("Escape");
      await dialog.locator(".assistant-context-strip").click();
      await expect(dialog.locator(".overlay-panel")).toBeVisible();
      await page.keyboard.press("Escape");
      await expect(dialog.locator(".overlay-panel")).toHaveCount(0);
      await expect(dialog).toBeVisible();
      await expect(dialog.locator(".assistant-context-strip")).toBeFocused();
      await expect
        .poll(() =>
          page.evaluate(
            () => document.documentElement.scrollWidth <= innerWidth,
          ),
        )
        .toBe(true);
      await dialog.locator(".assistant-drawer__header > .icon-button").click();
      await expect(dialog).toHaveCount(0);
      expect(failures).toEqual([]);
    });
  }
}
