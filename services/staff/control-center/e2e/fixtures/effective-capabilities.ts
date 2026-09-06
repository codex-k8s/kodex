import { expect, type Page } from "@playwright/test";
import type {
  Agent,
  AgentCommand,
  AgentEffectiveCapability,
  AgentEffectiveCapabilityPage,
} from "../../src/shared/api/generated/openapi/types.gen";

export const effectiveFiles: AgentEffectiveCapability = {
  key: "platform.artifact.manage",
  name: "Файлы",
  description: "Работа с файлами",
  source: "PLATFORM",
  reason: "AVAILABLE",
  requested: true,
  required: false,
  effective: true,
  grantable: true,
};
export const deniedLaunch: AgentEffectiveCapability = {
  key: "platform.run.launch",
  name: "Запуск задач",
  description: "Запуск других задач",
  source: "PLATFORM",
  reason: "ACTOR_PERMISSION_REQUIRED",
  requested: false,
  required: false,
  effective: false,
  grantable: false,
};
export function effectivePage(
  agent: Agent,
  items: AgentEffectiveCapability[],
): AgentEffectiveCapabilityPage {
  return {
    agentRef: agent.ref,
    agentVersion: agent.version,
    projectRef: agent.projectRef,
    runtimeConfigurationRef: "runtime_capabilities",
    runtimeConfigurationVersion: 1,
    environmentVersionRef: "environment_capabilities",
    digest: "a".repeat(64),
    evaluatedAt: "2026-09-05T00:00:00Z",
    runtimeReady: true,
    items,
    total: items.length,
  };
}

export async function checkEffectiveCapabilities(
  page: Page,
  projectRef: string,
) {
  let requested = true;
  let commands = 0;
  const agent: Agent = {
    ref: "agent_capabilities",
    version: 1,
    projectRef,
    name: "Проверка возможностей",
    purpose: "Проверка текущих полномочий",
    roleDescription: "Аналитик",
    state: "READY",
    enabled: true,
    system: false,
    runtimeRef: "runtime_capabilities",
    runtimeName: "Synthetic",
    runtimeReady: true,
    capabilities: [
      {
        key: effectiveFiles.key,
        name: effectiveFiles.name,
        description: effectiveFiles.description,
        category: "FILES",
        availableWithoutIntegration: true,
      },
    ],
    integrations: [],
    knowledgeArtifactRefs: [],
    updatedAt: "2026-09-05T00:00:00Z",
    nextActions: ["EDIT", "MANAGE_CAPABILITIES"],
  };
  const connections: AgentEffectiveCapability[] = ["one", "two"].map(
    (suffix, index) => ({
      ...effectiveFiles,
      key: "github.repository.read",
      name: "Чтение репозитория",
      source: "INTEGRATION",
      requested: true,
      effective: index === 0,
      grantable: false,
      reason: index === 0 ? "AVAILABLE" : "INTEGRATION_GRANT_UNAVAILABLE",
      connectionRef: `connection_${suffix}`,
      grantRef: `grant_${suffix}`,
      connectionVersion: 2,
      grantVersion: 3,
      definitionDigest: "b".repeat(64),
    }),
  );
  await page.route(`**/api/v1/agents/${agent.ref}`, (route) =>
    route.fulfill({ json: agent }),
  );
  await page.route(
    `**/api/v1/agents/${agent.ref}/instruction-versions*`,
    (route) => route.fulfill({ json: { items: [] } }),
  );
  await page.route(
    `**/api/v1/agents/${agent.ref}/effective-capabilities*`,
    (route) => {
      const url = new URL(route.request().url());
      expect(url.searchParams.get("pageSize")).toBe("30");
      const search = url.searchParams.get("query") ?? "";
      const more = Boolean(url.searchParams.get("pageToken"));
      const platform: AgentEffectiveCapability[] = [
        {
          ...effectiveFiles,
          requested,
          effective: false,
          grantable: false,
          reason: "ACTOR_PERMISSION_REQUIRED",
        },
        deniedLaunch,
      ];
      const items = search || more ? connections : platform;
      return route.fulfill({
        json: {
          ...effectivePage(agent, items),
          total: search ? 2 : 4,
          ...(!search && !more ? { nextPageToken: "capability_next" } : {}),
        },
      });
    },
  );
  await page.route(`**/api/v1/agents/${agent.ref}/commands`, async (route) => {
    const body = route.request().postDataJSON() as AgentCommand;
    expect(body).toEqual({
      action: "REVOKE_CAPABILITY",
      capabilityKey: effectiveFiles.key,
    });
    expect(route.request().headers()["if-match"]).toBe('"1"');
    expect(route.request().headers()["idempotency-key"]).toBeTruthy();
    commands++;
    requested = false;
    agent.version++;
    agent.capabilities = [];
    await route.fulfill({ json: agent });
  });
  await page.goto(`/projects/${projectRef}/agents/${agent.ref}?tab=access`);
  const catalog = page.locator(".effective-capabilities");
  await expect(
    catalog.getByText("Всего возможностей: 4", { exact: true }),
  ).toBeVisible();
  const files = catalog.getByRole("checkbox", { name: /^Файлы/ });
  await expect(files).toBeEnabled();
  await expect(files).toBeChecked();
  await expect(
    catalog.getByRole("checkbox", { name: /^Запуск задач/ }),
  ).toBeDisabled();
  await files.uncheck();
  await expect(files).not.toBeChecked();
  await expect(files).toBeDisabled();
  expect(commands).toBe(1);
  await catalog.getByRole("button", { name: "Загрузить ещё" }).click();
  await expect(catalog.locator(".effective-capabilities__row")).toHaveCount(4);
  await expect(catalog.getByText(/connection_one · grant_one/)).toBeVisible();
  await expect(catalog.getByText(/connection_two · grant_two/)).toBeVisible();
  await catalog.getByRole("searchbox").fill("репозиторий");
  await expect(catalog.locator(".effective-capabilities__row")).toHaveCount(2);
  await expect(
    catalog.getByText("Разрешение этого подключения недоступно."),
  ).toBeVisible();
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
  await page.evaluate(() => window.scrollTo(0, 0));
  await expect.poll(() => page.evaluate(() => window.scrollY)).toBe(0);
}
