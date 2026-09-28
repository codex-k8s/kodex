import { expect, test } from "@playwright/test";

import { authenticateOwner } from "./auth-flow";
import { expectAssistantConversationActions } from "./assistant-readiness";
import { loadE2EAuthEnvironment } from "./environment";
import { gotoWithRetry } from "./helpers";
import { installProtocolObserver } from "./session-renewal-proof";
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
  await context.addInitScript(installProtocolObserver);
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
  expect((await session).status()).toBe(200);
  await expect
    .poll(
      () =>
        page.evaluate(
          () =>
            (
              window as unknown as {
                __kodexSessionProofProtocols?: string[];
              }
            ).__kodexSessionProofProtocols?.includes("v2") ?? false,
        ),
      { timeout: 15_000 },
    )
    .toBe(true);
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
  const httpsJSONDefinition = await page.evaluate(async () => {
    const response = await fetch("/api/v1/integration-definitions?pageSize=30");
    if (!response.ok)
      throw new Error(
        `Integration catalog readback failed with ${String(response.status)}`,
      );
    const catalog = (await response.json()) as {
      items: Array<{
        key: string;
        adapter: string;
        credentialSecretKey?: string;
        available: boolean;
      }>;
    };
    return catalog.items.find((item) => item.key === "https-json");
  });
  expect(httpsJSONDefinition).toMatchObject({
    key: "https-json",
    adapter: "HTTPS_JSON_READ",
    credentialSecretKey: "token",
    available: true,
  });
  await page.getByRole("button", { name: "Открыть Kodex" }).click();
  const assistant = page.getByRole("dialog", { name: "Kodex" });
  await expect(assistant).toBeVisible();
  const assistantReadback = await page.evaluate(async () => {
    const response = await fetch("/api/v1/system-assistant");
    if (!response.ok)
      throw new Error(
        `System assistant readback failed with ${String(response.status)}`,
      );
    return (await response.json()) as {
      corePromptRevision: string;
      runtimeState: string;
      warmSessionRef?: string;
      nextActions: string[];
    };
  });
  expect(assistantReadback.corePromptRevision).toBe("system-assistant-core-v21");
  const providerAccountRequired =
    !assistantReadback.warmSessionRef &&
    !assistantReadback.nextActions.includes("CREATE_CONVERSATION");
  await expectAssistantConversationActions(assistant, !providerAccountRequired);
  if (providerAccountRequired) {
    await expect(
      assistant.getByRole("heading", { name: "Подключите аккаунт модели" }),
    ).toBeVisible();
    await expect(
      assistant.getByRole("link", { name: "Перейти к аккаунтам моделей" }),
    ).toBeVisible();
  }
  await assistant.getByRole("button", { name: "Закрыть" }).click();
  await expect(assistant).toHaveCount(0);

  const baseProjectName = "Локальная приёмка первого запуска";
  const existingProject = await page.evaluate(async (exactName) => {
    const response = await fetch(
      `/api/v1/projects?query=${encodeURIComponent(exactName)}&pageSize=30`,
    );
    if (!response.ok)
      throw new Error(
        `Project discovery failed with ${String(response.status)}`,
      );
    const body = (await response.json()) as {
      items: Array<{ name: string; ref: string }>;
    };
    return (
      body.items.find((item) => item.name === exactName) ??
      body.items.find((item) => item.name.startsWith(`${exactName} · `))
    );
  }, baseProjectName);
  let projectName = existingProject?.name ?? baseProjectName;
  let projectRef = existingProject?.ref;
  if (!projectRef) {
    const exactNameInTrash = await page.evaluate(async (exactName) => {
      let pageToken = "";
      for (let pageIndex = 0; pageIndex < 10; pageIndex += 1) {
        const suffix = pageToken
          ? `&pageToken=${encodeURIComponent(pageToken)}`
          : "";
        const response = await fetch(
          `/api/v1/projects/trash?pageSize=30${suffix}`,
        );
        if (!response.ok)
          throw new Error(
            `Project trash discovery failed with ${String(response.status)}`,
          );
        const body = (await response.json()) as {
          items: Array<{ name: string }>;
          nextPageToken?: string;
        };
        if (body.items.some((item) => item.name === exactName)) return true;
        if (!body.nextPageToken) return false;
        pageToken = body.nextPageToken;
      }
      throw new Error("Project trash discovery page budget exhausted");
    }, baseProjectName);
    if (exactNameInTrash)
      projectName = `${baseProjectName} · ${Date.now().toString(36)}`;
    await gotoWithRetry(page, "/projects");
    await page
      .getByRole("button", { name: "Новый Проект", exact: true })
      .first()
      .click();
    const dialog = page.getByRole("dialog", { name: "Новый Проект" });
    await dialog.getByLabel("Название", { exact: true }).fill(projectName);
    await dialog
      .getByLabel("Назначение", { exact: true })
      .fill(
        "Безопасная локальная проверка чтения и записи без provider account.",
      );
    const created = page.waitForResponse(
      (response) =>
        response.request().method() === "POST" &&
        new URL(response.url()).pathname === "/api/v1/projects",
    );
    await dialog.getByRole("button", { name: "Создать", exact: true }).click();
    const response = await created;
    const body = (await response.json()) as { ref?: string };
    expect(response.status(), JSON.stringify(body)).toBe(201);
    projectRef = body.ref;
  }
  expect(projectRef).toMatch(/^prj_[A-Za-z0-9_-]+$/);
  await gotoWithRetry(
    page,
    `/projects/${encodeURIComponent(projectRef ?? "")}`,
  );
  await expect(
    page.getByRole("heading", { level: 1, name: projectName }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Открыть Kodex" }).click();
  const projectAssistant = page.getByRole("dialog", { name: "Kodex" });
  await projectAssistant
    .getByRole("link", { name: "Открыть защищённую форму нового секрета" })
    .click();
  await expect(page).toHaveURL(
    new RegExp(
      `/projects/${encodeURIComponent(projectRef ?? "")}/secrets\\?assistantCreateSecret=1`,
    ),
  );
  await expect(
    page.getByRole("dialog", { name: "Новый секрет" }),
  ).toBeVisible();
  expect(browserFailures).toEqual([]);
  await writeStorageState(
    environment.outputStorageState,
    withoutKodexAPICookies(await context.storageState()),
  );
});

