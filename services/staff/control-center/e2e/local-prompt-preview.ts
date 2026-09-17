import { expect, test, type Page } from "@playwright/test";

import type {
  Agent,
  AgentPage,
  Project,
  ProjectPage,
  PromptTemplatePreview,
} from "../src/shared/api/generated/openapi/types.gen";
import { authenticateOwner } from "./auth-flow";
import { loadE2EAuthEnvironment } from "./environment";
import { gotoWithRetry } from "./helpers";

const environment = loadE2EAuthEnvironment();
const projectName = "Локальная приёмка первого запуска";
const agentName =
  process.env.KODEX_E2E_PROMPT_AGENT_NAME ?? "Локальный сотрудник 1bf9febe31";

type PromptProblem = {
  readonly status?: number;
  readonly code?: string;
  readonly retryable?: boolean;
  readonly diagnostics?: ReadonlyArray<{
    readonly severity?: string;
    readonly code?: string;
    readonly line?: number;
    readonly column?: number;
    readonly variableName?: string;
  }>;
};

test("Материализованный preview показывает pin и безопасную ошибку шаблона", async ({
  page,
}) => {
  const serverFailures: string[] = [];
  page.on("response", (response) => {
    if (response.status() >= 500)
      serverFailures.push(
        `${String(response.status())} ${new URL(response.url()).pathname}`,
      );
  });
  await authenticateOwner(
    page,
    {
      username: environment.ownerUsername,
      password: environment.ownerPassword,
    },
    { mode: "local" },
  );
  const project = await exactProject(page);
  const agent = await exactAgent(page, project.ref);
  await gotoWithRetry(
    page,
    `/projects/${encodeURIComponent(project.ref)}/agents/${encodeURIComponent(agent.ref)}`,
  );
  await page.getByRole("tab", { name: "Инструкции", exact: true }).click();
  const panel = page.locator("#agent-panel-instructions");
  const editor = panel.getByRole("textbox", {
    name: "Инструкции",
    exact: true,
  });
  await expect(editor).not.toHaveText("");

  const successfulPreview = page.waitForResponse(isPromptPreviewResponse);
  await panel
    .getByRole("button", { name: "Проверка подстановки", exact: true })
    .click();
  const successfulResponse = await successfulPreview;
  expect(successfulResponse.status()).toBe(200);
  const preview = (await successfulResponse.json()) as PromptTemplatePreview;
  expect(preview.complete).toBe(true);
  expect(preview.contextPin).toMatchObject({
    agentRef: agent.ref,
    agentVersion: agent.version,
  });
  expect(preview.contextPin?.digest).toMatch(/^[a-f0-9]{64}$/);
  expect(preview.materializationDigest).toMatch(/^[a-f0-9]{64}$/);
  expect(preview.fullMaterializedPrompt).toBeUndefined();
  await expect(
    panel.locator(".instructions-panel__preview > .safe-markdown"),
  ).toBeVisible();
  await panel
    .locator(".prompt-context-details")
    .getByText("Версии проверенного контекста", { exact: true })
    .click();
  await expect(
    panel
      .locator(".prompt-context-details")
      .getByText(preview.contextPin?.digest ?? "", { exact: true }),
  ).toBeVisible();

  await panel.getByRole("button", { name: "Редактор", exact: true }).click();
  await editor.fill("{{.unknown.value}}");
  const invalidPreview = page.waitForResponse(isPromptPreviewResponse);
  await panel
    .getByRole("button", { name: "Проверка подстановки", exact: true })
    .click();
  const invalidResponse = await invalidPreview;
  expect(invalidResponse.status()).toBeGreaterThanOrEqual(400);
  expect(invalidResponse.status()).toBeLessThan(500);
  const problem = (await invalidResponse.json()) as PromptProblem;
  expect(problem.code).toBe("PROMPT_TEMPLATE_INVALID");
  expect(problem.retryable).toBe(false);
  expect(problem.diagnostics).toEqual([
    expect.objectContaining({
      severity: "ERROR",
      code: "PROMPT_TEMPLATE_VARIABLE_UNKNOWN",
      line: expect.any(Number),
      column: expect.any(Number),
      variableName: "unknown.value",
    }),
  ]);
  await expect(panel.getByRole("alert")).toContainText(
    "Неизвестная переменная шаблона",
  );

  await panel.getByRole("button", { name: "Редактор", exact: true }).click();
  await editor.fill("{{range .agent.name}}value{{end}}");
  const wrongTypePreview = page.waitForResponse(isPromptPreviewResponse);
  await panel
    .getByRole("button", { name: "Проверка подстановки", exact: true })
    .click();
  const wrongTypeResponse = await wrongTypePreview;
  expect(wrongTypeResponse.status()).toBe(400);
  const wrongTypeProblem = (await wrongTypeResponse.json()) as PromptProblem;
  expect(wrongTypeProblem).toMatchObject({
    code: "PROMPT_TEMPLATE_INVALID",
    retryable: false,
  });
  expect(wrongTypeProblem.diagnostics).toEqual([
    expect.objectContaining({
      code: "PROMPT_TEMPLATE_EXECUTION_INVALID",
      variableName: "agent.name",
      line: 1,
      column: expect.any(Number),
    }),
  ]);
  await expect(panel.getByRole("alert")).toContainText(
    "Тип переменной несовместим с операцией шаблона",
  );

  await panel.getByRole("button", { name: "Редактор", exact: true }).click();
  await editor.fill("Простой текст без переменных.");
  const simplePreview = page.waitForResponse(isPromptPreviewResponse);
  await panel
    .getByRole("button", { name: "Проверка подстановки", exact: true })
    .click();
  const simpleResponse = await simplePreview;
  const simpleBody = (await simpleResponse.json()) as PromptTemplatePreview;
  expect(simpleResponse.status()).toBe(200);
  expect(simpleBody.complete).toBe(true);
  expect(simpleBody.contextPin).toMatchObject({
    agentRef: agent.ref,
    agentVersion: agent.version,
  });
  await expect(
    panel.locator(".instructions-panel__preview > .safe-markdown"),
  ).toBeVisible();

  await panel.getByRole("button", { name: "Редактор", exact: true }).click();
  await editor.fill(
    "{{range .input.files}}{{.name}}{{end}} {{range .runtime.environment.tools}}{{.name}}{{end}}",
  );
  const unavailableRangePreview = page.waitForResponse(isPromptPreviewResponse);
  await panel
    .getByRole("button", { name: "Проверка подстановки", exact: true })
    .click();
  const unavailableRangeResponse = await unavailableRangePreview;
  expect(unavailableRangeResponse.status()).toBe(400);
  const unavailableRangeProblem =
    (await unavailableRangeResponse.json()) as PromptProblem;
  expect(unavailableRangeProblem).toMatchObject({
    code: "PROMPT_TEMPLATE_INVALID",
    retryable: false,
  });
  expect(unavailableRangeProblem.diagnostics).toEqual([
    expect.objectContaining({
      severity: "ERROR",
      code: "CAPABILITY_REQUIRED",
      variableName: "input.files",
      line: 1,
      column: 1,
    }),
  ]);
  await expect(panel.getByRole("alert")).toContainText(
    "Для переменной сотруднику нужно назначить соответствующую возможность",
  );
  expect(serverFailures).toEqual([]);
});

