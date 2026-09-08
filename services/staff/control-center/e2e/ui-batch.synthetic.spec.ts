import { expect, test } from "@playwright/test";
import {
  assistantDialog,
  cleanupAssistant,
  safeAlertMetrics,
} from "./ui-acceptance-browser";

// Дожидаемся уже начатых fixture fetch до закрытия APIRequestContext.
test.afterEach(async ({ page }) => {
  await page.unrouteAll({ behavior: "wait" });
});

async function fixture(page: import("@playwright/test").Page, query = "") {
  await page.route("https://kodex.test/**", async (route) => {
    const url = new URL(route.request().url());
    if (url.pathname.startsWith("/api/"))
      throw new Error("Unexpected UI batch fixture request");
    await route.fulfill({
      response: await route.fetch({
        url: `http://127.0.0.1:43122${url.pathname}${url.search}`,
      }),
    });
  });
  await page.goto(`/e2e/fixtures/ui-proof.html${query}`);
}

test("synthetic: rich selector получает фокус после visibility и возвращает его после Escape", async ({
  page,
}) => {
  await fixture(page);
  const trigger = page.locator(".async-picker__trigger");
  const input = page.locator('.async-picker__popover input[role="combobox"]');
  for (let attempt = 0; attempt < 2; attempt++) {
    await trigger.click();
    await expect(input).toBeFocused();
    await input.fill("");
    await page.keyboard.type("fixture");
    await expect(input).toHaveValue("fixture");
    await page.keyboard.press("ArrowDown");
    await page.keyboard.press("Escape");
    await expect(page.locator(".async-picker__popover")).toHaveCount(0);
    await expect(trigger).toBeFocused();
  }
});

for (const locale of ["ru", "en"] as const) {
  test(`synthetic: assistant ${locale} сохраняет имя и cleanup после отказа`, async ({
    page,
  }) => {
    await fixture(page, `?locale=${locale}`);
    const opener = page.getByRole("button", {
      name: locale === "ru" ? "Открыть Kodex" : "Open Kodex",
      exact: true,
    });
    await opener.click();
    await expect(assistantDialog(page, locale)).toBeVisible();
    // Независимая очистка работает даже при ошибке проверяемого accessible name.
    await page
      .locator("#assistant-workspace")
      .evaluate((element) =>
        element.setAttribute("aria-label", "Synthetic changed title"),
      );
    await cleanupAssistant(page, locale);
    await page.setViewportSize({ width: 390, height: 844 });
    await opener.click();
    await expect(assistantDialog(page, locale)).toBeVisible();
    await cleanupAssistant(page, locale);
  });
}

test("synthetic: terminal integration page требует строку cursor и не показывает alert после исправления", async ({
  page,
}) => {
  await page.route("https://kodex.test/**", async (route) => {
    const url = new URL(route.request().url());
    if (url.pathname === "/config/runtime-config.json") {
      await route.fulfill({
        json: {
          revision: "0".repeat(64),
          environment: "synthetic",
          apiBaseUrl: "/",
          realtimeUrl: "/api/v1",
          requestTimeoutMs: 10000,
          oidc: {
            authority: "https://identity.invalid",
            clientId: "synthetic",
            redirectUri: "/auth/callback",
            postLogoutRedirectUri: "/",
            scope: "openid",
          },
        },
      });
      return;
    }
    if (url.pathname === "/api/v1/integration-connections") {
      await route.fulfill({ json: { items: [] } });
      return;
    }
    if (url.pathname.startsWith("/api/"))
      throw new Error("Unexpected integration terminal fixture request");
    await route.fulfill({
      response: await route.fetch({
        url: `http://127.0.0.1:43122${url.pathname}${url.search}`,
      }),
    });
  });
  const firstResponse = page.waitForResponse(
    (value) =>
      new URL(value.url()).pathname === "/api/v1/integration-connections",
  );
  await page.goto("/e2e/fixtures/ui-proof.html?fixture=integration-terminal");
  await firstResponse;
  await expect(page.locator('[role="alert"]')).toHaveCount(1);
  expect(await safeAlertMetrics(page)).toMatchObject({
    alertProblemNotice: true,
    alertDefaultError: true,
  });
  await page.route("**/api/v1/integration-connections**", (route) =>
    route.fulfill({ json: { items: [], nextPageToken: "" } }),
  );
  const response = page.waitForResponse(
    (value) =>
      new URL(value.url()).pathname === "/api/v1/integration-connections",
  );
  await page.reload();
  await response;
  await expect(page.locator(".connections-panel")).toBeVisible();
  await expect(page.locator('[role="alert"]')).toHaveCount(0);
});

test("synthetic: поздний popover callback не возвращает фокус после close/unmount", async ({
  page,
}) => {
  await fixture(page, "?fixture=popover-lifecycle");
  for (const label of ["Race close", "Race unmount"]) {
    const button = page.getByRole("button", { name: label, exact: true });
    await button.click();
    await expect(
      page.getByRole("dialog", { name: "Race", exact: true }),
    ).toHaveCount(0);
    await expect(button).toBeFocused();
  }
  await page.getByRole("button", { name: "Race reopen", exact: true }).click();
  await expect(page.getByLabel("Race input", { exact: true })).toBeFocused();
  await page.keyboard.press("Escape");
  await expect(
    page.getByRole("dialog", { name: "Race", exact: true }),
  ).toHaveCount(0);
});