test("локальный OpenAPI импорт проверяет контракт без записи", async ({
  page,
}) => {
  await authenticateOwner(
    page,
    {
      username: environment.ownerUsername,
      password: environment.ownerPassword,
    },
    { mode: "local" },
  );
  await gotoWithRetry(
    page,
    "/configurations/INTEGRATION_DEFINITION?assistantImportOpen=1",
  );
  const importDialog = page.getByRole("dialog", {
    name: "Импорт интеграции из OpenAPI",
  });
  await importDialog.getByLabel("Контракт OpenAPI JSON или YAML")
    .fill(`openapi: 3.1.0
info: {title: Заявки, version: 1.0.0}
servers:
  - url: https://api.example.test
paths:
  /health:
    get:
      operationId: getHealth
      summary: Проверить соединение
      responses:
        '200': {description: OK}
`);
  const inspected = page.waitForResponse(
    (response) =>
      response.request().method() === "POST" &&
      new URL(response.url()).pathname ===
        "/api/v1/integration-definition-configurations/openapi-inspections",
  );
  await importDialog
    .getByRole("button", { name: "Проверить контракт" })
    .click();
  expect((await inspected).status()).toBe(200);
  await expect(importDialog.getByText("Проверить соединение")).toBeVisible();
  await importDialog.getByRole("button", { name: "Отмена" }).click();
  await expect(importDialog).toHaveCount(0);
});
