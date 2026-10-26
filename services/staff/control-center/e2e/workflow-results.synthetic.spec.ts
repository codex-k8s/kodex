import { expect, test } from "@playwright/test";

for (const width of [1440, 390]) {
  test(`Условия процесса: зависимости, тип результата и readonly ${String(width)}px`, async ({
    page,
  }, testInfo) => {
    await page.setViewportSize({ width, height: 1000 });
    const errors: string[] = [];
    page.on("pageerror", (error) => errors.push(error.message));
    page.on("console", (message) => {
      if (["error", "warning"].includes(message.type()))
        errors.push(message.text());
    });
    await page.route("**/*", async (route) => {
      const url = new URL(route.request().url());
      if (
        url.origin !== "https://kodex.test" ||
        url.pathname.startsWith("/api/")
      )
        throw new Error("Unexpected workflow fixture request");
      await route.fulfill({
        response: await route.fetch({
          url: `http://127.0.0.1:43122${url.pathname}`,
        }),
      });
    });
    await page.goto("/e2e/fixtures/workflow-results.html");
    const save = page.getByRole("button", {
      name: "Сохранить процесс",
      exact: true,
    });
    const developer = page.getByRole("region", {
      name: "Разработка",
      exact: true,
    });
    const review = page.getByRole("region", { name: "Ревью", exact: true });
    await expect(save).toBeEnabled();
    await developer.getByLabel("Ключ этапа", { exact: true }).fill("renamed");
    await expect(save).toBeDisabled();
    await expect(
      review.getByRole("listbox", {
        name: "Предшествующие этапы",
        exact: true,
      }),
    ).toHaveValues(["code"]);
    await expect(
      review.getByRole("option", {
        name: "Недоступный предшественник · code",
        exact: true,
      }),
    ).toHaveCount(1);
    await developer.getByLabel("Ключ этапа", { exact: true }).fill("code");
    await expect(save).toBeEnabled();
    await review
      .getByRole("listbox", { name: "Предшествующие этапы", exact: true })
      .selectOption([]);
    await expect(save).toBeDisabled();
    await review
      .getByRole("listbox", { name: "Предшествующие этапы", exact: true })
      .selectOption(["code"]);
    await expect(save).toBeEnabled();
    await review
      .getByRole("combobox", { name: "Условие", exact: true })
      .selectOption("ARTIFACT");
    await expect(save).toBeDisabled();
    await review.getByLabel("Имя файла", { exact: true }).fill("review.md");
    await expect(save).toBeEnabled();
    expect(await page.locator("[data-test-draft]").textContent()).not.toContain(
      "subjectStepKey",
    );
    await page.getByLabel("Только чтение", { exact: true }).check();
    await expect(
      review.getByLabel("Имя файла", { exact: true }),
    ).toBeDisabled();
    await expect(
      review.getByRole("button", { name: "Добавить условие", exact: true }),
    ).toBeDisabled();
    await expect(save).toBeDisabled();
    await expect(
      page.getByText("Обязательные результаты не приняты", { exact: true }),
    ).toBeVisible();
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth,
      ),
    ).toBe(true);
    await page.screenshot({
      path: testInfo.outputPath("workflow-results.png"),
      fullPage: true,
    });
    expect(errors).toEqual([]);
  });
}
