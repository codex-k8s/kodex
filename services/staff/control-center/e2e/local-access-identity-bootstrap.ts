import { expect, test } from "@playwright/test";

import { authenticateOwner } from "./auth-flow";
import { loadE2EAuthEnvironment } from "./environment";

const environment = loadE2EAuthEnvironment();

test("локальная OIDC-личность без активного членства получает закрытый отказ", async ({
  page,
}) => {
  const callback = page.waitForResponse(
    (response) =>
      response.request().method() === "POST" &&
      new URL(response.url()).pathname === "/api/v1/session/callback",
  );
  await authenticateOwner(
    page,
    {
      username: environment.ownerUsername,
      password: environment.ownerPassword,
    },
    { mode: "local" },
  );

  expect((await callback).status()).toBe(200);
  await expect(
    page.getByRole("alert").filter({ hasText: "Недостаточно прав" }),
  ).toBeVisible();
  await expect(
    page
      .getByRole("alert")
      .filter({ hasText: "У вас нет прав на это действие" }),
  ).toBeVisible();
  await expect(page.getByRole("button", { name: /роль:/ })).toHaveCount(0);
});
