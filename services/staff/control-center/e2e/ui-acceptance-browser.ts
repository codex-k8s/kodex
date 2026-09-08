import { expect, type Page } from "@playwright/test";

export async function geometry(page: Page) {
  return page.evaluate(() => {
    const header = document.querySelector(".topbar")?.getBoundingClientRect();
    return {
      overflow: Math.max(
        0,
        document.documentElement.scrollWidth -
          document.documentElement.clientWidth,
      ),
      headerHeight: header?.height ?? 0,
      headings: document.querySelectorAll(".page-header h1").length,
      untranslated: document.body.innerText.includes("i18n:"),
      alerts: Array.from(document.querySelectorAll('[role="alert"]')).filter(
        (item) => (item as HTMLElement).offsetHeight > 0,
      ).length,
    };
  });
}
export async function visit(page: Page, path: string) {
  const response = await page.goto(path, { waitUntil: "domcontentloaded" });
  expect(response?.status()).toBe(200);
  expect(new URL(page.url()).pathname).toBe(path);
  await expect(page.locator(".app-shell")).toBeVisible();
  await expect(page.locator(".page-header h1").first()).toBeVisible();
  // Ограниченная стабилизация текущего экрана; не ждём networkidle при WS/polling.
  await page.waitForTimeout(750);
  const metrics = await geometry(page);
  expect(metrics.overflow).toBeLessThanOrEqual(1);
  expect(metrics.headerHeight).toBeGreaterThan(0);
  expect(metrics.untranslated).toBe(false);
  expect(metrics.alerts).toBe(0);
  return metrics;
}
