import { expect, type Page } from "@playwright/test";
import type {
  Agent,
  Project,
  Membership,
  Workflow,
} from "../../src/shared/api/generated/openapi/types.gen";

export async function checkOrganizationCatalog(
  page: Page,
  projects: Project[],
  invalidate: () => void,
  capture: () => Promise<void>,
): Promise<void> {
  const first = projects[0];
  const second = projects[1];
  if (!first || !second) throw new Error("Missing synthetic catalog projects");
  let version = 1;
  const requests: Array<{
    project: string | null;
    query: string | null;
    cursor: string | null;
  }> = [];
  const agent = (index: number, projectRef: string): Agent => ({
    ref: `agent_catalog_${String(index)}`,
    version,
    projectRef,
    name: `Сотрудник ${String(index)} ${"ДлинноеНазвание".repeat(10)}`,
    purpose: "Подробное назначение сотрудника ".repeat(12),
    roleDescription: "",
    state: "READY",
    enabled: true,
    system: false,
    runtimeRef: "runtime_synthetic",
    runtimeName: "Длинное имя модели ".repeat(10),
    runtimeReady: true,
    runtimeRevision: `revision-${String(version)}`,
    capabilities: [],
    integrations: [],
    knowledgeArtifactRefs: [],
    nextActions: [],
    updatedAt: "2026-09-05T00:00:00Z",
  });
  await page.route("**/api/v1/agents?**", async (route) => {
    const url = new URL(route.request().url());
    expect(url.searchParams.get("pageSize")).toBe("30");
    const project = url.searchParams.get("projectRef");
    const query = url.searchParams.get("query");
    const cursor = url.searchParams.get("pageToken");
    requests.push({ project, query, cursor });
    if (project) {
      expect(project).toBe(first.ref);
      await route.fulfill({
        json: { items: [agent(0, first.ref)], nextActions: [] },
      });
      return;
    }
    if (cursor) {
      expect(cursor).toBe("catalog_next");
      await route.fulfill({
        json: { items: [agent(8, first.ref)], nextActions: [] },
      });
      return;
    }
    await route.fulfill({
      json: {
        items: [
          ...Array.from({ length: 7 }, (_, i) => agent(i, first.ref)),
          agent(7, second.ref),
        ],
        nextPageToken: "catalog_next",
        nextActions: [],
      },
    });
  });
  await page.goto("/agents");
  const catalog = page.locator(".organization-catalog").first();
  const rows = catalog.locator(".agent-card");
  await expect(rows).toHaveCount(8);
  await expect(catalog.locator(".organization-catalog__group")).toHaveCount(2);
  const group = catalog
    .locator(".organization-catalog__group")
    .filter({ has: page.getByRole("link", { name: first.name, exact: true }) });
  const list = group.locator(".organization-catalog__items");
  expect(
    await list.evaluate(
      (element) => element.scrollHeight > element.clientHeight,
    ),
  ).toBe(true);
  expect(
    await rows.evaluateAll((elements) =>
      elements.every((element) => {
        const row = element.getBoundingClientRect();
        const copy = element.firstElementChild?.getBoundingClientRect();
        return !!copy && copy.top >= row.top && copy.bottom <= row.bottom;
      }),
    ),
  ).toBe(true);
  await list.evaluate((element) => {
    element.scrollTop = element.scrollHeight;
  });
  await expect(rows).toHaveCount(9);
  version = 2;
  invalidate();
  await expect(rows).toHaveCount(8);
  await expect(rows.first()).toContainText("revision-2");
  expect(requests.at(-1)?.cursor).toBeNull();
  await group
    .getByRole("button", { name: "Развернуть список проекта", exact: true })
    .click();
  const dialog = page.getByRole("dialog", { name: first.name, exact: true });
  await expect(dialog.locator(".agent-card")).toHaveCount(1);
  await dialog
    .getByRole("searchbox", { name: "Поиск", exact: true })
    .fill("Сотрудник");
  await expect
    .poll(() => requests.at(-1))
    .toEqual({ project: first.ref, query: "Сотрудник", cursor: null });
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
  await capture();
  await dialog
    .getByRole("button", { name: "Закрыть", exact: true })
    .last()
    .click();
  const memberRequests: Array<{
    project: string | null;
    query: string | null;
    cursor: string | null;
  }> = [];
  const member = (index: number, projectRef: string): Membership => ({
    ref: `membership_synthetic_${String(index)}`,
    projectRef,
    version: 1,
    user: {
      ref: `user_synthetic_${String(index)}`,
      displayName: `Участник ${String(index)}`,
    },
    platformRole: "MEMBER",
    permissions: ["VIEW"],
    active: true,
    nextActions: [],
  });
  await page.route("**/api/v1/project-memberships?**", async (route) => {
    const query = new URL(route.request().url()).searchParams;
    expect(query.get("pageSize")).toBe("30");
    const project = query.get("projectRef");
    const cursor = query.get("pageToken");
    memberRequests.push({ project, cursor, query: query.get("query") });
    if (project) {
      expect(project).toBe(first.ref);
      await route.fulfill({ json: { items: [member(0, first.ref)] } });
    } else if (cursor) {
      expect(cursor).toBe("members_next");
      await route.fulfill({ json: { items: [member(8, first.ref)] } });
    } else {
      await route.fulfill({
        json: {
          items: [
            ...Array.from({ length: 7 }, (_, index) =>
              member(index, first.ref),
            ),
            member(7, second.ref),
          ],
          nextPageToken: "members_next",
        },
      });
    }
  });
  await page.goto("/members");
  const memberRows = catalog.locator(".organization-catalog__entry");
  await expect(memberRows).toHaveCount(8);
  const navigation = page.locator('nav[aria-label="Навигация Проекта"]');
  for (const path of [
    "agents",
    "workflows",
    "runs",
    "files",
    "automations",
    "environments",
    "secrets",
    "members",
  ])
    await expect(navigation.locator(`a[href="/${path}"]`)).toHaveCount(1);
  await expect(navigation.locator('a[href="/members"]')).toHaveClass(
    /nav-link--active/,
  );
  await expect(memberRows.first()).toContainText("Участник");
  await expect(catalog.locator(".organization-catalog__group")).toHaveCount(2);
  await list.evaluate((element) => {
    element.scrollTop = element.scrollHeight;
  });
  await expect(memberRows).toHaveCount(9);
  await group
    .getByRole("button", { name: "Развернуть список проекта", exact: true })
    .click();
  await expect(dialog.locator(".organization-catalog__entry")).toHaveCount(1);
  await dialog
    .getByRole("searchbox", { name: "Поиск", exact: true })
    .fill("Участник");
  await expect
    .poll(() => memberRequests.at(-1))
    .toEqual({ project: first.ref, query: "Участник", cursor: null });
  await expect(
    dialog.locator(".organization-catalog__entry").first(),
  ).toHaveAttribute("href", `/projects/${first.ref}/members`);
  await dialog
    .getByRole("button", { name: "Закрыть", exact: true })
    .last()
    .click();

  const workflowRequests: typeof requests = [];
  const workflow = (index: number, projectRef: string): Workflow => ({
    ref: `workflow_catalog_${String(index)}`,
    version,
    projectRef,
    name: `Процесс каталога ${String(index)} ${"ДлинноеНазвание".repeat(8)}`,
    purpose: "Назначение процесса ".repeat(20),
    state: index === 0 ? "PUBLISHED" : "DRAFT",
    revisionRef: `workflow_catalog_revision_${String(index)}`,
    publishedRevisionRef:
      index === 0 ? "workflow_catalog_revision_0" : undefined,
    launchReadiness: {
      allowedToSubmit: index === 0,
      reason: index === 0 ? "READY" : "UNPUBLISHED",
      workflowVersion: version,
      revisionRef: index === 0 ? "workflow_catalog_revision_0" : undefined,
      contextDigest: "a".repeat(64),
      operationalState: "READY",
    },
    inputFields: [],
    steps: [],
    validationMessages: [],
    updatedAt: "2026-08-01T00:00:00Z",
    cardSummary: {
      stageCount: 31,
      uniqueAgentCount: 11,
      parallelGroupCount: 5,
      hasHumanGate: true,
      activeRunCount: version,
      pendingGateCount: 3,
      lastActivityAt: "2026-09-06T12:34:00Z",
    },
    nextActions: index === 0 ? ["EDIT"] : [],
  });
  await page.route("**/api/v1/workflows?**", async (route) => {
    const url = new URL(route.request().url());
    expect(url.searchParams.get("pageSize")).toBe("30");
    const project = url.searchParams.get("projectRef");
    const query = url.searchParams.get("query");
    const cursor = url.searchParams.get("pageToken");
    workflowRequests.push({ project, query, cursor });
    if (project) {
      expect(project).toBe(first.ref);
      await route.fulfill({ json: { items: [workflow(0, first.ref)] } });
    } else if (cursor) {
      expect(cursor).toBe("workflow_catalog_next");
      await route.fulfill({ json: { items: [workflow(8, first.ref)] } });
    } else {
      await route.fulfill({
        json: {
          items: [
            ...Array.from({ length: 7 }, (_, index) =>
              workflow(index, first.ref),
            ),
            workflow(7, second.ref),
          ],
          nextPageToken: "workflow_catalog_next",
        },
      });
    }
  });
  // Project route и realtime AGENT читают тот же каталог без зависимости от предыдущих сценариев.
  const projectAgentVersions: number[] = [];
  await page.route(`**/api/v1/projects/${first.ref}/agents?**`, (route) => {
    expect(route.request().method()).toBe("GET");
    expect(new URL(route.request().url()).searchParams.get("pageSize")).toBe(
      "100",
    );
    projectAgentVersions.push(version);
    return route.fulfill({ json: { items: [agent(0, first.ref)] } });
  });
  await page.goto("/workflows");
  const workflowCards = catalog.locator(".workflow-card");
  const projectWorkflowCards = group.locator(".workflow-card");
  await expect(workflowCards).toHaveCount(8);
  await expect(catalog.locator(".organization-catalog__group")).toHaveCount(2);
  await expect(
    workflowCards.first().locator('[data-metric="stageCount"] dd'),
  ).toHaveText("31");
  await expect(
    workflowCards.first().locator('[data-metric="uniqueAgentCount"] dd'),
  ).toHaveText("11");
  await expect(
    projectWorkflowCards
      .first()
      .getByRole("link", { name: "Изменить", exact: true }),
  ).toHaveAttribute(
    "href",
    `/projects/${first.ref}/workflows/workflow_catalog_0#workflow-editor`,
  );
  await expect(
    projectWorkflowCards
      .nth(1)
      .getByRole("button", { name: "Запустить", exact: true }),
  ).toBeDisabled();
  expect(
    await list.evaluate(
      (element) => element.scrollHeight > element.clientHeight,
    ),
  ).toBe(true);
  await list.evaluate((element) => {
    element.scrollTop = element.scrollHeight;
  });
  await expect(workflowCards).toHaveCount(9);
  await group
    .getByRole("button", { name: "Развернуть список проекта", exact: true })
    .click();
  await expect(dialog.locator(".workflow-card")).toHaveCount(1);
  await dialog
    .getByRole("searchbox", { name: "Поиск", exact: true })
    .fill("Процесс");
  await expect
    .poll(() => workflowRequests.at(-1))
    .toEqual({ project: first.ref, query: "Процесс", cursor: null });
  await dialog
    .getByRole("button", { name: "Закрыть", exact: true })
    .last()
    .click();
  await page.goto(`/projects/${first.ref}/workflows`);
  await expect(workflowCards).toHaveCount(1);
  expect(workflowRequests.at(-1)?.project).toBe(first.ref);
  await expect(
    workflowCards.first().getByRole("link", { name: "Запуски", exact: true }),
  ).toHaveAttribute("href", `/projects/${first.ref}/runs`);
  version = 3;
  invalidate();
  await expect(
    workflowCards.first().locator('[data-metric="activeRunCount"] dd'),
  ).toHaveText("3");
  expect(projectAgentVersions).toContain(3);
  expect(workflowRequests.at(-1)?.cursor).toBeNull();
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
}
