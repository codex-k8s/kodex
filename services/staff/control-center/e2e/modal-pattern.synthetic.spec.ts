import { expect, test } from "@playwright/test";

test.beforeEach(async ({ page }) => {
  await page.route("**/*", async (route) => {
    const url = new URL(route.request().url());
    if (url.origin !== "https://kodex.test" || url.pathname.startsWith("/api/"))
      throw new Error("Unexpected modal fixture request");
    await route.fulfill({
      response: await route.fetch({
        url: `http://127.0.0.1:43122${url.pathname}`,
      }),
    });
  });
  await page.goto("/e2e/fixtures/modal-pattern.html");
});

test("synthetic: настоящий Modal сохраняет focus, Tab/Escape и возврат", async ({
  page,
}) => {
  const opener = page.getByRole("button", { name: "Открыть", exact: true });
  await opener.click();
  await expect(page.getByLabel("Название", { exact: true })).toBeFocused();
  await page.getByLabel("Организация", { exact: true }).fill("synthetic-owner");
  await expect(page.getByLabel("Название", { exact: true })).toHaveValue(
    "GitHub",
  );
  await page.getByRole("button", { name: "Готово", exact: true }).focus();
  await page.keyboard.press("Tab");
  await expect(
    page.getByRole("button", { name: "Закрыть", exact: true }),
  ).toBeFocused();
  await page.keyboard.press("Shift+Tab");
  await expect(
    page.getByRole("button", { name: "Готово", exact: true }),
  ).toBeFocused();
  await page.keyboard.press("Escape");
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await expect(opener).toBeFocused();
  await page
    .getByRole("button", { name: "Открыть с выбором поля", exact: true })
    .click();
  await expect(page.getByLabel("Организация", { exact: true })).toBeFocused();
  await page
    .getByLabel("Организация", { exact: true })
    .pressSequentially("synthetic-owner");
  await expect(page.getByLabel("Название", { exact: true })).toHaveValue(
    "GitHub",
  );
});

test("synthetic: canonical patterns работают через native checkValidity", async ({
  page,
}) => {
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  page.on("console", (message) => {
    if (message.type() === "error") errors.push(message.text());
  });
  await page.getByRole("button", { name: "Открыть", exact: true }).click();
  for (const [label, valid, invalid] of [
    ["Ключ", "synthetic-key.read", "invalid key"],
    ["Hostname", "synthetic-host.example", "invalid host"],
    ["Версия", "1.2.3", "invalid version"],
  ] as const) {
    const input = page.getByLabel(label, { exact: true });
    await input.fill(valid);
    expect(
      await input.evaluate((element: HTMLInputElement) =>
        element.checkValidity(),
      ),
    ).toBe(true);
    await input.fill(invalid);
    expect(
      await input.evaluate((element: HTMLInputElement) =>
        element.checkValidity(),
      ),
    ).toBe(false);
  }
  expect(errors).toEqual([]);
});
