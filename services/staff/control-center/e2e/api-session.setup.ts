import { expect, test } from "@playwright/test";

import { authenticateOwner } from "./auth-flow";
import { loadE2EAPISessionEnvironment } from "./environment";
import { writeAuthenticatedStorageState } from "./storage-state";

const environment = loadE2EAPISessionEnvironment();

test("создаёт краткоживущую API-сессию владельца через warm OIDC", async ({
  context,
  page,
}) => {
  const sessionResponse = page.waitForResponse(
    (response) =>
      response.request().method() === "POST" &&
      new URL(response.url()).origin === environment.baseURL &&
      new URL(response.url()).pathname === "/api/v1/session/callback",
  );
  await authenticateOwner(page, undefined, { mode: "warm" });
  expect((await sessionResponse).status()).toBe(200);
  await expect(page.locator(".app-shell")).toBeVisible();
  await writeAuthenticatedStorageState(
    environment.outputStorageState,
    await context.storageState(),
    environment.baseURL,
  );
});
