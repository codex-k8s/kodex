import { expect, test } from "@playwright/test";

test("общая форма сохраняет строки SSH-ключа и не меняет ввод других credentials", async ({
  page,
}) => {
  await page.route("**/*", async (route) => {
    const url = new URL(route.request().url());
    if (url.pathname.startsWith("/api/"))
      throw new Error("unexpected credential fixture API request");
    await route.fulfill({
      response: await route.fetch({
        url: `http://127.0.0.1:43122${url.pathname}${url.search}`,
      }),
    });
  });
  await page.goto("/e2e/fixtures/ssh-credential-input.html");
  for (const key of ["ssh-managed", "ssh-execute"]) {
    await page.getByLabel("Профиль ввода").selectOption(key);
    const input = page.locator("textarea");
    await expect(input).toHaveCount(1);
    await expect(page.locator('input[type="password"]')).toHaveCount(0);
    await input.fill(
      "synthetic-private-line-one\nsynthetic-private-line-two\n",
    );
    await expect(input).toHaveValue(
      "synthetic-private-line-one\nsynthetic-private-line-two\n",
    );
    await page
      .getByRole("button", { name: "Завершить однократный ввод" })
      .click();
    await expect(input).toHaveValue("");
  }
  await page.getByLabel("Профиль ввода").selectOption("sentry");
  await expect(page.locator("textarea")).toHaveCount(0);
  await expect(page.locator('input[type="password"]')).toHaveCount(1);
});
