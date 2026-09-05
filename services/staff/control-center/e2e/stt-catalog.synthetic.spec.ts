import { expect, test } from "@playwright/test";
import type { SttModelCatalog } from "../src/shared/api/generated/openapi/types.gen";

for (const width of [390, 2900]) {
  test(`synthetic: серверный каталог STT ${String(width)}px`, async ({
    page,
  }, testInfo) => {
    await page.setViewportSize({ width, height: 900 });
    const failures: string[] = [];
    page.on("pageerror", (error) => failures.push(error.message));
    let unavailable = false;
    const catalog: SttModelCatalog = {
      version: "fixture-catalog-1",
      observedAt: "2026-09-05T00:00:00Z",
      recommendedModel: "fixture-model",
      recommendedMaximumAudioBytes: 10485760,
      recommendedMaximumAudioDurationMilliseconds: 120000,
      responseFormat: "json",
      models: [
        {
          model: "fixture-model",
          legacy: false,
          parameterNames: [
            "languages",
            "prompt",
            "temperature",
            "chunking_strategy",
          ],
          chunkingStrategies: ["", "auto"],
          fileStreamSupported: true,
          streamEnabled: false,
          maximumPromptBytes: 896,
          maximumKeywords: 0,
          maximumKeywordBytes: 0,
          minimumTemperature: 0,
          maximumTemperature: 1,
        },
      ],
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
      if (url.pathname === "/api/v1/system-stt/model-catalog") {
        expect(url.search).toBe("");
        await route.fulfill(
          unavailable
            ? {
                status: 503,
                json: { code: "SERVICE_UNAVAILABLE", status: 503 },
              }
            : { json: catalog },
        );
        return;
      }
      if (url.pathname.startsWith("/api/")) {
        failures.push(`Unhandled API ${url.pathname}`);
        await route.abort();
        return;
      }
      await route.fulfill({
        response: await route.fetch({
          url: `http://127.0.0.1:43122${url.pathname}${url.search}`,
        }),
      });
    });
    await page.goto("https://kodex.test/e2e/fixtures/stt-catalog.html");
    await expect(page.getByText(/Каталог fixture-catalog-1/)).toBeVisible();
    await expect(page.getByTestId("document")).toContainText("saved-unlisted");
    await page.locator(".async-picker__trigger").nth(1).click();
    await page.locator('input[role="combobox"]').fill("fixture");
    await page.getByRole("option", { name: /fixture-model/ }).click();
    await expect(page.getByTestId("document")).toContainText(
      '"model": "fixture-model"',
    );
    await expect(page.getByTestId("document")).toContainText(
      "Сохранённое слово",
    );
    await expect(page.getByLabel("Таймаут провайдера, мс")).toHaveAttribute(
      "max",
      "15000",
    );
    await expect(
      page.getByLabel("Максимальная длительность, мс"),
    ).toHaveAttribute("max", "1800000");
    unavailable = true;
    await page.locator(".async-picker__trigger").nth(1).click();
    await page.locator('input[role="combobox"]').fill("unavailable");
    await expect(
      page.getByText(
        "Каталог моделей недоступен. Сохранённые значения не изменены.",
      ),
    ).toBeVisible();
    await page.locator('input[role="combobox"]').press("Escape");
    await expect(page.getByTestId("document")).toContainText(
      '"model": "fixture-model"',
    );
    const overflow = await page.evaluate(
      () => document.documentElement.scrollWidth > innerWidth,
    );
    expect(overflow).toBe(false);
    await page.screenshot({
      path: testInfo.outputPath(`stt-catalog-${String(width)}.png`),
      fullPage: true,
    });
    expect(failures).toEqual([]);
  });
}
