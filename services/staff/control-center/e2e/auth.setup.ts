import { chmod, mkdir } from "node:fs/promises";
import { dirname } from "node:path";

import { authenticateOwner } from "./auth-flow";
import { loadE2EAuthEnvironment } from "./environment";
import { expect, test } from "./fixtures";

const environment = loadE2EAuthEnvironment();

test("владелец входит через настроенный OIDC", async ({ page, context }) => {
  // Trace, video и screenshot отключены в auth config: credentials не попадают
  // в reporter или browser artifacts.
  await authenticateOwner(page, {
    username: environment.ownerUsername,
    password: environment.ownerPassword,
  });

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
