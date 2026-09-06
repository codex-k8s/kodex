import { expect, test } from "@playwright/test";
import type {
  Schedule,
  SchedulePreview,
  SchedulePreviewInput,
  TemplateVariable,
} from "../src/shared/api/generated/openapi/types.gen";

const digest = "a".repeat(64);
function savedSchedule(targetType: "AGENT" | "WORKFLOW"): Schedule {
  const target = {
    type: targetType,
    ref: targetType === "AGENT" ? "agent_preview" : "workflow_preview",
    displayName: "Цель предпросмотра",
    version: 3,
  };
  const spec = {
    name: "Утренняя сводка",
    target,
    preset: "DAILY" as const,
    cronExpression: "0 9 * * *",
    timezone: "Europe/Saratov",
    input: { task: "Старый входной параметр", retained: { exact: true } },
    automationText: "Сохранённая задача",
    promptInputs: {},
    sessionPolicy: "NEW_EACH_RUN" as const,
    notificationPolicy: "CONTROL_CENTER_ONLY" as const,
    dstGapPolicy: "SHIFT_FORWARD" as const,
    dstFoldPolicy: "RUN_ONCE_EARLIEST" as const,
    misfirePolicy: "COALESCE" as const,
    overlapPolicy: "FORBID" as const,
    targetVersion: 3,
    targetDigest: digest,
  };
  return {
    ...spec,
    ref: "schedule_preview",
    version: 7,
    projectRef: "project_preview",
    state: "ACTIVE",
    timeOfDay: "09:00",
    nextActions: ["EDIT"],
    currentRevision: {
      ...spec,
      ref: "revision_saved",
      revision: 7,
      digest,
      createdAt: "2026-09-06T00:00:00Z",
    },
  };
}
function materialized(
  body: SchedulePreviewInput,
  saved: Schedule,
): SchedulePreview {
  const response: SchedulePreview = {
    normalizedCronExpression: "0 9 * * *",
    occurrences: [
      "2026-09-07T05:00:00Z",
      "2026-09-08T05:00:00Z",
      "2026-09-09T05:00:00Z",
      "2026-09-10T05:00:00Z",
      "2026-09-11T05:00:00Z",
    ],
    dstGapPolicy: "SHIFT_FORWARD",
    dstFoldPolicy: "RUN_ONCE_EARLIEST",
    misfirePolicy: "COALESCE",
    overlapPolicy: "FORBID",
  };
  const context = body.materialization;
  if (!context) return response;
  const current = context.mode === "CURRENT_REVISION";
  const variable = (name: string): TemplateVariable => ({
    name: `automation.${name}`,
    valueType: "STRING",
    description:
      name === "ref"
        ? "Ссылка Автоматизации"
        : `i18n:AUTOMATION_VARIABLE_${name.toUpperCase()}`,
    example: name === "revision" ? (current ? "7" : "") : name,
    source: "AUTOMATION",
    collection: false,
    available: name !== "revision" || current,
    reason:
      name === "revision" && !current ? "REVISION_NOT_SAVED" : "AVAILABLE",
    itemFields: [],
  });
  response.automationVariables = [
    variable("ref"),
    variable("name"),
    variable("task"),
    variable("scheduled_at"),
    variable("timezone"),
    variable("revision"),
  ];
  response.materializationPin = {
    scheduleRef: saved.ref,
    scheduleVersion: saved.version,
    scheduledFor: response.occurrences[0] ?? "",
    timezone: saved.timezone,
    continuation: false,
    revisionAvailable: current,
    mode: current ? "CURRENT_REVISION" : "DRAFT",
    executionActorRef: current ? "saved_author" : "current_viewer",
    baseRevisionRef: saved.currentRevision.ref,
    baseRevisionDigest: digest,
    ...(current
      ? { revisionRef: saved.currentRevision.ref, revisionDigest: digest }
      : {}),
  };
  response.materializedPrompt = {
    safePreview: context.task,
    diagnostics: [],
    complete: true,
    templateRef: "template_preview",
    templateDigest: digest,
    materializationDigest: digest,
    effectiveCapabilities: [],
    serviceTemplateRevision: "1",
    serviceTemplateDigest: digest,
    variableSnapshotDigest: digest,
    locale: "ru",
    slots: [],
    sections: [
      {
        source: "USER_TEMPLATE",
        userKind: "AUTOMATION_TASK",
        content: context.task,
      },
    ],
    contextPin: {
      digest,
      ...(context.targetType === "AGENT"
        ? { agentRef: context.targetRef }
        : {
            agentRef: "coordinator",
            workflowRef: context.targetRef,
            workflowRevisionRef: "workflow_revision",
            workflowStageKey: "workflow.coordinator.initial",
          }),
    },
    ...(context.includeFullMaterialization
      ? {
          fullMaterializedPrompt: `Служебный блок платформы ${context.targetType}\n\nЗадача Автоматизации: ${context.task}`,
        }
      : {}),
  };
  return response;
}

