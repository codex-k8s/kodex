import { expect, test } from "@playwright/test";
for (const width of [390, 1440, 2900])
  test(`Измерения модели и текущая фаза ${String(width)}px`, async ({
    page,
  }, testInfo) => {
    await page.setViewportSize({ width, height: 900 });
    const errors: string[] = [];
    page.on("pageerror", (error) => errors.push(error.message));
    await page.route("**/*", async (route) => {
      const url = new URL(route.request().url());
      if (
        url.origin !== "https://kodex.test" ||
        url.pathname.startsWith("/api/")
      ) {
        errors.push("Unexpected fixture network request");
        await route.abort();
        return;
      }
      await route.fulfill({
        response: await route.fetch({
          url: `http://127.0.0.1:43122${url.pathname}${url.search}`,
        }),
      });
    });
    await page.goto("/e2e/fixtures/model-measurements.html");
    const summary = page.getByTestId("model-measurements");
    await summary.locator("summary").click();
    await expect(summary).toContainText("Обычные шаги");
    await expect(summary).toContainText("Сжатие контекста");
    await expect(summary).toContainText("Стоимость неизвестна");
    await expect(summary).toContainText("42");
    await expect(summary).toContainText("12000");
    await expect(summary).toContainText("Неизвестно");
    await expect(page.locator(".run-transcript__work")).toHaveCount(1);
    await expect(page.locator(".run-transcript__work")).toContainText(
      "Сжатие контекста",
    );
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth,
      ),
    ).toBe(true);
    expect(errors).toEqual([]);
    await page.screenshot({
      path: testInfo.outputPath("model-measurements.png"),
      fullPage: true,
    });
  });
