import { expect, test } from "@playwright/test";

import { authenticateOwner } from "./auth-flow";
import { loadE2EAuthEnvironment } from "./environment";
import { withoutKodexAPICookies, writeStorageState } from "./storage-state";

const environment = loadE2EAuthEnvironment();

test("владелец входит через настроенный OIDC", async ({ page, context }) => {
  // Trace, video и screenshot отключены в auth config: credentials не попадают
  // в reporter или browser artifacts.
  const sessionResponse = page.waitForResponse(
    (response) =>
      response.request().method() === "POST" &&
      new URL(response.url()).origin === environment.baseURL &&
      new URL(response.url()).pathname === "/api/v1/session",
  );
  await authenticateOwner(
    page,
    {
      username: environment.ownerUsername,
      password: environment.ownerPassword,
    },
    { mode: "cold" },
  );
  expect((await sessionResponse).status()).toBe(204);

  await expect(page).toHaveURL(
    new RegExp(`^${escapeRegExp(environment.baseURL)}/`),
  );
  await expect(
    page.getByRole("button", { name: "Выйти", exact: true }),
  ).toBeVisible();

  const bootstrap = withoutKodexAPICookies(await context.storageState());
  await writeStorageState(environment.outputStorageState, bootstrap);
});

function escapeRegExp(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}
