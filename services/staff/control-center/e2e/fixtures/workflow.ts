import { expect, type Page, type Route } from "@playwright/test";
import {
  deniedLaunch,
  effectiveFiles,
  effectivePage,
} from "./effective-capabilities";
import type {
  Agent,
  BootstrapState,
  Workflow,
  WorkflowInput,
  PromptVariableCatalogInput,
  PromptTemplatePreviewInput,
  PromptTemplatePreview,
  PromptContextPin,
} from "../../src/shared/api/generated/openapi/types.gen";

export async function checkWorkflowEditor(
  page: Page,
  projectRef: string,
  speechBootstrap?: BootstrapState,
  captureUnpublished?: () => Promise<void>,
) {
  let releaseSave: (() => void) | undefined;
  const saveBarrier = speechBootstrap
    ? new Promise<void>((resolve) => {
        releaseSave = resolve;
      })
    : Promise.resolve();
  const bootstrapRoute = async (route: Route) => {
    await route.fulfill({
      headers: { ETag: '"1"' },
      json: {
        ...speechBootstrap,
        speechTranscription: {
          available: true,
          reason: "READY",
          validUntil: new Date(
            (await page.evaluate(() => Date.now())) + 30_000,
          ).toISOString(),
        },
      },
    });
  };
  if (speechBootstrap) {
    await page
      .context()
      .grantPermissions(["microphone"], { origin: "https://kodex.test" });
    await page.route("**/api/v1/bootstrap", bootstrapRoute);
  }
  let transcriptions = 0;
  const transcriptionRoute = async (route: Route) => {
    transcriptions += 1;
    await route.fulfill({ json: { text: "Unexpected transcription" } });
  };
  if (speechBootstrap)
    await page.route("**/api/v1/speech/transcriptions", transcriptionRoute);
  const agent: Agent = {
    ref: "agent_workflow_synthetic",
    version: 1,
    projectRef,
    name: "Аналитик synthetic",
    purpose: "Проверка процесса",
    roleDescription: "Аналитик",
    state: "READY",
    enabled: true,
    system: false,
    runtimeRef: "runtime_synthetic",
    runtimeName: "Synthetic",
    runtimeReady: true,
    capabilities: [],
    integrations: [],
    knowledgeArtifactRefs: [],
    nextActions: [],
    updatedAt: "2026-09-05T00:00:00Z",
  };
  let workflow: Workflow = {
    ref: "workflow_synthetic",
    version: 1,
    projectRef,
    name: "Процесс synthetic",
    purpose: "Согласование документа",
    state: "PUBLISHED",
    cardSummary: {
      stageCount: 0,
      uniqueAgentCount: 0,
      parallelGroupCount: 0,
      hasHumanGate: false,
      activeRunCount: 0,
      pendingGateCount: 0,
    },
    revisionRef: "workflow_published",
    publishedRevisionRef: "workflow_published",
    launchReadiness: {
      allowedToSubmit: true,
      reason: "READY",
      workflowVersion: 1,
      revisionRef: "workflow_published",
      operationalState: "UNKNOWN",
      contextDigest: "a".repeat(64),
    },
    draftRevisionRef: "workflow_draft",
    coordinatorAgentRef: agent.ref,
    inputFields: [],
    steps: [
      {
        ref: "step_synthetic",
        position: 1,
        name: "Анализ",
        purpose: "Проверить документ",
        agentRef: agent.ref,
        parallel: false,
        parallelGroup: 0,
        timeoutSeconds: 1800,
        expectedResult: "Заключение",
        humanGate: false,
        gateDecisions: [],
        requiredCapabilityKeys: [],
      },
    ],
    validationMessages: [],
    nextActions: ["EDIT", "VALIDATE"],
    updatedAt: "2026-09-05T00:00:00Z",
  };
  let saves = 0;
  workflow.draft = {
    ref: "workflow_draft",
    version: 1,
    revision: 2,
    state: "DRAFT",
    coordinatorAgentRef: agent.ref,
    inputFields: [],
    steps: workflow.steps.map((step) => ({
      ...step,
      ref: "draft_stage",
      purpose: "Назначение черновика",
    })),
    validationMessages: [],
  };
  let previews = 0;
  const contextPin = (): PromptContextPin => ({
    digest: String(workflow.version).padStart(64, "a"),
    workflowRef: workflow.ref,
    workflowVersion: workflow.version,
    workflowRevisionRef: workflow.draft?.ref ?? workflow.revisionRef,
    workflowStageKey: (workflow.draft ?? workflow).steps[0]?.ref,
    agentRef: agent.ref,
    agentVersion: agent.version,
  });
  await page.route(
    "**/api/v1/prompt-templates/catalog/query",
    async (route) => {
      const body = route.request().postDataJSON() as PromptVariableCatalogInput;
      expect(body.targetKind).toBe("WORKFLOW_STAGE");
      expect(body.targetRef).toBe(workflow.ref);
      expect(body.context?.workflowRevisionRef).toBe(
        contextPin().workflowRevisionRef,
      );
      expect(body.context?.workflowStageKey).toBe(
        contextPin().workflowStageKey,
      );
      expect(body.context?.expectedWorkflowVersion).toBe(workflow.version);
      await route.fulfill({
        json: { items: [], total: 0, contextPin: contextPin() },
      });
    },
  );
  await page.route("**/api/v1/prompt-templates/preview", async (route) => {
    const body = route.request().postDataJSON() as PromptTemplatePreviewInput;
    expect(body.template).toBe("");
    expect(body.targetKind).toBe("WORKFLOW_STAGE");
    expect(body.targetRef).toBe(workflow.ref);
    expect(body.expectedContextDigest).toBe(contextPin().digest);
    expect(body.context?.workflowRevisionRef).toBe(
      contextPin().workflowRevisionRef,
    );
    expect(body.includeFullMaterialization).toBe(false);
    previews++;
    const preview: PromptTemplatePreview = {
      safePreview: `Точный сохранённый этап: ${(workflow.draft ?? workflow).steps[0]?.purpose ?? ""}`,
      diagnostics: [],
      complete: true,
      templateRef: "template_synthetic",
      templateDigest: "b".repeat(64),
      materializationDigest: "c".repeat(64),
      serviceTemplateRevision: "7",
      serviceTemplateDigest: "d".repeat(64),
      variableSnapshotDigest: "e".repeat(64),
      locale: "ru",
      slots: [{ slot: "PURPOSE", source: "PLATFORM", position: 0 }],
      sections: [
        {
          slot: "PURPOSE",
          source: "PLATFORM",
          content: (workflow.draft ?? workflow).steps[0]?.purpose ?? "",
        },
      ],
      effectiveCapabilities: [],
      contextPin: contextPin(),
    };
    await route.fulfill({ json: preview });
  });
  const failures: string[] = [];
  await page.route("**/api/v1/platform-capabilities", (route) =>
    route.fulfill({ json: { items: [] } }),
  );
  await page.route(
    `**/api/v1/agents/${agent.ref}/effective-capabilities*`,
    (route) => {
      const url = new URL(route.request().url());
      const published = url.searchParams.has("workflowRef");
      if (published) {
        expect(url.searchParams.get("workflowRef")).toBe(workflow.ref);
        expect(url.searchParams.get("stepKey")).toBe(workflow.steps[0]?.ref);
      }
      return route.fulfill({
        json: {
          ...effectivePage(agent, [
            { ...effectiveFiles, required: published },
            deniedLaunch,
          ]),
          ...(published
            ? {
                workflowRef: workflow.ref,
                stepKey: workflow.steps[0]?.ref,
                workflowVersionRef: "workflow_published_version",
              }
            : {}),
        },
      });
    },
  );
  await page.route(`**/api/v1/projects/${projectRef}/agents*`, (route) =>
    route.fulfill({ json: { items: [agent], nextPageToken: "" } }),
  );
  await page.route("**/api/v1/workflows/workflow_synthetic", async (route) => {
    if (route.request().method() === "PATCH") {
      saves += 1;
      await saveBarrier;
      if (
        route.request().headers()["if-match"] !==
          `"${String(workflow.version)}"` ||
        !route.request().headers()["idempotency-key"]
      )
        failures.push("Invalid workflow mutation protection");
      const input = route.request().postDataJSON() as WorkflowInput;
      if (!input.steps)
        throw new Error("Missing workflow steps in synthetic save");
      workflow = {
        ...workflow,
        state: "DRAFT",
        launchReadiness: {
          ...workflow.launchReadiness,
          workflowVersion: workflow.version + 1,
          allowedToSubmit: false,
          reason: "UNPUBLISHED",
        },
        name: input.name,
        purpose: input.purpose,
        draftRevisionRef: "workflow_draft_saved",
        draft: {
          ref: "workflow_draft_saved",
          version: 2,
          revision: 3,
          state: "DRAFT",
          coordinatorAgentRef: input.coordinatorAgentRef,
          inputFields: [],
          validationMessages: [],
          steps: input.steps.map((step, index) => ({
            ...step,
            ref: `draft_stage_${String(index)}`,
          })),
        },
        version: workflow.version + 1,
      };
    } else if (route.request().method() !== "GET")
      failures.push("Unexpected workflow method");
    await route.fulfill({ json: workflow });
  });
  await page.goto(`/projects/${projectRef}/workflows/${workflow.ref}`);
  await page.getByRole("button", { name: "Координатор", exact: true }).click();
  await page.getByRole("option", { name: /Аналитик synthetic/ }).click();
  await page.getByRole("button", { name: "Исполнитель", exact: true }).click();
  await page.getByRole("option", { name: /Аналитик synthetic/ }).click();
  await page.getByLabel("Название", { exact: true }).fill("Изменённый процесс");
  await expect(
    page.getByRole("button", { name: "Запустить", exact: true }),
  ).toBeDisabled();
  const step = page.locator(".workflow-step").first();
  const instructions = step.locator(".cm-content").first();
  await instructions.fill("  Synthetic instructions  ");
  await instructions.press("End");
  await instructions.press("Tab");
  const exactInstructions = await instructions.innerText();
  expect(exactInstructions).toContain("  Synthetic instructions  ");
  await step.locator(".step-advanced > summary").click();
  await step.getByRole("checkbox", { name: /^Файлы/ }).check();
  await expect(
    step.getByRole("checkbox", { name: /^Запуск задач/ }),
  ).toBeDisabled();
  const expectedResult = step.locator(".cm-content").nth(1);
  await expectedResult.fill("  Synthetic result  ");
  await expect(
    page.getByRole("button", { name: "Проверить Процесс", exact: true }),
  ).toBeDisabled();
  page.once("dialog", (dialog) => dialog.dismiss());
  await page.getByRole("link", { name: "Kodex", exact: true }).click();
  await expect(page).toHaveURL(new RegExp(`/workflows/${workflow.ref}$`));
  await expect(page.getByLabel("Название", { exact: true })).toHaveValue(
    "Изменённый процесс",
  );
  const voice = step.locator(".shared-code-editor .voice-input").first();
  if (speechBootstrap) {
    await voice.locator("button").click();
    await expect(voice).toHaveAttribute("data-state", "recording");
  }
  try {
    await page.getByRole("button", { name: "Сохранить", exact: true }).click();
    if (speechBootstrap) {
      await expect.poll(() => saves).toBe(1);
      await expect(
        page.locator(".workflow-editor .voice-input button"),
      ).toHaveCount(0);
      await expect(voice).toHaveAttribute("data-state", "idle");
      await expect(instructions).toHaveAttribute("contenteditable", "false");
      expect(transcriptions).toBe(0);
    }
  } finally {
    releaseSave?.();
  }
  await expect(
    page.getByRole("button", { name: "Проверить Процесс", exact: true }),
  ).toBeEnabled();
  await expect(
    page.getByRole("button", { name: "Запустить", exact: true }),
  ).toBeDisabled();
  await expect(page.locator("#workflow-launch-explanation")).toHaveText(
    "Опубликуйте изменения процесса перед запуском.",
  );
  await page.locator("#workflow-launch-explanation").scrollIntoViewIfNeeded();
  await captureUnpublished?.();
  await expect(page.locator(".prompt-target-preview").first()).toBeVisible();
  await expect(
    page.locator(".prompt-target-preview").getByRole("alert"),
  ).toHaveCount(0);
  expect(saves).toBe(1);
  expect(workflow.draft.steps[0]?.purpose).toBe(exactInstructions);
  expect(workflow.draft.steps[0]?.expectedResult).toBe("  Synthetic result  ");
  expect(workflow.steps[0]?.purpose).toBe("Проверить документ");
  await expect(instructions).toContainText("Synthetic instructions");
  await page
    .getByRole("button", {
      name: "Просмотреть контекст исполнения",
      exact: true,
    })
    .click();
  await expect(
    page.getByText(/Точный сохранённый этап:.*Synthetic instructions/),
  ).toBeVisible();
  expect(previews).toBe(1);
  if (speechBootstrap) {
    await expect(voice.locator("button")).toHaveCount(1);
    await expect(instructions).toHaveAttribute("contenteditable", "true");
    expect(transcriptions).toBe(0);
    await page.unroute("**/api/v1/bootstrap", bootstrapRoute);
    await page.unroute("**/api/v1/speech/transcriptions", transcriptionRoute);
  }
  expect(failures).toEqual([]);
  expect(workflow.draft.steps[0]?.requiredCapabilityKeys).toEqual([
    effectiveFiles.key,
  ]);
  workflow.state = "PUBLISHED";
  workflow = {
    ...workflow,
    ...workflow.draft,
    ref: workflow.ref,
    version: workflow.version + 1,
    state: "PUBLISHED",
    revisionRef: workflow.draft.ref,
    publishedRevisionRef: workflow.draft.ref,
    launchReadiness: {
      ...workflow.launchReadiness,
      workflowVersion: workflow.version + 1,
      revisionRef: workflow.draft.ref,
      allowedToSubmit: true,
      reason: "READY",
      operationalState: "UNKNOWN",
    },
    draft: undefined,
    draftRevisionRef: undefined,
  };
  await page.goto(`/projects/${projectRef}/workflows/${workflow.ref}`);
  await page.locator(".step-advanced > summary").first().click();
  await page
    .getByRole("button", {
      name: "Возможности опубликованного этапа",
      exact: true,
    })
    .click();
  await expect(
    page.getByText("Требуется этапом", { exact: true }),
  ).toBeVisible();
  await page.evaluate(() => window.scrollTo(0, 0));
  await expect.poll(() => page.evaluate(() => window.scrollY)).toBe(0);
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBe(true);
}
