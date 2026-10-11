import { expect, type Page } from "@playwright/test";
import {
  assertSyntheticAdaptivePageSize,
  type SyntheticSnapshotInstaller,
} from "./runs-catalog";
import type {
  Agent,
  Project,
  Membership,
  Workflow,
} from "../../src/shared/api/generated/openapi/types.gen";
import type { PlatformCatalogSnapshot } from "../../src/shared/api/generated/asyncapi/PlatformCatalogSnapshot";

export async function installSyntheticMembershipProfile(
  page: Page,
  projectRef: string,
  readMembers: (scope?: string) => Membership[],
  installSnapshot?: SyntheticSnapshotInstaller,
): Promise<void> {
  const read = (scope?: string): PlatformCatalogSnapshot => {
    const memberships = structuredClone(readMembers(scope));
    return {
      memberships,
      page: scope ? {} : { nextPageToken: "members_next" },
      nextActions: [],
      // Определение из канонического access/engine.go; оно не выдаёт полномочия.
      permissions: [
        {
          key: "project.view",
          nameKey: "i18n:PERMISSION_PROJECT_VIEW_NAME",
          descriptionKey: "i18n:PERMISSION_PROJECT_VIEW_DESCRIPTION",
          risk: "READ",
          allowedScopes: [
            "ORGANIZATION",
            "PROJECT",
            "RESOURCE_KIND",
            "RESOURCE_INSTANCE",
          ],
          resourceKinds: ["PROJECT", "ROLE_IMAGE"],
          ownerConditionSupported: false,
        },
      ],
      accessSubjects: memberships.map(({ user, active }) => ({
        ref: user.ref,
        kind: "USER",
        displayName: user.displayName,
        active,
        oidcGroupRefs: [],
      })),
      accessSubjectsPage: {},
      oidcGroups: [],
      oidcGroupsPage: {},
      accessRoles: [],
      accessRolesPage: {},
      accessBindings: [],
      accessBindingsPage: {},
    };
  };
  installSnapshot?.("MEMBERSHIP", (scope) =>
    scope && scope !== projectRef ? undefined : { catalog: read(scope) },
  );
  const fields = {
    permissions: "permissions",
    subjects: "accessSubjects",
    "oidc-groups": "oidcGroups",
    roles: "accessRoles",
    bindings: "accessBindings",
  } as const;
  await page.route("**/api/v1/administration/access/*", (route) => {
    expect(route.request().method()).toBe("GET");
    const url = new URL(route.request().url());
    const scope = url.searchParams.get("projectRef") ?? undefined;
    if (scope) expect(scope).toBe(projectRef);
    const name = url.pathname.split("/").at(-1);
    if (!name || !(name in fields))
      throw new Error("Unknown synthetic access catalog");
    const field = fields[name as keyof typeof fields];
    return route.fulfill({ json: { items: read(scope)[field] } });
  });
}

