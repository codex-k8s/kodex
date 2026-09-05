import { expect, type Page, type Route } from "@playwright/test";
import type {
  Agent,
  BootstrapState,
  Workflow,
  WorkflowInput,
} from "../../src/shared/api/generated/openapi/types.gen";

export async function checkWorkflowEditor(
  page: Page,
  projectRef: string,
  speechBootstrap?: BootstrapState,
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
    state: "DRAFT",
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
  const failures: string[] = [];
  await page.route("**/api/v1/platform-capabilities", (route) =>
    route.fulfill({ json: { items: [] } }),
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
        name: input.name,
        purpose: input.purpose,
        coordinatorAgentRef: input.coordinatorAgentRef,
        steps: input.steps.map((step, index) => ({
          ...step,
          ref: workflow.steps[index]?.ref ?? `step_${String(index)}`,
        })),
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
  const step = page.locator(".workflow-step").first();
  const instructions = step.locator(".cm-content").first();
  await instructions.fill("  Synthetic instructions  ");
  await instructions.press("End");
  await instructions.press("Tab");
  const exactInstructions = await instructions.innerText();
  expect(exactInstructions).toContain("  Synthetic instructions  ");
  await step.locator(".step-advanced summary").click();
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
  expect(saves).toBe(1);
  expect(workflow.steps[0]?.purpose).toBe(exactInstructions);
  expect(workflow.steps[0]?.expectedResult).toBe("  Synthetic result  ");
  if (speechBootstrap) {
    await expect(voice.locator("button")).toHaveCount(1);
    await expect(instructions).toHaveAttribute("contenteditable", "true");
    expect(transcriptions).toBe(0);
    await page.unroute("**/api/v1/bootstrap", bootstrapRoute);
    await page.unroute("**/api/v1/speech/transcriptions", transcriptionRoute);
  }
  expect(failures).toEqual([]);
  await page.evaluate(() => window.scrollTo(0, 0));
  await expect.poll(() => page.evaluate(() => window.scrollY)).toBe(0);
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBe(true);
}
