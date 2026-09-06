import { expect } from "@playwright/test";
import { test } from "./fixtures/browser-diagnostics";
import { installEnvironmentFixture } from "./fixtures/environment";
import { installProviderFixture } from "./fixtures/providers";
import { checkSecretEditor } from "./fixtures/secrets";
import { checkInteractionIdentities } from "./fixtures/interaction-identities";
import { checkContextResources } from "./fixtures/context-resources";
import { checkIntegrationPackage } from "./fixtures/integration-package";
import { checkEmailEffects } from "./fixtures/email-effects";
import { checkGrantCandidates } from "./fixtures/grant-candidates";
import { checkRunsCatalog } from "./fixtures/runs-catalog";
import { checkOrganizationCatalog } from "./fixtures/organization-catalog";
import { checkFileSelection } from "./fixtures/file-selection";
import { checkAssistantHistory } from "./fixtures/assistant-history";
import { checkHomeResults } from "./fixtures/home-results";
import { checkResumableSessions } from "./fixtures/resumable-sessions";
import { syntheticBrowserSession } from "./fixtures/browser-session";
import {
  checkRoleImageCatalog,
  checkRoleImageHistory,
} from "./fixtures/role-images";
import { checkWorkflowEditor } from "./fixtures/workflow";
import { checkEffectiveCapabilities } from "./fixtures/effective-capabilities";
import type {
  ManagedConfiguration,
  ManagedConfigurationRevision,
  ManagedConfigurationSummary,
  VfsNode,
  BootstrapState,
  Overview,
  Project,
  SystemAssistant,
  IntegrationDefinition,
  RuntimeEnvironmentSet,
  RevisionImpactPlan,
  RevisionImpactPage,
} from "../src/shared/api/generated/openapi/types.gen";

const assistant: SystemAssistant = {
  ref: "assistant_synthetic",
  version: 1,
  name: "Kodex",
  system: true,
  removable: false,
  corePromptRevision: "1",
  ownerInstructions: "",
  runtimeState: "READY",
  readinessSummary: "",
  nextActions: [],
};
const bootstrap: BootstrapState = {
  speechTranscription: { available: false, reason: "STT_NOT_CONFIGURED" },
  initialized: true,
  onboardingComplete: true,
  webOnlyReady: true,
  assistant,
  currentUser: { ref: "user_synthetic", displayName: "Тестовый владелец" },
  platformRole: "OWNER",
  nextActions: [],
};
const projects: Project[] = Array.from({ length: 8 }, (_, index) => ({
  ref: `project_synthetic_${String(index)}`,
  version: 1,
  name:
    index === 0
      ? "Проект с длинным названием для проверки адаптивной вёрстки"
      : `Проект ${String(index + 1)}`,
  purpose: "Документы и согласование",
  language: "ru",
  lifecycle: "ACTIVE",
  agentCount: 3,
  integrationState: "NONE",
  workflowCount: 2,
  activeRunCount: 0,
  pendingGateCount: 0,
  updatedAt: index === 0 ? "2026-09-04T11:00:00Z" : "2026-09-04T10:00:00Z",
  nextActions: [
    "CREATE_RUN",
    "CREATE_AGENT",
    "CREATE_WORKFLOW",
    "UPLOAD_ARTIFACT",
  ],
}));
const overview: Overview = {
  projectCount: projects.length,
  agentCount: 24,
  activeRunCount: 0,
  pendingGateCount: 0,
  activeRuns: [],
  pendingGates: [],
  recentArtifacts: [],
};
const integration: IntegrationDefinition = {
  key: "github",
  name: "GitHub",
  description: "Репозитории и задачи",
  category: "source-control",
  builtIn: true,
  available: true,
  schemaVersion: "integrations.kodex.io/v1",
  definitionVersion: "1.1.0",
  origin: "SHIPPED",
  digest: "a".repeat(64),
  adapter: "GITHUB",
  adapterOwner: "integration-gateway",
  executionRoute: "MANAGED_MCP",
  adapterReadiness: "READY",
  configurationFields: [
    {
      key: "owner",
      label: "Организация",
      help: "",
      valueType: "TEXT",
      required: true,
      maximumLength: 100,
    },
    {
      key: "repository",
      label: "Репозиторий",
      help: "",
      valueType: "TEXT",
      required: true,
      maximumLength: 100,
    },
  ],
  capabilities: [
    {
      key: "github.repository.read",
      name: "Чтение репозитория",
      description: "Чтение разрешённого репозитория",
      risk: "READ",
      approvalRequired: false,
      operation: "github.repository.read",
      approvalPolicy: "NONE",
      resourceKind: "GITHUB_REPOSITORY",
      inputFields: [],
    },
  ],
};

