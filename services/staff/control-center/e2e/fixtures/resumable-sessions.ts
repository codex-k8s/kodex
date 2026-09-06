import { expect, type Page } from "@playwright/test";
import type {
  Agent,
  PromptVariableCatalogInput,
  PromptTemplatePreviewInput,
  PromptTemplatePreview,
} from "../../src/shared/api/generated/openapi/types.gen";
import { syntheticCatalogRun } from "./catalog-run";

export async function checkResumableSessions(
  page: Page,
  projectRef: string,
  capture: () => Promise<void>,
) {
  let stale = false;
  let targetReads = 0;
  const agent: Agent = {
    ref: "agent_session_catalog",
    version: 1,
    projectRef,
    name: "Сотрудник продолжения",
    purpose: "Проверка Session",
    roleDescription: "Аналитик",
    state: "READY",
    enabled: true,
    system: false,
    runtimeReady: true,
    runtimeRef: "runtime_synthetic",
    runtimeName: "Synthetic",
    capabilities: [],
    integrations: [],
    knowledgeArtifactRefs: [],
    updatedAt: "2026-09-05T00:00:00Z",
    nextActions: ["OPEN", "LAUNCH"],
  };
  const run = (index: number) => ({
    ...syntheticCatalogRun(index, projectRef),
    title: `Продолжение ${String(index)}`,
    state: "SUCCEEDED" as const,
    nextActions: ["OPEN", "ADD_TURN"] as const,
    target: {
      type: "AGENT" as const,
      ref: agent.ref,
      displayName: agent.name,
      version: 1,
    },
  });
  await page.route("**/api/v1/runs?**", async (route) => {
    const params = new URL(route.request().url()).searchParams;
    if (params.get("resumableSessionsOnly") !== "true") return route.fallback();
    expect(params.getAll("states")).toEqual([]);
    expect(params.get("pageSize")).toBe("30");
    if (params.get("targetRef")) {
      expect(params.get("projectRef")).toBe(projectRef);
      expect(params.get("targetType")).toBe("AGENT");
      expect(params.get("targetRef")).toBe(agent.ref);
      targetReads++;
      await route.fulfill({ json: { items: [run(70)], total: 1 } });
      return;
    }
    if (params.get("pageToken")) {
      expect(params.get("pageToken")).toBe("session-snapshot");
      stale = true;
      await route.fulfill({
        status: 412,
        json: {
          status: 412,
          code: "VERSION_OR_STATE_CONFLICT",
          retryable: true,
        },
      });
      return;
    }
    await route.fulfill({
      json: {
        items: stale
          ? [run(50)]
          : Array.from({ length: 30 }, (_, index) => run(index)),
        total: stale ? 1 : 43,
        ...(stale ? {} : { nextPageToken: "session-snapshot" }),
      },
    });
  });
  await page.goto("/");
  const card = page.locator('.home-result-catalog[data-kind="SESSION"]');
  await expect(card.locator("header > span")).toHaveText("43");
  expect(
    await card
      .locator(".home-result-rows")
      .evaluate((element) => element.clientHeight),
  ).toBeLessThanOrEqual(552);
  await card.locator(".home-result-rows").evaluate((element) => {
    element.scrollTop = element.scrollHeight;
    element.dispatchEvent(new Event("scroll"));
  });
  await expect(card.locator("header > span")).toHaveText("1");
  await expect(card.locator(".home-result-row")).toHaveCount(1);
  await expect(
    card.getByRole("link", { name: "Продолжение 50", exact: true }),
  ).toBeVisible();
  await card
    .getByRole("button", { name: "Развернуть список", exact: true })
    .click();
  await expect(
    page
      .getByRole("dialog", { name: "Продолжить сессию", exact: true })
      .locator(".home-result-row"),
  ).toHaveCount(1);
  await page
    .getByRole("dialog")
    .last()
    .getByRole("button", { name: "Закрыть", exact: true })
    .click();
  await page.route(`**/api/v1/agents/${agent.ref}`, (route) =>
    route.fulfill({ json: agent }),
  );
  const attachmentReads: string[] = [];
  await page.route(
    `**/api/v1/projects/${projectRef}/run-attachment-eligibility?**`,
    async (route) => {
      const query = new URL(route.request().url()).searchParams;
      expect(query.get("targetType")).toBe("AGENT");
      expect(query.get("targetRef")).toBe(agent.ref);
      const runRef = query.get("runRef") ?? "";
      attachmentReads.push(runRef);
      await route.fulfill({
        json: {
          projectRef,
          targetType: "AGENT",
          targetRef: agent.ref,
          ...(runRef ? { runRef } : {}),
          runVersion: runRef ? run(70).version : 0,
          eligible: false,
          reason: "AGENT_CAPABILITY_REQUIRED",
          digest: "a".repeat(64),
          evaluatedAt: "2026-09-05T12:00:00Z",
        },
      });
    },
  );
  await page.goto(
    `/projects/${projectRef}/runs/new?targetType=AGENT&targetRef=${agent.ref}`,
  );
  const continuation = page.locator(
    'input[name="new-run-session-policy"][value="CONTINUE"]',
  );
  await expect(continuation).toBeEnabled();
  await continuation.check();
  await expect(page.locator(".new-run-session-picker__overlay")).toBeVisible();
  await expect(
    page
      .locator(".new-run-session-picker__overlay")
      .getByText("Продолжение 70", { exact: true }),
  ).toBeVisible();
  expect(targetReads).toBeGreaterThan(0);
  await capture();
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
  await page
    .locator(".new-run-session-picker__overlay")
    .getByText("Продолжение 70", { exact: true })
    .click();
  await expect(page.locator("#new-run-session-picker-trigger")).toContainText(
    "Продолжение 70",
  );
  await expect.poll(() => attachmentReads.includes(run(70).ref)).toBe(true);
  await expect(
    page.getByText("Не всем исполнителям предоставлена возможность «Файлы».", {
      exact: true,
    }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Выбрать файлы", exact: true }),
  ).toBeDisabled();
  let previews = 0;
  const pin = {
    digest: "b".repeat(64),
    agentRef: agent.ref,
    agentVersion: agent.version,
    previousRuntimeRevisionRef: "runtime_previous",
  };
  await page.route(
    "**/api/v1/prompt-templates/catalog/query",
    async (route) => {
      const body = route.request().postDataJSON() as PromptVariableCatalogInput;
      expect(body.targetKind).toBe("SESSION_CONTINUATION");
      expect(body.targetRef).toBe(run(70).sessionRef);
      expect(body.context?.task).toMatch(/^Продолжить/);
      expect(body.context?.attachmentSetRef).toBeUndefined();
      await route.fulfill({ json: { items: [], total: 0, contextPin: pin } });
    },
  );
  await page.route("**/api/v1/prompt-templates/preview", async (route) => {
    const body = route.request().postDataJSON() as PromptTemplatePreviewInput;
    expect(body.targetKind).toBe("SESSION_CONTINUATION");
    expect(body.targetRef).toBe(run(70).sessionRef);
    expect(body.template).toBe("");
    expect(body.expectedContextDigest).toBe(pin.digest);
    expect(body.includeFullMaterialization).toBe(false);
    previews++;
    const result: PromptTemplatePreview = {
      safePreview: `Сообщение Session: ${body.context?.task ?? ""}`,
      complete: true,
      diagnostics: [],
      templateRef: "continuation_template",
      templateDigest: "c".repeat(64),
      materializationDigest: "d".repeat(64),
      serviceTemplateRevision: "7",
      serviceTemplateDigest: "e".repeat(64),
      variableSnapshotDigest: "f".repeat(64),
      locale: "ru",
      effectiveCapabilities: [],
      contextPin: pin,
      slots: [{ source: "PLATFORM", slot: "RUNTIME_CHANGES", position: 0 }],
      sections: [
        {
          source: "PLATFORM",
          slot: "RUNTIME_CHANGES",
          content: "Модель обновлена",
        },
      ],
      runtimeDiff: {
        sessionRef: run(70).sessionRef,
        previousRevisionRef: "runtime_previous",
        digest: "a".repeat(64),
        changes: [
          {
            component: "MODEL",
            action: "USE_CURRENT_CONTEXT",
            previous: [{ value: "before" }],
            current: [{ value: "after" }],
          },
        ],
      },
    };
    await route.fulfill({ json: result });
  });
  await page
    .getByRole("textbox", { name: /^Задание\s/ })
    .fill("Продолжить проверку");
  await page
    .getByRole("button", {
      name: "Просмотреть сообщение продолжения",
      exact: true,
    })
    .click();
  await expect(
    page.getByText("Сообщение Session: Продолжить проверку", { exact: true }),
  ).toBeVisible();
  expect(previews).toBe(1);
  await expect(
    page.locator(".prompt-target-preview").getByRole("alert"),
  ).toHaveCount(0);
  await expect(
    page.locator('.launch-summary button[type="submit"]'),
  ).toBeEnabled();
  await page
    .getByRole("textbox", { name: /^Задание\s/ })
    .fill("Продолжить с новым вводом");
  await expect(
    page.getByText("Сообщение Session: Продолжить проверку", { exact: true }),
  ).toHaveCount(0);
  await expect(
    page.locator('.launch-summary button[type="submit"]'),
  ).toBeDisabled();
  await page
    .getByRole("button", {
      name: "Просмотреть сообщение продолжения",
      exact: true,
    })
    .click();
  await expect(
    page.getByText("Сообщение Session: Продолжить с новым вводом", {
      exact: true,
    }),
  ).toBeVisible();
  expect(previews).toBe(2);
  await expect(
    page.locator('.launch-summary button[type="submit"]'),
  ).toBeEnabled();
}