for (const width of [390, 2900]) {
  test(`synthetic: Automation DRAFT/CURRENT и задача ${String(width)}px`, async ({
    page,
  }, testInfo) => {
    await page.setViewportSize({ width, height: width === 390 ? 844 : 1600 });
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
    let saved = savedSchedule("AGENT");
    const requests: SchedulePreviewInput[] = [];
    let delayNext = false;
    let releasePreview: (() => void) | undefined;
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
      if (url.pathname === "/api/v1/schedules/schedule_preview") {
        await route.fulfill({ json: saved });
        return;
      }
      if (url.pathname === "/api/v1/schedules/preview") {
        const body = route.request().postDataJSON() as SchedulePreviewInput;
        expect(body.limit).toBe(5);
        if (body.materialization) {
          requests.push(body);
          expect(body.materialization.input).toEqual(saved.input);
          expect(body.materialization.expectedScheduleVersion).toBe(7);
          expect(body.materialization).not.toHaveProperty("executionActorRef");
          if (body.materialization.mode === "CURRENT_REVISION")
            expect(body.materialization.task).toBe(saved.automationText);
        }
        const response = materialized(body, saved);
        if (body.materialization && delayNext) {
          delayNext = false;
          await new Promise<void>((resolve) => {
            releasePreview = resolve;
          });
          releasePreview = undefined;
        }
        await route.fulfill({ json: response });
        return;
      }
      if (url.pathname.startsWith("/api/")) {
        failures.push(`Unexpected API ${url.pathname}`);
        await route.fulfill({
          status: 404,
          json: { status: 404, code: "NOT_FOUND" },
        });
        return;
      }
      await route.fulfill({
        response: await route.fetch({
          url: `http://127.0.0.1:43122${url.pathname}${url.search}`,
        }),
      });
    });
    for (const targetType of ["AGENT", "WORKFLOW"] as const) {
      saved = savedSchedule(targetType);
      await page.goto("/e2e/fixtures/automation-preview.html");
      await expect(page.getByLabel("Задача", { exact: true })).toHaveValue(
        "Сохранённая задача",
      );
      await page
        .getByLabel("Задача", { exact: true })
        .fill(`Новая задача ${targetType}`);
      const panel = page.locator(".automation-prompt-preview");
      await panel
        .getByRole("button", { name: "Проверить запрос Автоматизации" })
        .click();
      await expect(
        panel.getByText("current_viewer", { exact: true }),
      ).toBeVisible();
      await expect(
        panel
          .getByText("Будет назначена после сохранения", { exact: true })
          .first(),
      ).toBeVisible();
      expect(requests.at(-1)?.materialization?.task).toBe(
        `Новая задача ${targetType}`,
      );
      delayNext = true;
      await panel
        .getByRole("button", { name: "Проверить запрос Автоматизации" })
        .click();
      await expect.poll(() => Boolean(releasePreview)).toBe(true);
      await page
        .getByLabel("Задача", { exact: true })
        .fill(`Изменённая задача ${targetType}`);
      releasePreview?.();
      await expect(
        panel.getByText("current_viewer", { exact: true }),
      ).toHaveCount(0);
      await page
        .getByLabel("Задача", { exact: true })
        .fill(`Новая задача ${targetType}`);
      await panel.getByRole("combobox").selectOption("CURRENT_REVISION");
      await expect(
        panel.getByText("current_viewer", { exact: true }),
      ).toHaveCount(0);
      await panel
        .getByRole("button", { name: "Проверить запрос Автоматизации" })
        .click();
      await expect(
        panel.getByText("saved_author", { exact: true }),
      ).toBeVisible();
      await panel
        .getByRole("checkbox", {
          name: "Запросить полный текст с отдельной проверкой доступа",
        })
        .check();
      await panel
        .getByRole("button", { name: "Проверить запрос Автоматизации" })
        .click();
      await expect(
        panel.getByText(`Служебный блок платформы ${targetType}`, {
          exact: true,
        }),
      ).toBeVisible();
      await expect(
        panel.getByText("Задача Автоматизации: Сохранённая задача", {
          exact: true,
        }),
      ).toBeVisible();
      await page
        .getByRole("button", { name: "Сохранить", exact: true })
        .click();
      const submitted = JSON.parse(
        (await page.locator("#submitted").textContent()) ?? "{}",
      ) as { automationText: string; input: unknown };
      expect(submitted.automationText).toBe(`Новая задача ${targetType}`);
      expect(submitted.input).toEqual(saved.input);
      await expect(panel.getByRole("alert")).toHaveCount(0);
      await panel
        .getByText("saved_author", { exact: true })
        .scrollIntoViewIfNeeded();
      await page.screenshot({
        path: testInfo.outputPath(
          `automation-${targetType}-${String(width)}.png`,
        ),
        fullPage: true,
      });
      expect(
        await page.evaluate(
          () => document.documentElement.scrollWidth <= innerWidth,
        ),
      ).toBe(true);
    }
    expect(failures).toEqual([]);
  });
}