for (const { width, height } of [
  { width: 2900, height: 1600 },
  { width: 2560, height: 1440 },
  { width: 1920, height: 1080 },
  { width: 1440, height: 900 },
  { width: 1280, height: 900 },
  { width: 900, height: 1080 },
  { width: 768, height: 1024 },
  { width: 390, height: 844 },
]) {
  test(`synthetic: Home и ассистент ${String(width)}px`, async ({
    context,
    page,
  }, testInfo) => {
    // Общий сценарий последовательно проверяет более двадцати экранов; отдельные ожидания сохраняют прежние лимиты.
    test.setTimeout(75_000);
    const failures: string[] = [];
    let snapshotConflictDiagnostics = 0;
    let publicationTimeoutDiagnostics = 0;
    let inspectorFailureDiagnostics = 0;
    let expectedInspectorFailure: 404 | 503 | undefined;
    let sessionVersion = 1;
    let sessionRenewals = 0;
    let sessionResumes = 0;
    if (width === 1440) await page.clock.install();
    await page.setViewportSize({ width, height });
    page.on("pageerror", (error) => failures.push(error.message));
    page.on("console", (message) => {
      // Service Worker намеренно исключён из synthetic-контура; остальные предупреждения являются ошибками проверки.
      if (
        message.text() === "Service Worker registration blocked by Playwright"
      )
        return;
      // Ожидаемый HTTP отказ проверяет recovery точного synthetic Session cursor.
      if (
        message.type() === "error" &&
        message.text() ===
          "Failed to load resource: the server responded with a status of 412 (Precondition Failed)" &&
        message.location().url.includes("/api/v1/runs?") &&
        message.location().url.includes("resumableSessionsOnly=true") &&
        message.location().url.includes("pageToken=session-snapshot")
      ) {
        snapshotConflictDiagnostics++;
        return;
      }
      if (
        message.type() === "error" &&
        message.text() ===
          "Failed to load resource: the server responded with a status of 504 (Gateway Timeout)" &&
        /^https:\/\/kodex\.test\/api\/v1\/(prompt-template-configurations\/configuration_synthetic\/revisions\/[^/]+|runtime-environment-drafts\/draft_prepared_environment)\/publication$/.test(
          message.location().url,
        )
      ) {
        publicationTimeoutDiagnostics++;
        return;
      }
      if (
        expectedInspectorFailure &&
        message.type() === "error" &&
        message.text() ===
          `Failed to load resource: the server responded with a status of ${String(expectedInspectorFailure)} (${expectedInspectorFailure === 404 ? "Not Found" : "Service Unavailable"})` &&
        /^https:\/\/kodex\.test\/api\/v1\/runtime-environments\/environment_synthetic\/(readiness|agents)(?:\?|$)/.test(
          message.location().url,
        )
      ) {
        inspectorFailureDiagnostics++;
        return;
      }
      if (message.type() === "error" || message.type() === "warning")
        failures.push(message.text());
    });
    page.on("requestfailed", (request) => {
      if (request.failure()?.errorText !== "net::ERR_ABORTED")
        failures.push(`Failed request: ${new URL(request.url()).pathname}`);
    });
    await context.addCookies([
      {
        name: "__Host-kodex-csrf",
        value: "s".repeat(43),
        domain: "kodex.test",
        path: "/",
        secure: true,
        sameSite: "Strict",
      },
    ]);
    await context.route("**/*", async (route) => {
      const url = new URL(route.request().url());
      if (url.origin !== "https://kodex.test") {
        failures.push(`Unexpected origin: ${url.origin}`);
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
            requestTimeoutMs: 10_000,
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
      if (
        url.pathname === "/api/v1/session" &&
        ["PUT", "GET"].includes(route.request().method())
      ) {
        if (route.request().method() === "PUT") {
          expect(route.request().headers()["x-csrf-token"]).toBeTruthy();
          expect(route.request().headers()["authorization"]).toBeUndefined();
          sessionVersion++;
          sessionRenewals++;
        }
        await route.fulfill({
          json: syntheticBrowserSession({
            version: sessionVersion,
            ...(width === 390 && sessionRenewals === 0
              ? { renewAfter: new Date(Date.now() + 1500).toISOString() }
              : {}),
          }),
        });
        return;
      }
      const responses: Record<string, unknown> = {
        ...Object.fromEntries(
          projects.map((project) => [
            `/api/v1/projects/${project.ref}`,
            project,
          ]),
        ),
        "/api/v1/bootstrap": bootstrap,
        "/api/v1/overview": overview,
        "/api/v1/projects": {
          items: projects,
          nextActions: ["CREATE_PROJECT"],
        },
        "/api/v1/runs": { items: [], total: 0, nextActions: [] },
        "/api/v1/artifacts": { items: [], total: 0 },
        "/api/v1/owner-gates": { items: [], total: 0, nextPageToken: "" },
        "/api/v1/system-assistant": assistant,
        "/api/v1/assistant-conversations": { items: [] },
      };
      if (url.pathname in responses && route.request().method() === "GET") {
        await route.fulfill({
          json: responses[url.pathname],
          headers: { ETag: '"1"' },
        });
        return;
      }
      if (url.pathname.startsWith("/api/")) {
        failures.push(
          `Unhandled API: ${route.request().method()} ${url.pathname}`,
        );
        await route.fulfill({
          status: 501,
          json: { code: "UNHANDLED_SYNTHETIC_API" },
        });
        return;
      }
      const response = await route.fetch({
        url: `http://127.0.0.1:43122${url.pathname}${url.search}`,
        maxRetries: route.request().method() === "GET" ? 2 : 0,
        timeout: 15000,
      });
      if (!response.ok())
        failures.push(`Asset: ${url.pathname} ${String(response.status())}`);
      await route.fulfill({ response });
    });
    let invalidateAgents: (() => void) | undefined;
    let invalidateRuns: (() => void) | undefined;
    await page.routeWebSocket("**/*", (socket) => {
      socket.onMessage((message) => {
        const input = JSON.parse(String(message)) as {
          type: string;
          requestRef: string;
        };
        if (input.type === "SESSION_RESUME") {
          sessionResumes++;
          let invalidationCursor = 0;
          invalidateAgents = () =>
            socket.send(
              JSON.stringify({
                type: "PLATFORM_INVALIDATED",
                requestRef: input.requestRef,
                streamKind: "PLATFORM",
                streamRef: "PLATFORM",
                cursor: ++invalidationCursor,
                eventName: "AGENT_CHANGED",
                kind: "AGENT",
              }),
            );
          invalidateRuns = () =>
            socket.send(
              JSON.stringify({
                type: "PLATFORM_INVALIDATED",
                requestRef: input.requestRef,
                streamKind: "PLATFORM",
                streamRef: "PLATFORM",
                cursor: ++invalidationCursor,
                eventName: "RUN_CHANGED",
                kind: "RUN",
              }),
            );
          socket.send(
            JSON.stringify({
              type: "SESSION_READY",
              requestRef: input.requestRef,
              streams: [
                { streamKind: "PLATFORM", streamRef: "PLATFORM", cursor: 0 },
              ],
            }),
          );
          socket.send(
            JSON.stringify({
              type: "PLATFORM_READY",
              requestRef: input.requestRef,
              streamKind: "PLATFORM",
              streamRef: "PLATFORM",
              cursor: 0,
            }),
          );
          socket.send(
            JSON.stringify({
              type: "STREAM_HEARTBEAT",
              streamKind: "PLATFORM",
              streamRef: "PLATFORM",
              cursor: 0,
              serverTime: new Date().toISOString(),
            }),
          );
        }
      });
    });
    const projectQueries: string[] = [];
    await page.route("**/api/v1/projects?**", async (route) => {
      const url = new URL(route.request().url());
      const query = url.searchParams.get("query") ?? "";
      projectQueries.push(query);
      const matches = projects.filter((project) =>
        project.name.includes(query),
      );
      const more = url.searchParams.get("pageToken") === "projects_next";
      await route.fulfill({
        json: {
          items: more ? matches.slice(6) : matches.slice(0, 6),
          nextPageToken:
            !more && matches.length > 6 ? "projects_next" : undefined,
          nextActions: ["CREATE_PROJECT"],
        },
      });
    });
    await page.goto("/");
    await expect(page.locator("main h1")).toBeVisible();
    await expect(page.locator("header .realtime-status")).toHaveAttribute(
      "data-presentation-state",
      "live",
    );
    if (width === 390) {
      await expect.poll(() => sessionRenewals).toBe(1);
      await expect.poll(() => sessionResumes).toBe(2);
      await expect(page.locator("header .realtime-status")).toHaveAttribute(
        "data-presentation-state",
        "live",
      );
    }
    await expect(
      page.getByText(projects[0]?.name ?? "", { exact: true }).first(),
    ).toBeVisible();
    await expect
      .poll(() =>
        page.evaluate(
          () => document.documentElement.scrollWidth <= window.innerWidth,
        ),
      )
      .toBe(true);
    await page.screenshot({
      path: testInfo.outputPath(`home-${String(width)}.png`),
      fullPage: true,
    });
    await page
      .locator(".home-project-section")
      .getByRole("button", { name: "Развернуть список проекта", exact: true })
      .click();
    const homeProjects = page.getByRole("dialog", {
      name: "Проекты",
      exact: true,
    });
    await expect(homeProjects.locator(".home-project")).toHaveCount(6);
    await homeProjects.getByRole("searchbox").fill("Проект 2");
    await expect(homeProjects.locator(".home-project")).toHaveCount(1);
    await homeProjects.getByRole("searchbox").fill("");
    await expect(homeProjects.locator(".home-project")).toHaveCount(6);
    await homeProjects
      .getByRole("button", { name: "Закрыть", exact: true })
      .click();
    await page
      .getByRole("button", { name: "Открыть Kodex", exact: true })
      .click();
    await expect(
      page.getByRole("dialog", { name: "Kodex", exact: true }),
    ).toBeVisible();
    await expect
      .poll(() =>
        page.evaluate(
          () => document.documentElement.scrollWidth <= window.innerWidth,
        ),
      )
      .toBe(true);
    await page.screenshot({
      path: testInfo.outputPath(`assistant-${String(width)}.png`),
      fullPage: true,
    });
    await page
      .getByRole("dialog", { name: "Kodex", exact: true })
      .getByRole("button", { name: "Закрыть", exact: true })
      .click();
    let configuration: ManagedConfiguration = {
      ref: "configuration_synthetic",
      version: 1,
      kind: "PROMPT_TEMPLATE",
      name: "Шаблон",
      managedBy: "UI",
      source: "ui",
      sourceRevision: "1",
      updatedAt: "2026-09-04T11:00:00Z",
    };
    let revision: ManagedConfigurationRevision = {
      ref: "revision_synthetic",
      revision: 1,
      state: "DRAFT",
      contentFormat: "TEXT",
      content: "",
      digest: "a".repeat(64),
      validationDiagnostics: [],
      createdAt: configuration.updatedAt,
    };
    const commands: string[] = [];
    let promptPlan: RevisionImpactPlan | undefined;
    let firstPromptPublicationKey: string | undefined;
    const draftHistory: ManagedConfigurationRevision[] = [];
    await page.route("**/api/v1/**", async (route) => {
      const path = new URL(route.request().url()).pathname;
      const method = route.request().method();
      if (path === "/api/v1/revision-impact-plans/prompt_plan_synthetic") {
        if (!promptPlan) throw new Error("Prompt plan was not prepared");
        const report: RevisionImpactPage = {
          plan: promptPlan,
          total: 1,
          items: [
            {
              ref: "prompt_item_synthetic",
              projectRef: projects[0]?.ref ?? "project_synthetic",
              consumerKind: "AGENT",
              consumerRef: "agent_prompt_consumer",
              consumerVersion: 2,
              bindingRef: "prompt_binding",
              bindingVersion: 3,
              sourceRevisionRef: "previous_prompt",
              outcome: promptPlan.state === "APPLIED" ? "CONFLICT" : "PENDING",
            },
          ],
        };
        await route.fulfill({ json: report });
        return;
      }
      if (
        path.endsWith("/impact-plans") &&
        path.startsWith("/api/v1/prompt-template-configurations/")
      ) {
        expect(method).toBe("POST");
        expect(route.request().headers()["if-match"]).toBe(
          `"${String(configuration.version)}"`,
        );
        expect(revision.state).toBe("VALID");
        promptPlan = {
          ref: "prompt_plan_synthetic",
          version: 1,
          kind: "PROMPT_TEMPLATE",
          sourceRef: configuration.ref,
          sourceVersion: configuration.version,
          draftRef: revision.ref,
          draftVersion: revision.revision,
          targetDigest: revision.digest,
          digest: "plan-digest",
          total: 2,
          state: "PREPARED",
          createdAt: "2026-09-06T00:00:00Z",
          expiresAt: "2099-09-06T00:00:00Z",
        };
        commands.push("prepare");
        await route.fulfill({ json: promptPlan });
        return;
      }
      if (
        path === "/api/v1/prompt-template-configurations/drafts" &&
        method === "POST"
      ) {
        const body = route.request().postDataJSON() as {
          name: string;
          content: string;
          configurationRef?: string;
        };
        expect(body.configurationRef).toBeUndefined();
        expect(body.content).toBe("Проверить документы");
        configuration = { ...configuration, name: body.name };
        revision = { ...revision, content: body.content };
        commands.push("create");
        await route.fulfill({ status: 201, json: { configuration, revision } });
        return;
      }
      if (
        path === `/api/v1/managed-configurations/${configuration.ref}/revisions`
      ) {
        await route.fulfill({
          json: {
            configuration,
            items: [revision, ...draftHistory],
            total: 1 + draftHistory.length,
          },
        });
        return;
      }
      const base = `/api/v1/prompt-template-configurations/${configuration.ref}/revisions/${revision.ref}`;
      if (path === `${base}/saves`) {
        expect(route.request().headers()["if-match"]).toBe(
          `"${String(configuration.version)}"`,
        );
        const body = route.request().postDataJSON() as {
          content: string;
          contentFormat: string;
        };
        expect(body.contentFormat).toBe("TEXT");
        draftHistory.unshift({ ...revision, state: "DISCARDED" });
        revision = {
          ...revision,
          ref: `${revision.ref}_next`,
          parentRevisionRef: revision.ref,
          revision: revision.revision + 1,
          content: body.content,
          state: "DRAFT",
        };
        configuration = {
          ...configuration,
          version: configuration.version + 1,
        };
        commands.push("save");
        await route.fulfill({
          headers: { ETag: `"${String(configuration.version)}"` },
          json: { configuration, revision },
        });
        return;
      }
      if (path === `${base}/validation` || path === `${base}/publication`) {
        expect(route.request().headers()["if-match"]).toBe(
          `"${String(configuration.version)}"`,
        );
        expect(route.request().headers()["idempotency-key"]).toMatch(
          /^[0-9a-f-]{36}$/,
        );
        const publishing = path.endsWith("publication");
        if (publishing) {
          if (!promptPlan)
            throw new Error("Prompt publication requires prepared plan");
          expect(route.request().postDataJSON()).toEqual({
            planRef: promptPlan.ref,
            selectedItemRefs: ["prompt_item_synthetic"],
          });
          const key = route.request().headers()["idempotency-key"];
          if (!firstPromptPublicationKey) {
            firstPromptPublicationKey = key;
            commands.push("publish-unknown");
            await route.fulfill({
              status: 504,
              json: {
                type: "about:blank",
                title: "Gateway Timeout",
                status: 504,
              },
            });
            return;
          }
          expect(key).toBe(firstPromptPublicationKey);
          promptPlan = {
            ...promptPlan,
            state: "APPLIED",
            version: 2,
            publishedRevisionRef: revision.ref,
          };
        }
        expect(revision.state).toBe(publishing ? "VALID" : "DRAFT");
        revision = { ...revision, state: publishing ? "PUBLISHED" : "VALID" };
        configuration = {
          ...configuration,
          version: configuration.version + 1,
          ...(publishing ? { currentRevision: revision } : {}),
        };
        commands.push(publishing ? "publish" : "validate");
        await route.fulfill({
          json: {
            configuration,
            revision,
            ...(publishing ? { plan: promptPlan } : {}),
          },
        });
        return;
      }
      await route.fallback();
    });
    await page.goto("/configurations/PROMPT_TEMPLATE/new");
    await page.getByLabel("Название", { exact: true }).fill("Шаблон");
    await page
      .getByRole("textbox", { name: "Содержимое", exact: true })
      .fill("Проверить документы");
    await page
      .getByRole("button", { name: "Сохранить черновик", exact: true })
      .click();
    await expect(page).toHaveURL(/configuration_synthetic$/);
    await page
      .getByRole("textbox", { name: "Содержимое", exact: true })
      .fill("");
    await page
      .getByRole("button", { name: "Сохранить черновик", exact: true })
      .click();
    await expect.poll(() => revision.content).toBe("");
    await expect(
      page.getByRole("button", { name: "Сохранить черновик", exact: true }),
    ).toBeDisabled();
    await page
      .getByRole("textbox", { name: "Содержимое", exact: true })
      .fill("Проверить документы");
    await page
      .getByRole("button", { name: "Сохранить черновик", exact: true })
      .click();
    await expect.poll(() => revision.revision).toBe(3);
    await expect(
      page.getByRole("button", { name: "Опубликовать", exact: true }),
    ).toBeDisabled();
    await page.getByRole("button", { name: "Проверить", exact: true }).click();
    await expect(
      page.getByRole("button", { name: "Опубликовать", exact: true }),
    ).toBeEnabled();
    await page
      .getByRole("button", { name: "Опубликовать", exact: true })
      .click();
    const promptPlanDialog = page.getByRole("dialog", {
      name: "План публикации",
      exact: true,
    });
    await expect(
      promptPlanDialog.getByText("В исходном плане: 2", { exact: true }),
    ).toBeVisible();
    await promptPlanDialog
      .getByRole("checkbox", { name: "agent_prompt_consumer", exact: true })
      .check();
    await promptPlanDialog
      .getByRole("button", {
        name: "Опубликовать и обновить выбранных: 1",
        exact: true,
      })
      .click();
    await promptPlanDialog
      .getByRole("button", { name: "Повторить исходный запрос", exact: true })
      .click();
    await expect(
      promptPlanDialog.locator('[data-state="CONFLICT"]'),
    ).toBeVisible();
    await promptPlanDialog
      .getByRole("button", { name: "Закрыть", exact: true })
      .click();
    await expect(
      page.locator(".configuration-editor [data-state='PUBLISHED']"),
    ).toBeVisible();
    await expect
      .poll(() =>
        page.evaluate(
          () => document.documentElement.scrollWidth <= window.innerWidth,
        ),
      )
      .toBe(true);
    await page.screenshot({
      path: testInfo.outputPath(`configuration-${String(width)}.png`),
      fullPage: true,
    });
    expect(commands).toEqual([
      "create",
      "save",
      "save",
      "validate",
      "prepare",
      "publish-unknown",
      "publish",
    ]);
    const summaries: ManagedConfigurationSummary[] = Array.from(
      { length: 8 },
      (_, index) => ({
        ref: `configuration_${String(index)}`,
        version: 1,
        kind: "PROMPT_TEMPLATE",
        name: `Шаблон ${String(index + 1)}`,
        managedBy: index === 0 ? "GIT" : "UI",
        source: index === 0 ? "repository/templates/summary.txt" : "ui",
        sourceRevision: "revision-1",
        updatedAt: "2026-09-04T11:00:00Z",
        currentRevision: {
          ref: `revision_${String(index)}`,
          revision: 1,
          state: "PUBLISHED",
          digest: "b".repeat(64),
        },
      }),
    );
    const rootNodes: VfsNode[] = projects.map((project) => ({
      ref: `node_${project.ref}`,
      path: `/projects/${project.ref}`,
      parentPath: "/projects",
      name: project.name,
      kind: "PROJECT",
      directory: true,
      projectRef: project.ref,
      entityRef: project.ref,
      runRef: "",
      sizeBytes: 0,
      digest: "",
      modifiedAt: project.updatedAt,
      version: 0,
      revisionRef: "",
      revision: 0,
      lifecycleState: "ACTIVE",
      scanState: "",
      resourceKind: "",
      selectable: false,
      selectionReason: "DIRECTORY",
      nextActions: [],
    }));
    const vfsRequests: string[] = [];
    await page.route("**/api/v1/**", async (route) => {
      const url = new URL(route.request().url());
      if (url.pathname === "/api/v1/managed-configurations") {
        expect(url.searchParams.get("kind")).toBe("PROMPT_TEMPLATE");
        const query = url.searchParams.get("query") ?? "";
        const items = summaries.filter((item) => item.name.includes(query));
        await route.fulfill({ json: { items, total: items.length } });
        return;
      }
      if (url.pathname === "/api/v1/vfs/nodes") {
        const path = url.searchParams.get("path");
        vfsRequests.push(path ?? "");
        const first = rootNodes[0];
        const items =
          path === "/projects"
            ? rootNodes
            : first && path === first.path
              ? [
                  {
                    ...first,
                    ref: "folder_runs",
                    path: `${first.path}/runs`,
                    parentPath: first.path,
                    kind: "DIRECTORY",
                    name: "Запуски",
                    entityRef: "",
                  },
                ]
              : [];
        await route.fulfill({
          json: { items, total: items.length, nextPageToken: "" },
        });
        return;
      }
      if (url.pathname === "/api/v1/vfs/search") {
        expect(url.searchParams.get("query")).toBe("Проект 2");
        await route.fulfill({
          json: { items: [rootNodes[1]], total: 1, nextPageToken: "" },
        });
        return;
      }
      await route.fallback();
    });
    await page.goto("/configurations/PROMPT_TEMPLATE");
    await expect(page.locator(".configuration-catalog__row")).toHaveCount(8);
    await page
      .getByRole("button", { name: "Развернуть список проекта", exact: true })
      .click();
    const catalogDialog = page.getByRole("dialog");
    await expect(
      catalogDialog.locator(".configuration-catalog__row"),
    ).toHaveCount(8);
    await catalogDialog.getByRole("searchbox").fill("Шаблон 8");
    await expect(
      catalogDialog.locator(".configuration-catalog__row"),
    ).toHaveCount(1);
    await catalogDialog
      .getByRole("button", { name: "Закрыть", exact: true })
      .click();
    await page.screenshot({
      path: testInfo.outputPath(`catalog-${String(width)}.png`),
      fullPage: true,
    });
    await page.goto("/files");
    await expect(page.locator(".vfs-row")).toHaveCount(8);
    await expect(page.locator(".vfs-inspector")).toHaveCount(0);
    await page.locator(".vfs-row").first().click();
    await page
      .locator(".vfs-inspector")
      .getByRole("button", { name: "Открыть", exact: true })
      .click();
    await expect(page.locator(".vfs-row")).toHaveCount(1);
    await expect(page.locator(".vfs-row")).toContainText("Запуски");
    expect(vfsRequests).toContain(rootNodes[0]?.path);
    await page.getByRole("button", { name: "Назад", exact: true }).click();
    await expect(page.locator(".vfs-row")).toHaveCount(8);
    await page
      .getByRole("searchbox", { name: "Найти файл", exact: true })
      .fill("Проект 2");
    await expect(page.locator(".vfs-row")).toHaveCount(1);
    await page.locator(".vfs-row").click();
    await expect
      .poll(() =>
        page.evaluate(
          () => document.documentElement.scrollWidth <= window.innerWidth,
        ),
      )
      .toBe(true);
    await page.screenshot({
      path: testInfo.outputPath(`vfs-${String(width)}.png`),
      fullPage: true,
    });
    await page.goto("/projects");
    await expect(page.locator(".project-list__item")).toHaveCount(6);
    await page
      .getByRole("button", { name: "Загрузить ещё", exact: true })
      .click();
    await expect(page.locator(".project-list__item")).toHaveCount(8);
    await page
      .getByRole("button", { name: "Развернуть список проекта", exact: true })
      .click();
    await expect(
      page.getByRole("dialog").locator(".project-list__item"),
    ).toHaveCount(8);
    await page.getByRole("dialog").getByRole("searchbox").fill("Проект 2");
    await expect(
      page.getByRole("dialog").locator(".project-list__item"),
    ).toHaveCount(1);
    expect(projectQueries).toContain("Проект 2");
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth,
      ),
    ).toBe(true);
    await page.screenshot({
      path: testInfo.outputPath(`projects-${String(width)}.png`),
      fullPage: true,
    });
    await page.route("**/api/v1/integration-definitions**", (route) =>
      route.fulfill({
        json: {
          items: [integration],
          coreReady: true,
          nextActions: ["CREATE_CONNECTION"],
        },
      }),
    );
    await page.route("**/api/v1/integration-connections**", (route) =>
      route.fulfill({
        json: { items: [], nextActions: [], nextPageToken: "" },
      }),
    );
    await page.goto("/integrations");
    await page.getByRole("tab", { name: /Каталог/ }).click();
    const card = page.locator(".package-card");
    await expect(card).toHaveCount(1);
    const beforeDetails = await card.boundingBox();
    await card.getByRole("button", { name: "Подробнее", exact: true }).click();
    await expect(page.getByRole("dialog")).toContainText("GITHUB_REPOSITORY");
    const afterDetails = await card.boundingBox();
    expect(afterDetails?.width).toBe(beforeDetails?.width);
    expect(afterDetails?.height).toBe(beforeDetails?.height);
    await page
      .getByRole("dialog")
      .getByRole("button", { name: "Подключить", exact: true })
      .click();
    await page
      .getByRole("dialog")
      .getByLabel("Организация", { exact: true })
      .fill("synthetic-owner");
    await page
      .getByRole("dialog")
      .getByLabel("Репозиторий", { exact: true })
      .fill("synthetic-repository");
    await page
      .getByRole("dialog")
      .getByRole("button", { name: "YAML", exact: true })
      .click();
    await expect(page.getByRole("dialog").locator(".cm-content")).toContainText(
      "owner: synthetic-owner",
    );
    await page
      .getByRole("dialog")
      .getByRole("button", { name: "Форма", exact: true })
      .click();
    await expect(
      page.getByRole("dialog").getByLabel("Репозиторий", { exact: true }),
    ).toHaveValue("synthetic-repository");
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth,
      ),
    ).toBe(true);
    await page.screenshot({
      path: testInfo.outputPath(`integration-${String(width)}.png`),
      fullPage: true,
    });
    let contextEnvironment: RuntimeEnvironmentSet | undefined;
    {
      const projectRef = projects[0]?.ref ?? "project_synthetic_0";
      const fixture = await installEnvironmentFixture(page, projectRef);
      contextEnvironment = fixture.environment;
      await page.goto(`/projects/${projectRef}/environments/new`);
      const saveDraft = page.getByRole("button", {
        name: "Сохранить черновик",
        exact: true,
      });
      await expect(saveDraft).toBeEnabled();
      await saveDraft.click();
      await expect(page).toHaveURL(/draftRef=draft_synthetic_environment/);
      await expect(page.locator(".environment-draft-state")).toContainText(
        "Новое окружение: опубликованной базы нет",
      );
      await expect(
        page.locator(".environment-draft-state time"),
      ).toHaveAttribute("datetime", "2026-09-05T00:00:00Z");
      expect(fixture.events).not.toContain("publish");
      await page
        .getByRole("button", { name: "Проверить", exact: true })
        .click();
      await expect(page.locator(".environment-draft-state")).toContainText(
        "Окружение не прошло проверку",
      );
      await expect(
        page.locator(".environment-draft-state time"),
      ).toHaveAttribute("datetime", "2026-09-05T00:00:00Z");
      await expect(
        page.getByRole("button", { name: "Опубликовать", exact: true }),
      ).toBeDisabled();
      await page
        .getByLabel("Название", { exact: true })
        .fill("Незавершённое окружение");
      await saveDraft.click();
      await page.reload();
      await expect(page.getByLabel("Название", { exact: true })).toHaveValue(
        "Незавершённое окружение",
      );
      await expect(
        page.locator(".environment-draft-state time"),
      ).toHaveAttribute("datetime", "2026-09-05T00:05:00Z");
      await page
        .getByLabel("Название", { exact: true })
        .fill("Изменения перед выходом");
      await page.getByRole("link", { name: "Отмена", exact: true }).click();
      await page
        .getByRole("dialog")
        .getByRole("button", { name: "Остаться", exact: true })
        .click();
      await expect(page.getByLabel("Название", { exact: true })).toHaveValue(
        "Изменения перед выходом",
      );
      const savedDraftUrl = page.url();
      await page.getByRole("link", { name: "Отмена", exact: true }).click();
      await page
        .getByRole("dialog")
        .getByRole("button", { name: "Сохранить и выйти", exact: true })
        .click();
      await expect(page).toHaveURL(
        new RegExp(`/projects/${projectRef}/environments$`),
      );
      await expect(page.locator(".environment-name")).toHaveCount(1);
      await expect(page.locator(".environment-inspector")).toHaveCount(0);
      expect(fixture.events).not.toContain("readiness");
      await page.locator(".environment-name").click();
      await expect(page.locator(".environment-inspector")).toBeVisible();
      await expect.poll(() => fixture.events.includes("readiness")).toBe(true);
      await page.locator(".environment-name").click();
      await expect(page.locator(".environment-inspector")).toHaveCount(0);
      await page
        .getByRole("button", { name: "Развернуть список проекта", exact: true })
        .click();
      await expect(
        page.getByRole("dialog").locator(".environment-name"),
      ).toHaveCount(1);
      await page
        .getByRole("dialog")
        .getByRole("button", { name: "Закрыть", exact: true })
        .click();
      await page.goto(savedDraftUrl);
      await expect(page.getByLabel("Название", { exact: true })).toHaveValue(
        "Изменения перед выходом",
      );
      await page.getByLabel("Название", { exact: true }).fill("Не сохранять");
      await page.getByRole("link", { name: "Отмена", exact: true }).click();
      await page
        .getByRole("dialog")
        .getByRole("button", { name: "Выйти без сохранения", exact: true })
        .click();
      await expect(page).toHaveURL(
        new RegExp(`/projects/${projectRef}/environments$`),
      );
      await page.goto(savedDraftUrl);
      await expect(page.getByLabel("Название", { exact: true })).toHaveValue(
        "Изменения перед выходом",
      );
      await page
        .getByRole("button", { name: "Удалить черновик", exact: true })
        .click();
      await page
        .getByRole("dialog")
        .getByRole("button", { name: "Удалить черновик", exact: true })
        .click();
      await expect(page).not.toHaveURL(/draftRef=/);
      expect(fixture.events).toContain("discard");
      await page.goto(
        `/projects/${projectRef}/environments/new?draftRef=${fixture.preparedRef}`,
      );
      await expect(page.getByLabel("Название", { exact: true })).toHaveValue(
        "Окружение synthetic",
      );
      await page
        .getByRole("button", { name: "Проверить", exact: true })
        .click();
      await expect(
        page.getByRole("button", { name: "Опубликовать", exact: true }),
      ).toBeEnabled();
      await page
        .getByRole("button", { name: "Опубликовать", exact: true })
        .click();
      const environmentPlan = page.getByRole("dialog", {
        name: "План публикации",
        exact: true,
      });
      await environmentPlan
        .getByRole("button", {
          name: "Опубликовать и обновить выбранных: 0",
          exact: true,
        })
        .click();
      await environmentPlan
        .getByRole("button", { name: "Обновить", exact: true })
        .click();
      await expect(
        environmentPlan.locator('[data-state="APPLIED"]'),
      ).toBeVisible();
      await environmentPlan
        .getByRole("button", { name: "Закрыть", exact: true })
        .click();
      await page.getByRole("link", { name: "Открыть", exact: true }).click();
      await expect(page).toHaveURL(
        new RegExp(
          `/projects/${projectRef}/environments/environment_synthetic$`,
        ),
      );
      expect(
        fixture.events.filter((event) => event === "publish"),
      ).toHaveLength(1);
      expect(fixture.events).toContain("environment-readback");
      await page
        .getByRole("button", { name: "Влияние ревизии", exact: true })
        .click();
      const environmentImpact = page.getByRole("dialog", {
        name: "Перепривязка окружения",
        exact: true,
      });
      await environmentImpact.getByRole("checkbox").check();
      await environmentImpact
        .getByRole("button", {
          name: "Перепривязать выбранные: 1",
          exact: true,
        })
        .click();
      await expect(environmentImpact.locator(".impact-receipt")).toContainText(
        "agent_impact_synthetic",
      );
      expect(fixture.events.filter((event) => event === "rebind")).toHaveLength(
        1,
      );
      await page.screenshot({
        path: testInfo.outputPath(`environment-impact-${String(width)}.png`),
        fullPage: false,
      });
      await environmentImpact
        .getByRole("button", { name: "Закрыть", exact: true })
        .last()
        .click();
      expect(fixture.failures).toEqual([]);
      expect(
        await page.evaluate(
          () => document.documentElement.scrollWidth <= window.innerWidth,
        ),
      ).toBe(true);
      await page.screenshot({
        path: testInfo.outputPath(`environment-${String(width)}.png`),
        fullPage: true,
      });
      await expect(
        page.getByRole("button", { name: "Добавить переменную", exact: true }),
      ).toHaveCount(0);
      for (const section of [
        "Образ и инструменты",
        "Переменные",
        "Секреты",
        "Ресурсы и доступ",
        "Готовность",
      ]) {
        await page.getByRole("tab", { name: section, exact: true }).click();
        await expect(page.getByRole("tabpanel")).toBeVisible();
        await expect(
          page.getByRole("button", {
            name: "Добавить переменную",
            exact: true,
          }),
        ).toHaveCount(section === "Переменные" ? 1 : 0);
        expect(
          await page.evaluate(
            () => document.documentElement.scrollWidth <= window.innerWidth,
          ),
        ).toBe(true);
        await page.screenshot({
          path: testInfo.outputPath(
            `environment-${String(width)}-${section}.png`,
          ),
          fullPage: true,
        });
      }
      for (const [endpoint, status] of [
        ["readiness", 404],
        ["agents", 404],
        ["readiness", 503],
      ] as const) {
        fixture.setInspectorFailure(undefined);
        await page.goto(`/projects/${projectRef}/environments`);
        const selectEnvironment = page.locator(".environment-name").first();
        await selectEnvironment.click();
        const inspector = page.locator(".environment-inspector");
        await expect(
          inspector.locator(".environment-lifecycle .status-badge"),
        ).toHaveText("Готов");
        await selectEnvironment.click();
        await expect(inspector).toHaveCount(0);
        const beforeLists = fixture.events.filter(
          (event) => event === "list",
        ).length;
        const beforeDiagnostics = inspectorFailureDiagnostics;
        fixture.setInspectorFailure({ endpoint, status });
        expectedInspectorFailure = status;
        await selectEnvironment.click();
        if (status === 404) {
          await expect(inspector).toHaveCount(0);
          await expect(page.locator(".environment-table tbody tr")).toHaveCount(
            0,
          );
          expect(
            fixture.events.filter((event) => event === "list"),
          ).toHaveLength(beforeLists + 1);
        } else {
          await expect(inspector).toBeVisible();
          await expect(
            inspector.locator(".environment-lifecycle .status-badge"),
          ).toHaveCount(0);
          await expect(page.locator(".environment-table tbody tr")).toHaveCount(
            1,
          );
          expect(
            fixture.events.filter((event) => event === "list"),
          ).toHaveLength(beforeLists);
        }
        await expect
          .poll(() => inspectorFailureDiagnostics)
          .toBe(beforeDiagnostics + 1);
        expectedInspectorFailure = undefined;
      }
      fixture.setInspectorFailure(undefined);
    }
    if (width === 1440) {
      let speechAvailable = true;
      let grants = 0;
      let transcriptions = 0;
      let speechClockOffset = 0;
      await context.grantPermissions(["microphone"], {
        origin: "https://kodex.test",
      });
      await page.route("**/api/v1/bootstrap", async (route) => {
        grants += 1;
        await route.fulfill({
          headers: { ETag: '"1"' },
          json: {
            ...bootstrap,
            speechTranscription: speechAvailable
              ? {
                  available: true,
                  reason: "READY",
                  validUntil: new Date(
                    Date.now() + speechClockOffset + 30_000,
                  ).toISOString(),
                }
              : { available: false, reason: "STT_PERMISSION_DENIED" },
          },
        });
      });
      await page.route("**/api/v1/speech/transcriptions", async (route) => {
        transcriptions += 1;
        expect(route.request().method()).toBe("POST");
        expect(new URL(route.request().url()).search).toBe("");
        expect(route.request().headers()["x-kodex-project-id"]).toBeUndefined();
        expect(route.request().headers()["content-type"]).toContain(
          "multipart/form-data; boundary=",
        );
        await route.fulfill({ json: { text: "Диктовка" } });
      });
      await page.goto("/configurations/PROMPT_TEMPLATE/new");
      const voice = page.locator(".configuration-editor .voice-input");
      await expect(voice.locator("button")).toHaveCount(1);
      const prompt = page.getByRole("textbox", {
        name: "Содержимое",
        exact: true,
      });
      await prompt.focus();
      await voice.locator("button").click();
      await expect(voice).toHaveAttribute("data-state", "recording");
      await page.waitForTimeout(350);
      await voice.locator("button").first().click();
      await expect(prompt).toHaveText("Диктовка");
      await prompt.press("Control+z");
      await expect(prompt).toHaveText("");
      await voice.locator("button").click();
      await expect(voice).toHaveAttribute("data-state", "recording");
      for (let renewal = 0; renewal < 3; renewal += 1) {
        const before = grants;
        speechClockOffset += 15_000;
        await page.clock.fastForward(15_000);
        await expect.poll(() => grants).toBeGreaterThan(before);
        await expect(voice).toHaveAttribute("data-state", "recording");
      }
      speechAvailable = false;
      await page.clock.fastForward(15_000);
      await expect(voice.locator("button")).toHaveCount(0);
      await expect(voice).toHaveAttribute("data-state", "idle");
      expect(transcriptions).toBe(1);
      await expect(
        page.getByRole("textbox", { name: "Содержимое", exact: true }),
      ).toHaveText("");
    }
    if (width !== 900) {
      const providers = await installProviderFixture(page);
      await page.goto("/administration/providers");
      await page
        .getByRole("button", { name: "Добавить учётную запись", exact: true })
        .click();
      await page
        .getByRole("dialog")
        .getByLabel("Название", { exact: true })
        .fill("Проверка устройства synthetic");
      await page
        .getByRole("dialog")
        .getByRole("button", { name: "Провайдер", exact: true })
        .click();
      await page.getByRole("option", { name: /OpenAI/ }).click();
      await page
        .getByRole("dialog")
        .getByRole("button", { name: "Создать", exact: true })
        .click();
      let authorization = page.getByRole("dialog", {
        name: "Авторизация: Проверка устройства synthetic",
        exact: true,
      });
      await authorization
        .getByRole("button", { name: "Получить код", exact: true })
        .click();
      await expect(authorization.locator(".device-code")).toContainText(
        "SYNTHETIC-CODE",
      );
      await authorization
        .getByRole("button", { name: "Проверить сейчас", exact: true })
        .click();
      await expect(
        authorization.getByRole("button", {
          name: "Переавторизовать",
          exact: true,
        }),
      ).toBeVisible();
      await authorization
        .getByRole("button", { name: "Переавторизовать", exact: true })
        .click();
      await expect(authorization.locator(".device-code")).toContainText(
        "SYNTHETIC-CODE",
      );
      await authorization
        .getByRole("button", { name: "Закрыть", exact: true })
        .last()
        .click();
      const requestsBefore = providers.events.filter(
        (event) => event === "verify",
      ).length;
      await page.waitForTimeout(4200);
      expect(
        providers.events.filter((event) => event === "verify"),
      ).toHaveLength(requestsBefore);
      await page
        .getByRole("button", { name: "Добавить учётную запись", exact: true })
        .click();
      await page
        .getByRole("dialog")
        .getByLabel("Название", { exact: true })
        .fill("API synthetic");
      await page
        .getByRole("dialog")
        .getByRole("button", { name: "Создать", exact: true })
        .click();
      authorization = page.getByRole("dialog", {
        name: "Авторизация: API synthetic",
        exact: true,
      });
      await authorization
        .getByRole("tab", { name: "API key", exact: true })
        .click();
      await authorization
        .locator('input[type="password"]')
        .fill("synthetic-input-not-a-credential");
      await expect(authorization.locator(".voice-input-button")).toHaveCount(0);
      await authorization
        .getByRole("button", { name: "Авторизовать", exact: true })
        .click();
      await expect(authorization.locator('input[type="password"]')).toHaveCount(
        0,
      );
      await authorization
        .getByRole("button", { name: "Удалить", exact: true })
        .click();
      const deletion = page.getByRole("dialog", {
        name: "Зависимости и удаление",
        exact: true,
      });
      await deletion
        .getByRole("button", { name: "Запросить удаление", exact: true })
        .click();
      await deletion
        .getByRole("button", { name: "Подтвердить команду", exact: true })
        .click();
      await expect(
        deletion.getByText("Учётная запись удалена после очистки", {
          exact: true,
        }),
      ).toBeVisible();
      expect(providers.events).toContain("deleted-readback");
      await deletion
        .getByRole("button", { name: "Закрыть", exact: true })
        .last()
        .click();
      expect(providers.events).toContain("reauthorize");
      expect(providers.events).toContain("api-key");
      expect(providers.events).toContain("delete");
      expect(providers.failures).toEqual([]);
      expect(
        await page.evaluate(
          () => document.documentElement.scrollWidth <= window.innerWidth,
        ),
      ).toBe(true);
      await page.screenshot({
        path: testInfo.outputPath(`providers-${String(width)}.png`),
        fullPage: true,
      });
    }
    if (width !== 900) {
      const project = projects[0];
      if (!project) throw new Error("Missing synthetic project");
      await checkSecretEditor(
        page,
        project.ref,
        testInfo.outputPath(`secret-draft-${String(width)}.png`),
      );
      await page.screenshot({
        path: testInfo.outputPath(`secrets-${String(width)}.png`),
        fullPage: true,
      });
      await checkInteractionIdentities(page);
      await page.screenshot({
        path: testInfo.outputPath(
          `interaction-identities-${String(width)}.png`,
        ),
        fullPage: true,
      });
      const recipe = await checkRoleImageCatalog(page, project.ref);
      await page.screenshot({
        path: testInfo.outputPath(`role-images-${String(width)}.png`),
        fullPage: true,
      });
      await checkRoleImageHistory(page, recipe);
      await page.screenshot({
        path: testInfo.outputPath(`role-image-history-${String(width)}.png`),
        fullPage: true,
      });
      await checkWorkflowEditor(
        page,
        project.ref,
        width === 1440 || width === 390 ? bootstrap : undefined,
        async () => {
          await page.screenshot({
            path: testInfo.outputPath(
              `workflow-unpublished-${String(width)}.png`,
            ),
            fullPage: true,
          });
        },
      );
      await page.screenshot({
        path: testInfo.outputPath(`workflow-${String(width)}.png`),
        fullPage: true,
      });
      if (width === 390 || width === 2900) {
        await checkEffectiveCapabilities(page, project.ref);
        await page.screenshot({
          path: testInfo.outputPath(
            `effective-capabilities-${String(width)}.png`,
          ),
          fullPage: true,
        });
      }
      await checkContextResources(
        page,
        project.ref,
        contextEnvironment,
        async (name) => {
          await page.screenshot({
            path: testInfo.outputPath(`${name}-${String(width)}.png`),
            fullPage: false,
          });
        },
      );
      await page.screenshot({
        path: testInfo.outputPath(`context-${String(width)}.png`),
        fullPage: true,
      });
      await checkIntegrationPackage(page);
      await page.evaluate(() => window.scrollTo(0, 0));
      await page.screenshot({
        path: testInfo.outputPath(`integration-package-${String(width)}.png`),
        fullPage: true,
      });
      await checkEmailEffects(page, async () => {
        await page.screenshot({
          path: testInfo.outputPath(`email-effect-${String(width)}.png`),
          fullPage: false,
        });
      });
      await checkGrantCandidates(page, project.ref);
    }
    const catalogProject = projects[0];
    if (!catalogProject) throw new Error("Missing synthetic project");
    await checkRunsCatalog(
      page,
      catalogProject.ref,
      async () => {
        await page.screenshot({
          path: testInfo.outputPath(`runs-catalog-${String(width)}.png`),
          fullPage: false,
        });
      },
      () => {
        if (!invalidateRuns)
          throw new Error("Missing synthetic platform stream");
        invalidateRuns();
      },
    );
    await checkOrganizationCatalog(
      page,
      projects,
      () => {
        if (!invalidateAgents)
          throw new Error("Missing synthetic platform stream");
        invalidateAgents();
      },
      async () => {
        await page.screenshot({
          path: testInfo.outputPath(
            `organization-catalog-${String(width)}.png`,
          ),
          fullPage: false,
        });
      },
    );
    await expect(page.locator("header .realtime-status")).toHaveAttribute(
      "data-presentation-state",
      "live",
    );
    if (width === 1440 || width === 390 || width === 2900)
      await checkFileSelection(
        page,
        catalogProject.ref,
        async (name = "file-selection") => {
          await page.screenshot({
            path: testInfo.outputPath(`${name}-${String(width)}.png`),
            fullPage: name === "file-selection",
          });
        },
      );
    if (width === 1440 || width === 390)
      await checkAssistantHistory(page, catalogProject.ref);
    if (width === 2900 || width === 390)
      await checkHomeResults(page, catalogProject.ref, async () => {
        await page.screenshot({
          path: testInfo.outputPath(`home-results-${String(width)}.png`),
          fullPage: false,
        });
      });
    if (width === 2900 || width === 390)
      await checkResumableSessions(page, catalogProject.ref, async () => {
        await page.screenshot({
          path: testInfo.outputPath(`resumable-sessions-${String(width)}.png`),
          fullPage: false,
        });
      });
    expect(snapshotConflictDiagnostics).toBe(
      width === 390 || width === 2900 ? 1 : 0,
    );
    expect(publicationTimeoutDiagnostics).toBe(2);
    expect(failures).toEqual([]);
  });
}