export async function checkOrganizationCatalog(
  page: Page,
  projects: Project[],
  invalidate: (
    agents: Agent[],
    projectRef?: string,
    workflows?: Workflow[],
  ) => void,
  capture: () => Promise<void>,
  installSnapshot?: SyntheticSnapshotInstaller,
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
    runtimeModel: `revision-${String(version)}`,
    runtimeReady: true,
    runtimeRevision: `revision-${String(version)}`,
    capabilities: [],
    integrations: [],
    knowledgeArtifactRefs: [],
    nextActions: [],
    updatedAt: "2026-09-05T00:00:00Z",
  });
  const projectAgentVersions: number[] = [];
  const currentAgents = () => [
    ...Array.from({ length: 7 }, (_, index) => agent(index, first.ref)),
    agent(7, second.ref),
    ...(version > 1 ? [agent(8, first.ref)] : []),
  ];
  await page.route(`**/api/v1/projects/${first.ref}/agents?**`, (route) => {
    expect(route.request().method()).toBe("GET");
    const params = new URL(route.request().url()).searchParams;
    assertSyntheticAdaptivePageSize(params.get("pageSize"));
    projectAgentVersions.push(version);
    if (params.get("query"))
      requests.push({
        project: first.ref,
        query: params.get("query"),
        cursor: params.get("pageToken"),
      });
    return route.fulfill({ json: { items: [agent(0, first.ref)] } });
  });
  await page.route("**/api/v1/agents?**", async (route) => {
    const url = new URL(route.request().url());
    assertSyntheticAdaptivePageSize(url.searchParams.get("pageSize"));
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
        items: currentAgents(),
        ...(version === 1 ? { nextPageToken: "catalog_next" } : {}),
        nextActions: [],
      },
    });
  });
  installSnapshot?.("AGENT", (scope) =>
    scope && scope !== first.ref
      ? undefined
      : {
          catalog: {
            agents:
              scope === first.ref
                ? [agent(0, first.ref)]
                : scope
                  ? []
                  : currentAgents(),
            page: scope || version > 1 ? {} : { nextPageToken: "catalog_next" },
          },
        },
  );
  await page.goto("/agents");
  const catalog = page.locator(".organization-catalog").first();
  const rows = catalog.locator(".organization-catalog__row");
  await expect(rows.nth(7)).toBeVisible();
  const identity = rows.locator(".organization-catalog__identity a");
  const projectLinks = rows.locator(".organization-catalog__project-link");
  await expect(projectLinks.filter({ hasText: second.name })).toHaveCount(1);
  expect(
    await rows.evaluateAll((elements) =>
      elements.every((element) => {
        const row = element.getBoundingClientRect();
        const copy = element
          .querySelector(".organization-catalog__identity")
          ?.getBoundingClientRect();
        return !!copy && copy.top >= row.top && copy.bottom <= row.bottom;
      }),
    ),
  ).toBe(true);
  await catalog
    .locator(".organization-catalog__sentinel")
    .scrollIntoViewIfNeeded();
  await expect(rows).toHaveCount(9);
  await expect(identity).toHaveText(
    Array.from(
      { length: 9 },
      (_, index) => agent(index, index === 7 ? second.ref : first.ref).name,
    ),
  );
  version = 2;
  const readsBeforeDelta = requests.length;
  invalidate(currentAgents());
  await expect(rows).toHaveCount(9);
  await expect(identity).toHaveText(currentAgents().map((item) => item.name));
  await expect(rows.first()).toContainText("revision-2");
  expect(requests).toHaveLength(readsBeforeDelta);
  await projectLinks.first().click();
  await expect(page).toHaveURL(`/projects/${first.ref}/agents`);
  const projectAgents = page.locator(".agent-table tbody tr");
  // Проектный каталог сотрудников использует собственную таблицу.
  await expect(projectAgents).toHaveCount(1);
  await page
    .getByRole("searchbox", { name: "Поиск сотрудников", exact: true })
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
    assertSyntheticAdaptivePageSize(query.get("pageSize"));
    const project = query.get("projectRef");
    const cursor = query.get("pageToken");
    memberRequests.push({ project, cursor, query: query.get("query") });
    if (project || query.get("query")) {
      if (project) expect(project).toBe(first.ref);
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
  await installSyntheticMembershipProfile(
    page,
    first.ref,
    (scope) =>
      scope
        ? [member(0, first.ref)]
        : [
            ...Array.from({ length: 7 }, (_, index) =>
              member(index, first.ref),
            ),
            member(7, second.ref),
          ],
    installSnapshot,
  );
  await page.goto("/members");
  const memberRows = catalog.locator(".organization-catalog__row");
  // Общий sentinel может уже загрузить следующую страницу короткого списка.
  await expect(memberRows.nth(7)).toBeVisible();
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
  await expect(
    memberRows
      .locator(".organization-catalog__project-link")
      .filter({ hasText: second.name }),
  ).toHaveCount(1);
  await catalog
    .locator(".organization-catalog__sentinel")
    .scrollIntoViewIfNeeded();
  await expect(memberRows).toHaveCount(9);
  expect(memberRequests.map((request) => request.cursor)).toEqual([
    "members_next",
  ]);
  await expect(
    memberRows.locator(".organization-catalog__project-link").first(),
  ).toHaveAttribute("href", `/projects/${first.ref}/members`);
  await catalog
    .getByRole("searchbox", {
      name: "Найти по участнику, email или Проекту",
      exact: true,
    })
    .fill("Участник");
  await expect
    .poll(() => memberRequests.at(-1))
    .toEqual({ project: null, query: "Участник", cursor: null });
  await expect(memberRows).toHaveCount(1);
  await expect(
    memberRows.locator(".organization-catalog__identity a").first(),
  ).toHaveAttribute(
    "href",
    `/projects/${first.ref}/members?memberRef=user_synthetic_0`,
  );

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
    assertSyntheticAdaptivePageSize(url.searchParams.get("pageSize"));
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
  installSnapshot?.("WORKFLOW", (scope) =>
    scope && scope !== first.ref
      ? undefined
      : {
          catalog: {
            workflows:
              scope === first.ref
                ? [workflow(0, first.ref)]
                : scope
                  ? []
                  : [
                      ...Array.from({ length: 7 }, (_, index) =>
                        workflow(index, first.ref),
                      ),
                      workflow(7, second.ref),
                    ],
            page: scope ? {} : { nextPageToken: "workflow_catalog_next" },
          },
        },
  );
  await page.goto("/workflows");
  const workflowCards = catalog.locator(".organization-catalog__row");
  await expect(workflowCards.nth(7)).toBeVisible();
  await expect(
    workflowCards
      .locator(".organization-catalog__project-link")
      .filter({ hasText: second.name }),
  ).toHaveCount(1);
  await expect(
    workflowCards
      .first()
      .getByTitle("Этапов: 31 · сотрудников: 11 · ожидают решения: 3", {
        exact: true,
      }),
  ).toBeVisible();
  await expect(
    workflowCards.first().getByRole("link", { name: "Изменить", exact: true }),
  ).toHaveAttribute(
    "href",
    `/projects/${first.ref}/workflows/workflow_catalog_0#workflow-editor`,
  );
  await expect(
    workflowCards
      .nth(1)
      .getByRole("button", { name: "Запустить", exact: true }),
  ).toBeDisabled();
  await catalog
    .locator(".organization-catalog__sentinel")
    .scrollIntoViewIfNeeded();
  await expect(workflowCards).toHaveCount(9);
  await workflowCards
    .locator(".organization-catalog__project-link")
    .first()
    .click();
  await expect(workflowCards).toHaveCount(1);
  await catalog
    .getByRole("searchbox", {
      name: "Найти по названию, описанию или Проекту",
      exact: true,
    })
    .fill("Процесс");
  await expect
    .poll(() => workflowRequests.at(-1))
    .toEqual({ project: first.ref, query: "Процесс", cursor: null });
  await page.goto(`/projects/${first.ref}/workflows`);
  await expect(workflowCards).toHaveCount(1);
  expect(workflowRequests.at(-1)?.project).toBe(first.ref);
  await expect(
    workflowCards.first().getByRole("link", { name: "Запуски", exact: true }),
  ).toHaveAttribute("href", `/projects/${first.ref}/runs`);
  version = 3;
  const projectAgentReadsBeforeDelta = projectAgentVersions.length;
  invalidate([agent(0, first.ref)], first.ref, [workflow(0, first.ref)]);
  await expect(
    workflowCards.first().getByText("Активных запусков: 3", { exact: true }),
  ).toBeVisible();
  expect(projectAgentVersions).toHaveLength(projectAgentReadsBeforeDelta);
  expect(workflowRequests.at(-1)?.cursor).toBeNull();
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
}