function isPromptPreviewResponse(response: {
  request(): { method(): string };
  url(): string;
}): boolean {
  return (
    response.request().method() === "POST" &&
    new URL(response.url()).pathname === "/api/v1/prompt-templates/preview"
  );
}

async function exactProject(page: Page): Promise<Project> {
  return await page.evaluate(async (name) => {
    const response = await fetch(
      `/api/v1/projects?query=${encodeURIComponent(name)}&pageSize=30`,
    );
    if (!response.ok) throw new Error("Acceptance project read failed");
    const result = (await response.json()) as ProjectPage;
    const matches = result.items.filter((item) => item.name === name);
    if (matches.length !== 1)
      throw new Error("Acceptance project is not unique");
    return matches[0] as Project;
  }, projectName);
}

async function exactAgent(page: Page, projectRef: string): Promise<Agent> {
  return await page.evaluate(
    async ({ name, project }) => {
      const query = new URLSearchParams({
        projectRef: project,
        query: name,
        pageSize: "40",
      });
      const response = await fetch(`/api/v1/agents?${query}`);
      if (!response.ok) throw new Error("Agent catalog read failed");
      const result = (await response.json()) as AgentPage;
      const matches = result.items.filter(
        (item) =>
          item.projectRef === project && !item.system && item.name === name,
      );
      if (matches.length !== 1)
        throw new Error("Prompt preview agent fixture is not unique");
      return matches[0] as Agent;
    },
    { name: agentName, project: projectRef },
  );
}
