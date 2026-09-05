import { expect, test } from "@playwright/test";
import { overlaySchemaFixture } from "../src/test-utils/runtime-catalog-fixture";
import type {
  AgentRuntimeConfigurationView,
  RuntimeRevisionDiff,
} from "../src/shared/api/generated/openapi/types.gen";
for (const width of [1440, 390, 2900]) {
  test(`synthetic: runtime editor и revision diff ${String(width)}px`, async ({
    page,
  }, testInfo) => {
    await page.setViewportSize({ width, height: 900 });
    const failures: string[] = [];
    page.on("pageerror", (error) => failures.push(error.message));
    page.on("console", (message) => {
      if (["warning", "error"].includes(message.type()))
        failures.push(message.text());
    });
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
    const digest = "a".repeat(64);
    const now = "2026-09-05T00:00:00Z";
    let content = 'personality = "none"';
    let finishSave: (() => void) | undefined;
    let saves = 0;
    let diffReads = 0;
    const view: AgentRuntimeConfigurationView = {
      overlaySchema: overlaySchemaFixture,
      agentVersion: 3,
      skillBindings: [],
      memoryBindings: [],
      environmentBinding: {
        ref: "binding_synthetic",
        version: 1,
        agentRef: "agent_synthetic",
        environmentRef: "environment_synthetic",
        versionRef: "environment_revision",
        digest,
      },
      environment: {
        ref: "environment_synthetic",
        version: 1,
        projectRef: "project_synthetic",
        name: "Synthetic",
        description: "",
        state: "ACTIVE",
        updatedAt: now,
        ready: true,
        readinessBlockers: [],
        nextActions: [],
        currentVersion: {
          ref: "environment_revision",
          version: 1,
          revision: 1,
          values: [],
          secretDescriptors: [],
          tools: [],
          digest,
          createdAt: now,
          image: {
            artifactRef: "artifact_synthetic",
            recipeRef: "recipe_synthetic",
            recipeGeneration: 1,
            reference: `registry.invalid/test@sha256:${digest}`,
            digest,
          },
          policy: {
            resources: {
              cpuRequestMilli: 1000,
              cpuLimitMilli: 2000,
              memoryRequestMib: 1024,
              memoryLimitMib: 2048,
              ephemeralStorageRequestMib: 1024,
              ephemeralStorageLimitMib: 2048,
            },
            volumes: [],
            network: { denyByDefault: true, egress: [] },
            kubernetesAccess: { kind: "NONE", namespace: "kodex-runtime" },
            resourcesDigest: digest,
            volumesDigest: digest,
            networkDigest: digest,
            rbacDigest: digest,
          },
        },
      },
      configuration: {
        ref: "configuration_synthetic",
        version: 1,
        agentRef: "agent_synthetic",
        runtimeProfileRef: "runtime_synthetic",
        provider: "openai-codex",
        model: "model-synthetic",
        providerPolicy: {
          ref: "policy_synthetic",
          version: 1,
          mode: "FIXED",
          accountCandidates: [],
          digest,
          createdAt: now,
        },
        digest,
        createdAt: now,
      },
      publishedOverlay: {
        ref: "overlay_synthetic",
        version: 1,
        revision: 1,
        state: "PUBLISHED",
        content,
        digest,
        validationMessages: [],
        createdAt: now,
      },
      safeEffectiveConfig: content,
    };
    const current = {
      ref: "revision_current",
      version: 2,
      runRef: "run_current",
      sessionRef: "session_one",
      attempt: 2,
      revisionDigest: digest,
      createdAt: now,
    };
    await page.route("**/*", async (route) => {
      const url = new URL(route.request().url());
      if (url.origin !== "https://kodex.test") {
        failures.push("Unexpected origin");
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
      if (url.pathname === "/api/v1/runtime-selections") {
        await route.fulfill({ json: { items: [] } });
        return;
      }
      if (
        url.pathname === "/api/v1/projects/project_synthetic/template-variables"
      ) {
        await route.fulfill({
          json: { items: [], total: 0, nextPageToken: "" },
        });
        return;
      }
      if (
        url.pathname === "/api/v1/agents/agent_synthetic/runtime-configuration"
      ) {
        await route.fulfill({ json: view });
        return;
      }
      if (
        url.pathname === "/api/v1/agents/agent_synthetic/config-overlay-drafts"
      ) {
        saves++;
        const body = route.request().postDataJSON() as { content: string };
        content = body.content;
        expect(route.request().headers()["if-match"]).toBe('"3"');
        await new Promise<void>((resolve) => {
          finishSave = resolve;
        });
        await route.fulfill({
          json: {
            ...view,
            agentVersion: 4,
            draftOverlay: {
              ...view.publishedOverlay,
              ref: "draft_synthetic",
              version: 2,
              revision: 2,
              content,
              state: "DRAFT",
            },
          },
          headers: { ETag: '"4"' },
        });
        return;
      }
      if (url.pathname === "/api/v1/runs/run_current/runtime-revision-diff") {
        diffReads++;
        const diff: RuntimeRevisionDiff =
          diffReads === 1
            ? {
                current,
                previous: {
                  ...current,
                  ref: "revision_previous",
                  runRef: "run_previous",
                  attempt: 1,
                },
                changes: [
                  {
                    component: "MODEL",
                    previous: { ref: "previous-model" },
                    current: { ref: "current-model" },
                  },
                  { component: "IMAGE", current: { digest: "b".repeat(64) } },
                ],
              }
            : { current, changes: [] };
        await route.fulfill({ json: diff });
        return;
      }
      if (url.pathname.startsWith("/api/")) {
        failures.push(`Unhandled API ${url.pathname}`);
        await route.abort();
        return;
      }
      const response = await route.fetch({
        url: `http://127.0.0.1:43122${url.pathname}${url.search}`,
      });
      await route.fulfill({ response });
    });
    await page.goto("https://kodex.test/e2e/fixtures/runtime-detail.html");
    await expect(
      page.getByText("previous-model", { exact: true }),
    ).toBeVisible();
    await expect(
      page.getByText("current-model", { exact: true }),
    ).toBeVisible();
    const editor = page.locator(".overlay-panel > .code-editor .cm-content");
    await expect(editor).toBeVisible();
    await editor.fill('personality = "friendly"');
    const effort = page.getByRole("combobox", {
      name: "Степень рассуждения",
      exact: true,
    });
    await expect(effort.locator("option")).toHaveCount(3);
    await effort.selectOption("low");
    await expect(editor).toContainText('model_reasoning_effort = "low"');
    await expect(editor).toContainText('personality = "friendly"');
    const voice = page
      .locator(".overlay-panel")
      .getByRole("button", { name: "Голосовой ввод", exact: true });
    await expect(voice).toBeVisible();
    await page
      .getByRole("button", { name: "Сохранить черновик", exact: true })
      .click();
    await expect.poll(() => saves).toBe(1);
    await expect(editor).toHaveAttribute("contenteditable", "false");
    await expect(voice).toHaveCount(0);
    finishSave?.();
    await expect(editor).toHaveAttribute("contenteditable", "true");
    await expect(editor).toContainText('personality = "friendly"');
    await expect(editor).toContainText('model_reasoning_effort = "low"');
    expect(content).toContain('model_reasoning_effort = "low"');
    await page
      .getByRole("button", { name: "Новая ревизия", exact: true })
      .click();
    await expect(
      page.getByText("Первая ревизия сессии", { exact: true }),
    ).toBeVisible();
    await expect(
      page.getByText("Изменённых компонентов нет", { exact: true }),
    ).toBeVisible();
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
    await page.screenshot({
      path: testInfo.outputPath(`runtime-detail-${String(width)}.png`),
      fullPage: true,
    });
    await page
      .getByRole("button", { name: "Проверить редакторы", exact: true })
      .click();
    const analogs = page.getByTestId("analog-editors");
    await expect(analogs.locator(".voice-input button")).toHaveCount(2);
    await analogs.getByLabel("Выполняется сохранение", { exact: true }).check();
    await expect(analogs.locator(".voice-input button")).toHaveCount(0);
    await expect(analogs.locator(".cm-content")).toHaveAttribute(
      "contenteditable",
      "false",
    );
    await expect(analogs.locator("textarea")).toBeDisabled();
    await analogs
      .getByLabel("Выполняется сохранение", { exact: true })
      .uncheck();
    await expect(analogs.locator(".voice-input button")).toHaveCount(2);
    await analogs.screenshot({
      path: testInfo.outputPath(`analog-editors-${String(width)}.png`),
    });
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
    expect(failures).toEqual([]);
  });
}
