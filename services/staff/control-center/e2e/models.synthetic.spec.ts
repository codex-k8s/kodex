import { expect, test } from "@playwright/test";
import type { ModelCapability } from "../src/shared/api/generated/openapi/types.gen";
for (const width of [1440, 390]) {
  test(`synthetic: каталог моделей ${String(width)}px`, async ({
    page,
  }, testInfo) => {
    await page.setViewportSize({ width, height: 844 });
    const failures: string[] = [];
    const queries: string[] = [];
    page.on("pageerror", (error) => failures.push(error.message));
    page.on("console", (message) => {
      if (["warning", "error"].includes(message.type()))
        failures.push(message.text());
    });
    let revoked = false;
    const current: ModelCapability = {
      id: "model-current",
      providerDefinitionKey: "openai-codex",
      reasoningEfforts: ["medium", "high"],
      defaultReasoningEffort: "medium",
      available: true,
      eligibleProviderAccountRefs: ["pacc_primary"],
      readinessBlockers: [],
    };
    await page.route("**/*", async (route) => {
      const url = new URL(route.request().url());
      if (url.origin !== "https://kodex.test") {
        failures.push("Unexpected origin");
        await route.abort();
        return;
      }
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
      if (url.pathname === "/api/v1/model-capabilities") {
        expect(url.searchParams.get("providerDefinitionKey")).toBe(
          "openai-codex",
        );
        expect(url.searchParams.get("pageSize")).toBe("40");
        const account = url.searchParams.get("providerAccountRef");
        expect(["pacc_primary", "pacc_secondary"]).toContain(account);
        const query = url.searchParams.get("query") ?? "";
        queries.push(query);
        const permitted = !revoked && account === "pacc_primary";
        const item = {
          ...current,
          available: permitted,
          eligibleProviderAccountRefs: permitted ? ["pacc_primary"] : [],
          readinessBlockers: permitted ? [] : ["MODEL_UNAVAILABLE"],
        };
        const items = [item, { ...item, id: "model-alternative" }].filter(
          (model) => !query || model.id.includes(query),
        );
        await route.fulfill({
          json: { items, total: items.length, nextPageToken: "" },
        });
        return;
      }
      if (url.pathname.startsWith("/api/")) {
        failures.push(`Unhandled API ${url.pathname}`);
        await route.abort();
        return;
      }
      const response = await route.fetch({
        url: `http://127.0.0.1:43122${url.pathname}${url.search}`,
      });
      await route.fulfill({ response });
    });
    await page.goto("https://kodex.test/e2e/fixtures/models.html");
    await expect(
      page.getByRole("button", { name: "Сохранить", exact: true }),
    ).toBeEnabled();
    await page.locator(".async-picker__trigger").click();
    await page.getByRole("option", { name: /^model-alternative/ }).click();
    await expect(page.getByTestId("model")).toHaveText("model-alternative");
    await expect(
      page.getByRole("button", { name: "Сохранить", exact: true }),
    ).toBeEnabled();
    await page.locator(".async-picker__trigger").click();
    const search = page.getByRole("combobox");
    await search.fill("missing");
    await expect(page.getByRole("option")).toHaveCount(0);
    await expect.poll(() => queries.includes("missing")).toBe(true);
    await expect(page.getByTestId("model")).toHaveText("model-alternative");
    await search.press("Escape");
    await page.getByRole("checkbox").check();
    await expect(
      page.getByRole("button", { name: "Сохранить", exact: true }),
    ).toBeDisabled();
    await expect(
      page
        .getByText("Модель недоступна для выбранных учётных записей", {
          exact: true,
        })
        .last(),
    ).toBeVisible();
    revoked = true;
    await page.getByRole("checkbox").uncheck();
    await expect(
      page.getByRole("button", { name: "Сохранить", exact: true }),
    ).toBeDisabled();
    await expect(
      page
        .getByText("Модель недоступна для выбранных учётных записей", {
          exact: true,
        })
        .last(),
    ).toBeVisible();
    await expect(page.getByTestId("model")).toHaveText("model-alternative");
    await page.screenshot({
      path: testInfo.outputPath(`models-${String(width)}.png`),
      fullPage: true,
    });
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
    expect(failures).toEqual([]);
  });
}
