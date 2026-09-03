import { expect, test } from "@playwright/test";

import { authenticateOwner } from "./auth-flow";
import { loadE2EAuthEnvironment } from "./environment";
import { gotoWithRetry } from "./helpers";
import { withoutKodexAPICookies, writeStorageState } from "./storage-state";

const environment = loadE2EAuthEnvironment();
const topLevelRoutes = [
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
  const currentUserMenu = page.locator("button[aria-haspopup='menu']");
  const logout = page.getByRole("button", { name: "Выйти", exact: true });
  await currentUserMenu.click();
  await expect(logout).toBeVisible();
  await page.locator("main").getByRole("heading").first().click();
  await expect(logout).toBeHidden();

  const projectsReadback = await page.evaluate(async () => {
    const response = await fetch("/api/v1/projects?pageSize=1");
    return {
      status: response.status,
      problem: response.ok ? "" : await response.text(),
    };
  });
  expect(projectsReadback.status, projectsReadback.problem).toBe(200);

  const oidcGroups = await page.evaluate(async (expectedGroup) => {
    const response = await fetch(
      `/api/v1/administration/access/oidc-groups?query=${encodeURIComponent(expectedGroup)}&pageSize=10`,
    );
    if (!response.ok) {
      throw new Error(
        `OIDC group readback failed with ${String(response.status)}`,
      );
    }
    return (await response.json()) as {
      items: Array<{
        displayName: string;
        memberCount: number;
        state: string;
      }>;
    };
  }, environment.rbacGroup);
  const exactOIDCGroups = oidcGroups.items.filter(
    (group) => group.displayName === environment.rbacGroup,
  );
  expect(exactOIDCGroups).toHaveLength(1);
  expect(exactOIDCGroups[0]).toMatchObject({ state: "ACTIVE" });
  expect(exactOIDCGroups[0]?.memberCount).toBeGreaterThanOrEqual(1);

  await gotoWithRetry(page, "/onboarding");
  await expect(
    page.getByRole("heading", {
      level: 1,
      name: /^(Настроим Kodex|Проекты|Добрый день, .+)$/,
    }),
  ).toBeVisible();

  for (const [path, heading] of topLevelRoutes) {
    await gotoWithRetry(page, path);
    await expect(
      page.getByRole("heading", { level: 1, name: heading }),
    ).toBeVisible();
  }
  await page.getByRole("button", { name: "Открыть Kodex" }).click();
  await expect(page.getByRole("dialog", { name: "Kodex" })).toBeVisible();
  expect(browserFailures).toEqual([]);
  await writeStorageState(
    environment.outputStorageState,
    withoutKodexAPICookies(await context.storageState()),
  );
});
