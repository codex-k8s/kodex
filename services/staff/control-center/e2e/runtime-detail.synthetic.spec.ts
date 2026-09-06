import { expect, test } from "@playwright/test";
import { overlaySchemaFixture } from "../src/test-utils/runtime-catalog-fixture";
import { providerUsageFixture } from "../src/test-utils/provider-usage-fixture";
import type {
  AgentRuntimeConfigurationView,
  RuntimeRevisionDiff,
  ProviderAccountUsageContext,
  ProviderAccount,
  PromptTemplatePreview,
} from "../src/shared/api/generated/openapi/types.gen";
for (const width of [1440, 390, 2900]) {
  test(`synthetic: runtime editor и revision diff ${String(width)}px`, async ({
    page,
  }, testInfo) => {
    await page.setViewportSize({
      width,
      height: width === 390 ? 844 : width === 2900 ? 1600 : 900,
    });
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
    let rollbacks = 0;
    let usageRevoked = false;
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
      const historyPath =
        "/api/v1/agents/agent_synthetic/config-overlay/revisions";
      const oldOverlay = {
        ...view.publishedOverlay,
        ref: "overlay_old",
        revision: 7,
        state: "SUPERSEDED",
        content: 'personality = "pragmatic"',
        digest: "b".repeat(64),
      };
      if (url.pathname === historyPath) {
        expect(url.searchParams.get("pageSize")).toBe("30");
        await route.fulfill({ json: { items: [oldOverlay], total: 1 } });
        return;
      }
      if (url.pathname === `${historyPath}/overlay_old`) {
        await route.fulfill({ json: oldOverlay });
        return;
      }
      if (
        url.pathname ===
        "/api/v1/agents/agent_synthetic/config-overlay-rollbacks"
      ) {
        expect(route.request().headers()["if-match"]).toBe('"4"');
        expect(route.request().postDataJSON()).toEqual({
          publishedOverlayRef: "overlay_old",
        });
        rollbacks++;
        await route.fulfill({
          json: {
            ...view,
            agentVersion: 5,
            publishedOverlay: {
              ...oldOverlay,
              ref: "overlay_restored",
              revision: 8,
              state: "PUBLISHED",
            },
            safeEffectiveConfig: oldOverlay.content,
          },
        });
        return;
      }
      if (url.pathname === "/api/v1/runtime-selections") {
        await route.fulfill({
          json: {
            items: [
              {
                ref: "runtime_synthetic",
                name: "Профиль каталога",
                revision: "runtime-revision",
                ready: true,
                provider: "openai-codex",
                model: "model-synthetic",
              },
            ],
          },
        });
        return;
      }
      if (url.pathname.startsWith("/api/v1/provider-accounts")) {
        expect(url.searchParams.get("usagePurpose")).toBe("CONFIGURE");
        expect(url.searchParams.get("usageAgentRef")).toBe("agent_synthetic");
        expect(url.searchParams.get("usageRuntimeProfileRef")).toBe(
          "runtime_synthetic",
        );
        const usageContext: ProviderAccountUsageContext = {
          purpose: "CONFIGURE",
          agentRef: "agent_synthetic",
          runtimeProfileRef: "runtime_synthetic",
          providerDefinitionKey: "openai-codex",
          model: url.searchParams.get("usageModel") ?? undefined,
          reasoningEffort:
            url.searchParams.get("usageReasoningEffort") ?? undefined,
        };
        const account: ProviderAccount = {
          ref: "pacc_synthetic",
          version: 1,
          name: "Учётная запись каталога",
          definitionKey: "openai-codex",
          state: "AUTHORIZED",
          enabled: true,
          ready: true,
          externalAccountMasked: "fixture",
          createdAt: now,
          updatedAt: now,
          nextActions: [],
          usage: providerUsageFixture(
            usageContext,
            usageRevoked
              ? {
                  eligibleForSelection: false,
                  allowedToSubmit: false,
                  actorEligibility: {
                    state: "BLOCKED",
                    reason: "PERMISSION_REQUIRED",
                    remediation: "CONTACT_ADMINISTRATOR",
                  },
                }
              : {
                  providerHealth: {
                    state: "READY",
                    reason: "CREDENTIALED_CATALOG_REACHABLE",
                    remediation: "NONE",
                  },
                  providerHealthObservedAt: now,
                  providerHealthExpiresAt: "2099-01-01T00:00:00Z",
                },
          ),
        };
        await route.fulfill({
          json: url.pathname.endsWith("/provider-accounts")
            ? { items: [account], nextPageToken: "", nextActions: [] }
            : account,
        });
        return;
      }
      if (url.pathname === "/api/v1/model-capabilities") {
        expect(url.searchParams.get("providerAccountRef")).toBe(
          "pacc_synthetic",
        );
        const usage = providerUsageFixture();
        await route.fulfill({
          json: {
            items: [
              {
                id: "model-synthetic",
                providerDefinitionKey: "openai-codex",
                available: true,
                eligibleProviderAccountRefs: ["pacc_synthetic"],
                reasoningEfforts: ["low", "high"],
                defaultReasoningEffort: "high",
                readinessBlockers: [],
              },
            ],
            total: 1,
            nextPageToken: "",
            catalogRevision: usage.catalogRevision,
            catalogDigest: usage.catalogDigest,
            catalogStatus: usage.catalogStatus,
          },
        });
        return;
      }
      if (url.pathname === "/api/v1/prompt-templates/catalog/query") {
        await route.fulfill({
          json: {
            items: [],
            total: 0,
            nextPageToken: "",
            contextPin: {
              digest,
              agentRef: "agent_synthetic",
              agentVersion: 3,
            },
          },
        });
        return;
      }
      if (url.pathname === "/api/v1/prompt-templates/preview") {
        const preview: PromptTemplatePreview = {
          safePreview:
            "Материализованные инструкции для проверки прокрутки.\n\n".repeat(
              100,
            ),
          diagnostics: [],
          complete: true,
          templateRef: "template_synthetic",
          templateDigest: digest,
          materializationDigest: digest,
          effectiveCapabilities: [],
          serviceTemplateRevision: "1",
          serviceTemplateDigest: digest,
          variableSnapshotDigest: digest,
          locale: "ru",
          slots: [],
          sections: [],
          contextPin: { digest, agentRef: "agent_synthetic", agentVersion: 3 },
        };
        await route.fulfill({ json: preview });
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
      .getByRole("button", { name: "История overlay", exact: true })
      .click();
    const history = page.getByRole("dialog", { name: "История overlay" });
    await history
      .getByRole("button", { name: "Выберите опубликованную ревизию" })
      .click();
    await page.getByRole("option").filter({ hasText: "7" }).click();
    await expect(history.locator(".cm-content")).toHaveAttribute(
      "contenteditable",
      "false",
    );
    await expect(history.locator(".cm-content")).toContainText(
      'personality = "pragmatic"',
    );
    await history
      .getByRole("button", {
        name: "Восстановить выбранную ревизию",
        exact: true,
      })
      .click();
    await expect.poll(() => rollbacks).toBe(1);
    await expect(editor).toContainText('personality = "pragmatic"');
    await page
      .getByRole("button", { name: "Новая ревизия", exact: true })
      .click();
    await expect(
      page.getByText("Первая ревизия сессии", { exact: true }),
    ).toBeVisible();
    await expect(
      page.getByText("Изменённых компонентов нет", { exact: true }),
    ).toBeVisible();
    await page.locator(".provider-selector__trigger").click();
    await page.getByRole("option", { name: /Учётная запись каталога/ }).click();
    const saveRuntime = page.getByRole("button", {
      name: "Сохранить runtime",
      exact: true,
    });
    await expect(saveRuntime).toBeEnabled();
    const selectedUsage = page.locator(
      ".provider-selector__selected .provider-usage",
    );
    await selectedUsage.locator("summary").click();
    const usageBox = await selectedUsage.boundingBox();
    const accountBox = await page
      .locator(".provider-selector__selected-row")
      .boundingBox();
    if (!usageBox || !accountBox)
      throw new Error("Provider usage geometry is unavailable");
    expect(usageBox.width).toBeGreaterThan(accountBox.width * 0.85);
    await expect(
      selectedUsage.getByText("Доступность каталога", { exact: true }),
    ).toBeVisible();
    await expect(
      selectedUsage.getByText("Ёмкость выполнения", { exact: true }),
    ).toBeVisible();
    await expect(
      selectedUsage.getByText("Ваши полномочия", { exact: true }),
    ).toBeVisible();
    await expect(selectedUsage).toContainText(
      "Проверка каталога этой учётной записи, не общая доступность провайдера",
    );
    await expect(selectedUsage).toContainText("Ёмкость занята");
    await page.screenshot({
      path: testInfo.outputPath(`provider-usage-${String(width)}.png`),
      fullPage: true,
    });
    usageRevoked = true;
    await page
      .getByRole("button", { name: "Обновить проверку", exact: true })
      .click();
    await expect(saveRuntime).toBeDisabled();
    await expect(selectedUsage).toContainText("Недостаточно полномочий");
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
    await expect(analogs.locator(".variable-catalog")).toHaveAttribute(
      "aria-busy",
      "false",
    );
    await analogs.screenshot({
      path: testInfo.outputPath(`analog-editors-${String(width)}.png`),
    });
    const instructions = analogs.locator(".instructions-panel");
    const geometry = () =>
      instructions
        .locator(".instructions-panel__workspace")
        .evaluate((element) => {
          const editorElement = element.querySelector(
            ".instructions-panel__editor",
          );
          const variablesElement = element.querySelector(
            ".instructions-panel__variables",
          );
          if (!editorElement || !variablesElement)
            throw new Error("Instruction editor geometry is unavailable");
          const editor = editorElement.getBoundingClientRect();
          const variables = variablesElement.getBoundingClientRect();
          const row = element.getBoundingClientRect();
          return {
            editorHeight: editor.height,
            variablesHeight: variables.height,
            bottom: row.bottom + scrollY,
          };
        });
    const before = await geometry();
    expect(
      Math.abs(before.editorHeight - before.variablesHeight),
    ).toBeLessThanOrEqual(1);
    expect(before.editorHeight).toBeLessThanOrEqual(680);
    for (const label of ["Предпросмотр", "Проверка подстановки"]) {
      await instructions
        .getByRole("button", { name: label, exact: true })
        .click();
      const preview = instructions.locator(".instructions-panel__preview");
      await expect(preview.locator(".safe-markdown")).toBeVisible();
      const after = await geometry();
      expect(Math.abs(after.bottom - before.bottom)).toBeLessThanOrEqual(1);
      expect(after.editorHeight).toBe(before.editorHeight);
      expect(after.variablesHeight).toBe(before.variablesHeight);
      expect(
        await preview.evaluate(
          (element) => element.scrollHeight > element.clientHeight,
        ),
      ).toBe(true);
      await preview.evaluate((element) => {
        element.scrollTop = element.scrollHeight;
      });
      expect(
        await preview.evaluate((element) => element.scrollTop),
      ).toBeGreaterThan(0);
    }
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
    expect(failures).toEqual([]);
  });
}
