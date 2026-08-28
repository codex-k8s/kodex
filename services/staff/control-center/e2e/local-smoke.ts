import { expect, test } from "@playwright/test";

import { authenticateOwner } from "./auth-flow";
import { loadE2EAuthEnvironment } from "./environment";
import { gotoWithRetry } from "./helpers";
import { withoutKodexAPICookies, writeStorageState } from "./storage-state";

const environment = loadE2EAuthEnvironment();
const topLevelRoutes = [
  ["/assistant", "Помощник Kodex"],
  ["/projects", "Проекты"],
  ["/runs", "Запуски"],
  ["/integrations", "Интеграции"],
  ["/decisions", "Решения"],
  ["/administration", "Администрирование"],
] as const;

test("локальный OIDC, API и основные экраны доступны", async ({
  context,
  page,
}) => {
  const browserFailures: string[] = [];
  page.on("pageerror", (error) => browserFailures.push(error.message));
  page.on("response", (response) => {
    if (response.status() >= 500) {
      browserFailures.push(
        `${String(response.status())} ${new URL(response.url()).pathname}`,
      );
    }
  });

  const session = page.waitForResponse(
    (response) =>
      response.request().method() === "POST" &&
      new URL(response.url()).pathname === "/api/v1/session",
  );
  await authenticateOwner(
    page,
    {
      username: environment.ownerUsername,
      password: environment.ownerPassword,
    },
    { mode: "local" },
  );
  expect((await session).status()).toBe(204);
  await page.locator("details.current-user-menu > summary").click();
  await expect(
    page.getByRole("button", { name: "Выйти", exact: true }),
  ).toBeVisible();

  const projectsStatus = await page.evaluate(async () =>
    fetch("/api/v1/projects?pageSize=1").then((response) => response.status),
  );
  expect(projectsStatus).toBe(200);

  await gotoWithRetry(page, "/onboarding");
  await expect(
    page.getByRole("heading", {
      level: 1,
      name: /^(Настроим Kodex|Проекты)$/,
    }),
  ).toBeVisible();

  for (const [path, heading] of topLevelRoutes) {
    await gotoWithRetry(page, path);
    await expect(
      page.getByRole("heading", { level: 1, name: heading }),
    ).toBeVisible();
  }
  expect(browserFailures).toEqual([]);
  await writeStorageState(
    environment.outputStorageState,
    withoutKodexAPICookies(await context.storageState()),
  );
});
