import { expect, test, type Locator } from "@playwright/test";
import type {
  Project,
  Run,
  IntegrationConnection,
  IntegrationCapability,
  VfsNode,
} from "../src/shared/api/generated/openapi/types.gen";
const project: Project = {
  ref: "project",
  version: 1,
  name: "Проект",
  purpose: "",
  language: "ru",
  lifecycle: "ACTIVE",
  agentCount: 1,
  integrationState: "NONE",
  workflowCount: 0,
  activeRunCount: 0,
  pendingGateCount: 0,
  updatedAt: "2026-09-06T00:00:00Z",
  nextActions: [],
};
const capability: IntegrationCapability = {
  key: "read",
  name: "Чтение репозитория",
  description: "",
  risk: "READ",
  approvalRequired: false,
  operation: "READ",
  approvalPolicy: "NONE",
  resourceKind: "GITHUB_REPOSITORY",
  inputFields: [],
};
const connection: IntegrationConnection = {
  ref: "connection",
  version: 3,
  name: "GitHub",
  definitionKey: "github",
  state: "CONNECTED",
  credentialsConfigured: true,
  credentialsHint: "",
  capabilities: [capability],
  grants: [],
  nextActions: ["MANAGE_GRANTS"],
  definitionVersion: "2.3",
  definitionDigest: "a".repeat(64),
  publicConfiguration: {},
};
const run: Run = {
  ref: "run",
  projectRef: "project",
  version: 1,
  sessionRef: "session",
  rootRunRef: "run",
  target: { type: "AGENT", ref: "agent", displayName: "Агент", version: 1 },
  title: "Сохранённая сессия",
  titleSource: "USER_EDITED",
  activitySummary: "",
  state: "SUCCEEDED",
  source: "CONTROL_CENTER",
  initiator: { ref: "actor", displayName: "Пользователь" },
  attempt: 1,
  graphRevision: 1,
  lastEventSequence: 1,
  usage: {
    totalTokens: 0,
    inputTokens: 0,
    cachedInputTokens: 0,
    cacheWriteInputTokens: 0,
    outputTokens: 0,
    reasoningOutputTokens: 0,
    modelContextWindow: 0,
  },
  artifactRefs: [],
  gateRefs: [],
  createdAt: "2026-09-06T00:00:00Z",
  nextActions: ["ADD_TURN"],
};
function node(
  kind: VfsNode["kind"],
  entityRef: string,
  resourceKind: VfsNode["resourceKind"],
  name: string,
  selectable = true,
): VfsNode {
  return {
    ref: `vfs:${entityRef}`,
    entityRef,
    projectRef: "project",
    runRef: "",
    path: `/projects/project/${entityRef}`,
    parentPath: "/projects/project",
    name,
    kind,
    directory: kind === "SKILL",
    sizeBytes: 12,
    digest: "a".repeat(64),
    version: 7,
    revisionRef: "revision",
    revision: 2,
    lifecycleState: "ACTIVE",
    scanState: "CLEAN",
    resourceKind,
    selectable,
    selectionReason: selectable ? "AVAILABLE" : "IMMUTABLE_CONTEXT",
    nextActions: selectable
      ? [resourceKind === "ARTIFACT" ? "DELETE" : "ARCHIVE"]
      : ["DOWNLOAD"],
  };
}
async function choose(container: Locator, name: string) {
  await container.locator(".async-picker__trigger").click();
  await container
    .page()
    .getByRole("option", { name: new RegExp(name) })
    .click();
}
for (const width of [390, 2900]) {
  test(`synthetic: controlled clear и VFS lifecycle ${String(width)}px`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: width === 390 ? 844 : 1600 });
    const errors: string[] = [];
    page.on("pageerror", (error) => errors.push(error.message));
    await page.context().addCookies([
      {
        name: "__Host-kodex-csrf",
        value: "a".repeat(43),
        domain: "kodex.test",
        path: "/",
        secure: true,
        sameSite: "Strict",
      },
    ]);
    const nodes = [
      node("INPUT", "file", "ARTIFACT", "Документ"),
      node("SKILL", "skill", "SKILL_BUNDLE", "Навык"),
      node("MEMORY", "memory", "MEMORY_RECORD", "Память"),
      node("INPUT", "immutable", "ARTIFACT", "Вход запуска", false),
    ];
    const mutations: string[] = [];
    const filters: URLSearchParams[] = [];
    const pins = {
      contextDigest: "a".repeat(64),
      connectionVersion: 3,
      definitionVersion: "2.3",
      definitionDigest: "a".repeat(64),
      projectVersion: 1,
      recipientVersion: 1,
    };
    await page.route("**/*", async (route) => {
      const url = new URL(route.request().url());
      if (url.origin !== "https://kodex.test") {
        errors.push("Unexpected origin");
        await route.abort();
        return;
      }
      if (url.pathname === "/config/runtime-config.json") {
        await route.fulfill({
          json: {
            revision: "0".repeat(64),
            environment: "synthetic",
            apiBaseUrl: "/",
            realtimeUrl: "/api/v1",
            requestTimeoutMs: 10000,
            oidc: {
              authority: "https://identity.invalid",
              clientId: "synthetic",
              redirectUri: "/auth/callback",
              postLogoutRedirectUri: "/",
              scope: "openid",
            },
          },
        });
        return;
      }
      if (url.pathname === "/e2e/fixtures/resource-selection.json") {
        await route.fulfill({ json: { project, connection, run } });
        return;
      }
      if (url.pathname === "/api/v1/projects") {
        await route.fulfill({
          json: { items: [project], total: 1, nextPageToken: "" },
        });
        return;
      }
      if (url.pathname.startsWith("/api/v1/integration-grant-candidates/")) {
        const context = Object.fromEntries(
          ["connectionRef", "projectRef", "recipientKind", "recipientRef"]
            .filter((key) => url.searchParams.has(key))
            .map((key) => [key, url.searchParams.get(key)]),
        );
        let item;
        if (url.pathname.endsWith("/connections"))
          item = {
            connectionRef: "connection",
            name: "GitHub",
            definitionKey: "github",
            providerName: "GitHub",
            credentialKind: "TOKEN",
            resourceScope: {},
            grantable: true,
            usable: false,
            reason: "READY",
            pins,
          };
        else if (url.pathname.endsWith("/projects"))
          item = {
            projectRef: "project",
            name: "Проект",
            grantable: true,
            reason: "READY",
            pins,
          };
        else if (url.pathname.endsWith("/recipients"))
          item = {
            recipientRef: "agent",
            recipientKind: "AGENT",
            projectRef: "project",
            name: "Агент",
            grantable: true,
            reason: "READY",
            pins,
          };
        else item = { capability, grantable: true, reason: "READY", pins };
        await route.fulfill({
          json: {
            items: [item],
            total: 1,
            context,
            contextDigest: pins.contextDigest,
            pins,
          },
        });
        return;
      }
      if (
        url.pathname === "/api/v1/vfs/nodes" ||
        url.pathname === "/api/v1/vfs/search"
      ) {
        filters.push(url.searchParams);
        expect(url.searchParams.get("path")).toBe("/projects/project");
        const trash = url.searchParams.get("lifecycleState") === "DELETED";
        const kinds = url.searchParams.getAll("kinds");
        const items = nodes.filter(
          (item) =>
            (trash
              ? item.lifecycleState !== "ACTIVE"
              : item.lifecycleState === "ACTIVE") &&
            (!kinds.length || kinds.includes(item.kind)),
        );
        await route.fulfill({
          json: { items, total: items.length, nextPageToken: "" },
        });
        return;
      }
      if (url.pathname === "/api/v1/artifacts/file/impact") {
        await route.fulfill({
          json: {
            artifactRef: "file",
            artifactVersion: 7,
            action: "DELETE",
            impactDigest: "b".repeat(64),
            bindingCount: 0,
            attachmentCount: 0,
            activeRuntimeCount: 0,
            activeRuns: [],
            activeRunsTruncated: false,
            blockers: [],
            permitted: true,
          },
        });
        return;
      }
      if (["DELETE", "POST"].includes(route.request().method())) {
        expect(route.request().headers()["if-match"]).toBe('"7"');
        expect(route.request().headers()["idempotency-key"]).toBeTruthy();
        expect(route.request().headers()["x-csrf-token"]).toBe("a".repeat(43));
        mutations.push(url.pathname);
        const target = nodes.find((item) =>
          url.pathname.includes(`/${item.entityRef}`),
        );
        if (!target) {
          errors.push(`Unexpected mutation: ${url.pathname}`);
          await route.abort();
          return;
        }
        if (target.entityRef === "skill") {
          await route.fulfill({
            status: 409,
            json: {
              status: 409,
              code: "VERSION_OR_STATE_CONFLICT",
              retryable: false,
            },
          });
          return;
        }
        target.version = 8;
        target.lifecycleState =
          target.resourceKind === "ARTIFACT" ? "DELETED" : "ARCHIVED";
        target.nextActions = ["RESTORE", "PURGE"];
        await route.fulfill({
          json: {
            ref: target.entityRef,
            projectRef: "project",
            version: 8,
            lifecycleState: target.lifecycleState,
            state: "ARCHIVED",
          },
        });
        return;
      }
      if (url.pathname.startsWith("/api/")) {
        errors.push(`Unexpected API: ${url.pathname}`);
        await route.fulfill({ status: 404, json: { code: "NOT_FOUND" } });
        return;
      }
      await route.fulfill({
        response: await route.fetch({
          url: `http://127.0.0.1:43122${url.pathname}${url.search}`,
        }),
      });
    });
    await page.goto("/e2e/fixtures/resource-selection.html");
    await page
      .locator("#project-picker")
      .getByRole("button", { name: "Очистить выбор" })
      .click();
    await expect(page.locator("#project-selection")).toHaveText("");
    await expect(
      page.locator("#project-picker .async-picker__trigger"),
    ).toBeFocused();
    await page.getByRole("button", { name: "Открыть сессии" }).click();
    await page
      .getByRole("dialog")
      .getByRole("button", { name: "Очистить выбор" })
      .click();
    await expect(page.locator("#session-selection")).toHaveText("");
    await page.getByRole("option", { name: /Сохранённая сессия/ }).click();
    await expect(page.locator("#session-selection")).toHaveText("session");
    await page.getByRole("option", { name: /Сохранённая сессия/ }).click();
    await expect(page.locator("#session-selection")).toHaveText("");
    await page
      .getByRole("dialog")
      .getByRole("button", { name: "Закрыть", exact: true })
      .click();
    const fields = page.locator(".grant-form > .field");
    const fillGrant = async () => {
      await choose(fields.nth(0), "Проект");
      await choose(fields.nth(2), "Агент");
      await choose(fields.nth(3), "Чтение репозитория");
      await expect(
        page.locator(".grant-form button[type=submit]"),
      ).toBeEnabled();
    };
    await fillGrant();
    for (const index of [3, 2, 0]) {
      await fields
        .nth(index)
        .getByRole("button", { name: "Очистить выбор" })
        .click();
      await expect(
        page.locator(".grant-form button[type=submit]"),
      ).toBeDisabled();
      await expect(page.locator("#grant-selection")).toContainText(
        '"capabilityKey":""',
      );
      await fillGrant();
    }
    await page
      .locator(".connection-picker")
      .getByRole("button", { name: "Очистить выбор" })
      .click();
    await expect(page.locator("#grant-selection")).toContainText(
      '"projectRef":""',
    );
    await expect(
      page.locator(".grant-form button[type=submit]"),
    ).toBeDisabled();
    await expect(
      page.getByRole("checkbox", { name: "Выбрать: Вход запуска" }),
    ).toBeDisabled();
    await page.locator(".vfs-row").filter({ hasText: "Документ" }).click();
    await expect(page.locator("#assistant-context")).toContainText(
      '"entityKind":"FILE"',
    );
    await expect(page.locator("#assistant-context")).toContainText(
      '"entityRef":"file"',
    );
    await expect(page.locator("#assistant-context")).toContainText(
      '"allowedOperations":[]',
    );
    await page.locator(".vfs-row").filter({ hasText: "Документ" }).click();
    await expect(page.locator("#assistant-context")).toContainText(
      '"entityKind":""',
    );
    for (const name of ["Документ", "Навык", "Память"])
      await page.getByRole("checkbox", { name: `Выбрать: ${name}` }).check();
    await page
      .getByRole("button", { name: "В корзину / архив", exact: true })
      .click();
    expect(mutations).toEqual([]);
    await page
      .getByRole("button", { name: "Подтвердить для 3 объектов" })
      .click();
    await expect(
      page.getByRole("heading", { name: "Результаты по объектам" }),
    ).toBeVisible();
    expect(mutations).toHaveLength(3);
    await page.locator(".vfs-filter select").selectOption("DELETED");
    await expect(
      page.getByRole("checkbox", { name: "Выбрать: Документ" }),
    ).toBeVisible();
    await expect(
      page.getByRole("checkbox", { name: "Выбрать: Память" }),
    ).toBeVisible();
    await expect(
      page.getByRole("checkbox", { name: "Выбрать: Навык" }),
    ).toHaveCount(0);
    await page.locator(".vfs-kind-filter summary").click();
    await page
      .locator(".vfs-kind-filter")
      .getByRole("checkbox", { name: "Память", exact: true })
      .check();
    await expect
      .poll(() => filters.at(-1)?.getAll("kinds"))
      .toEqual(["MEMORY"]);
    expect(filters.at(-1)?.get("lifecycleState")).toBe("DELETED");
    await expect(
      page.getByRole("checkbox", { name: "Выбрать: Память" }),
    ).toBeVisible();
    await expect(
      page.getByRole("checkbox", { name: "Выбрать: Документ" }),
    ).toHaveCount(0);
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth,
      ),
    ).toBe(true);
    expect(errors).toEqual([]);
  });
}
