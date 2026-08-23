import { chmod, mkdir } from "node:fs/promises";
import { dirname } from "node:path";

import { expect, test } from "@playwright/test";

import { loadE2EAuthEnvironment } from "./environment";

const environment = loadE2EAuthEnvironment();

test("владелец входит через настроенный OIDC", async ({ page, context }) => {
  await page.goto("/");
  await page.getByRole("button", { name: "Войти", exact: true }).click();

  // Поставляемый профиль использует стандартную форму Keycloak. Пароль не
  // попадает в reporter, trace, video или screenshot.
  await page.locator('input[name="username"]').fill(environment.ownerUsername);
  await page.locator('input[name="password"]').fill(environment.ownerPassword);
  await page.locator('button[type="submit"], input[type="submit"]').click();

  await expect(page).toHaveURL(
    new RegExp(`^${escapeRegExp(environment.baseURL)}/`),
  );
  await expect(
    page.getByRole("button", { name: "Выйти", exact: true }),
  ).toBeVisible();

  await mkdir(dirname(environment.outputStorageState), {
    recursive: true,
    mode: 0o700,
  });
  await context.storageState({ path: environment.outputStorageState });
  await chmod(environment.outputStorageState, 0o600);
});

function escapeRegExp(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}
