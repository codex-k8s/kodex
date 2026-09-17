import { expect, test, type Page, type Response } from "@playwright/test";

import type {
  Agent,
  AgentPage,
  AgentRuntimeConfigurationView,
  Project,
  ProjectPage,
  SkillBundle,
  SkillBundlePage,
} from "../src/shared/api/generated/openapi/types.gen";
import { authenticateOwner } from "./auth-flow";
import { loadE2EAuthEnvironment } from "./environment";
import { createAgent, gotoWithRetry } from "./helpers";

const environment = loadE2EAuthEnvironment();
const sourceRevision = process.env.KODEX_E2E_SOURCE_REVISION ?? "";
if (!/^[a-f0-9]{40}$/.test(sourceRevision))
  throw new Error("KODEX_E2E_SOURCE_REVISION must be an exact commit SHA");
const fixtureKey =
  process.env.KODEX_E2E_SKILL_FIXTURE_KEY ?? sourceRevision.slice(0, 10);
if (!/^[a-f0-9]{10}$/.test(fixtureKey))
  throw new Error(
    "KODEX_E2E_SKILL_FIXTURE_KEY must be 10 lowercase hex digits",
  );
const projectName = "Локальная приёмка первого запуска";
const skillName = `Локальный Skill ${fixtureKey}`;
const agentName = `Локальный сотрудник ${fixtureKey}`;
const skillDescription = "Безопасная локальная проверка malware scanner.";
const skillSource = `---
name: ${skillName}
description: ${skillDescription}
---
Проверяй только явно переданные локальные данные и не выполняй внешние действия.
`;

test("Skill проходит реальный scan, review, publication и history", async ({
  page,
}) => {
  await authenticateOwner(
    page,
    {
      username: environment.ownerUsername,
      password: environment.ownerPassword,
    },
    { mode: "local" },
  );
  const project = await exactProject(page);
  let skill = await exactSkill(page, project.ref);
  if (!skill) skill = await createSkill(page, project.ref);

  await gotoWithRetry(
    page,
    `/projects/${encodeURIComponent(project.ref)}/context/skills/${encodeURIComponent(skill.ref)}`,
  );
  skill = await advanceSkill(page, skill);
  expect(skill.currentRevision).toMatchObject({
    name: skillName,
    description: skillDescription,
    state: "PUBLISHED",
    scanState: "CLEAN",
  });
  expect(skill.currentRevision?.scanEngine).toBeTruthy();
  expect(skill.currentRevision?.scanDigest).toMatch(/^[a-f0-9]{64}$/);

  await page.getByRole("button", { name: "История", exact: true }).click();
  const history = page.getByRole("dialog", { name: "История", exact: true });
  await expect(history).toBeVisible();
  await expect(history.locator("details")).toHaveCount(1);
  await history.locator("summary").click();
  await expect(history.locator('[data-state="PUBLISHED"]')).toBeVisible();
  await expect(history.locator('[data-state="CLEAN"]')).toBeVisible();
  await history.getByRole("button", { name: "Закрыть", exact: true }).click();

  const agent = await exactAgent(page, project.ref);
  await gotoWithRetry(
    page,
    `/projects/${encodeURIComponent(project.ref)}/context/skills/${encodeURIComponent(skill.ref)}`,
  );
  await bindSkill(page, agent, skill);
});

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

async function exactSkill(
  page: Page,
  projectRef: string,
): Promise<SkillBundle | undefined> {
  return await page.evaluate(
    async ({ name, project }) => {
      const query = new URLSearchParams({
        projectRef: project,
        query: name,
        pageSize: "30",
      });
      const response = await fetch(`/api/v1/skill-bundles?${query}`);
      if (!response.ok) throw new Error("Skill catalog read failed");
      const result = (await response.json()) as SkillBundlePage;
      const matches = result.items.filter(
        (item) =>
          item.projectRef === project &&
          (item.draftRevision?.name ?? item.currentRevision?.name) === name,
      );
      if (matches.length > 1) throw new Error("Skill fixture is not unique");
      return matches[0];
    },
    { name: skillName, project: projectRef },
  );
}

async function exactAgent(page: Page, projectRef: string): Promise<Agent> {
  const existing = await page.evaluate(
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
      if (matches.length > 1) throw new Error("Agent fixture is not unique");
      return matches[0];
    },
    { name: agentName, project: projectRef },
  );
  if (existing) return existing;
  const ref = await createAgent(page, projectRef, {
    name: agentName,
    purpose: "Проверять локальный Skill без запуска модели.",
    role: "Локальный сотрудник для безопасной приёмки контекста.",
    instructions:
      "Используй только явно привязанный Skill и не выполняй внешние действия.",
  });
  return await page.evaluate(async (agentRef) => {
    const response = await fetch(
      `/api/v1/agents/${encodeURIComponent(agentRef)}`,
    );
    if (!response.ok) throw new Error("Created agent readback failed");
    return (await response.json()) as Agent;
  }, ref);
}

async function bindSkill(
  page: Page,
  agent: Agent,
  skill: SkillBundle,
): Promise<void> {
  const revision = skill.currentRevision;
  if (!revision) throw new Error("Published Skill revision is unavailable");
  let configuration = await readAgentConfiguration(page, agent.ref);
  let binding = configuration.skillBindings.find(
    (item) => item.resourceRef === skill.ref,
  );
  if (!binding) {
    await page
      .getByRole("button", {
        name: "Привязка к ИИ-сотруднику",
        exact: true,
      })
      .click();
    const picker = page.getByRole("dialog", {
      name: "Привязка к ИИ-сотруднику",
      exact: true,
    });
    await picker
      .getByRole("option", { name: new RegExp(`^${escapeRegExp(agentName)}`) })
      .click();
    const response = page.waitForResponse(
      (candidate) =>
        candidate.request().method() === "PUT" &&
        new URL(candidate.url()).pathname ===
          `/api/v1/agents/${agent.ref}/skill-bundles/${skill.ref}`,
    );
    await page
      .getByRole("button", { name: "Привязать ревизию", exact: true })
      .click();
    expect((await response).status()).toBe(200);
    configuration = await readAgentConfiguration(page, agent.ref);
    binding = configuration.skillBindings.find(
      (item) => item.resourceRef === skill.ref,
    );
  }
  expect(binding).toMatchObject({
    agentRef: agent.ref,
    resourceRef: skill.ref,
    revisionRef: revision.ref,
    digest: revision.digest,
  });
  expect(binding?.version).toBeGreaterThan(0);
}

async function readAgentConfiguration(
  page: Page,
  agentRef: string,
): Promise<AgentRuntimeConfigurationView> {
  return await page.evaluate(async (ref) => {
    const response = await fetch(
      `/api/v1/agents/${encodeURIComponent(ref)}/runtime-configuration`,
    );
    if (!response.ok)
      throw new Error("Agent runtime configuration read failed");
    return (await response.json()) as AgentRuntimeConfigurationView;
  }, agentRef);
}

async function createSkill(
  page: Page,
  projectRef: string,
): Promise<SkillBundle> {
  await gotoWithRetry(
    page,
    `/projects/${encodeURIComponent(projectRef)}/context/skills/new`,
  );
  await page.getByLabel("Название", { exact: true }).fill(skillName);
  await page.getByLabel(/^Описание/).fill(skillDescription);
  await page.getByRole("button", { name: "Импорт Skill", exact: true }).click();
  const importer = page.getByRole("dialog", {
    name: "Импорт Skill",
    exact: true,
  });
  await importer.getByRole("button", { name: "SKILL.md", exact: true }).click();
  await importer
    .getByRole("textbox", { name: "SKILL.md", exact: true })
    .fill(skillSource);
  await importer
    .getByRole("button", { name: "Добавить SKILL.md", exact: true })
    .click();
  const upload = page.waitForResponse(
    (response) =>
      response.request().method() === "POST" &&
      new URL(response.url()).pathname ===
        `/api/v1/projects/${projectRef}/artifacts`,
  );
  await importer
    .getByRole("button", { name: "Загрузить", exact: true })
    .click();
  expect((await upload).status()).toBe(201);
  for (let attempt = 0; attempt < 20; attempt += 1) {
    if (await importer.locator('[data-state="CLEAN"]').isVisible()) break;
    const refresh = page.waitForResponse(
      (response) =>
        response.request().method() === "GET" &&
        /^\/api\/v1\/artifacts\/[^/]+$/.test(new URL(response.url()).pathname),
    );
    await importer
      .getByRole("button", { name: "Повторить", exact: true })
      .click();
    expect((await refresh).status()).toBe(200);
    if (attempt === 19)
      throw new Error("Skill artifact did not reach CLEAN state");
  }
  await importer
    .getByRole("button", { name: "Добавить в manifest", exact: true })
    .click();
  const save = page.waitForResponse(
    (response) =>
      response.request().method() === "POST" &&
      new URL(response.url()).pathname ===
        `/api/v1/projects/${projectRef}/skill-bundle-drafts`,
  );
  await page.getByRole("button", { name: "Сохранить", exact: true }).click();
  const response = await save;
  expect(response.status()).toBe(201);
  const skill = (await response.json()) as SkillBundle;
  expect(skill.projectRef).toBe(projectRef);
  await expect(page).toHaveURL(
    new RegExp(`/context/skills/${escapeRegExp(skill.ref)}$`),
  );
  return skill;
}

async function advanceSkill(
  page: Page,
  initial: SkillBundle,
): Promise<SkillBundle> {
  let skill = await readSkill(page, initial.ref);
  if (
    ["DRAFT", "INVALID", "REJECTED"].includes(skill.draftRevision?.state ?? "")
  ) {
    const response = waitForRevisionAction(page, skill.ref, "validation");
    await page.getByRole("button", { name: "Проверить", exact: true }).click();
    expect((await response).status()).toBe(200);
    skill = await readSkill(page, skill.ref);
  }
  if (skill.draftRevision?.state === "VALIDATED") {
    await page
      .getByRole("button", { name: "Рассмотреть", exact: true })
      .click();
    const review = page.getByRole("dialog", {
      name: "Рассмотреть",
      exact: true,
    });
    await review
      .getByLabel("Комментарий", { exact: true })
      .fill("Локальный scanner и manifest проверены.");
    const response = waitForRevisionAction(page, skill.ref, "review");
    await review
      .getByRole("button", { name: "Рассмотреть", exact: true })
      .click();
    expect((await response).status()).toBe(200);
    skill = await readSkill(page, skill.ref);
  }
  if (skill.draftRevision?.state === "APPROVED") {
    const response = waitForRevisionAction(page, skill.ref, "publication");
    await page
      .getByRole("button", { name: "Опубликовать", exact: true })
      .click();
    expect((await response).status()).toBe(200);
    skill = await readSkill(page, skill.ref);
  }
  return skill;
}

async function readSkill(page: Page, ref: string): Promise<SkillBundle> {
  return await page.evaluate(async (skillRef) => {
    const response = await fetch(
      `/api/v1/skill-bundles/${encodeURIComponent(skillRef)}`,
    );
    if (!response.ok) throw new Error("Skill readback failed");
    return (await response.json()) as SkillBundle;
  }, ref);
}

function waitForRevisionAction(
  page: Page,
  skillRef: string,
  action: "publication" | "review" | "validation",
): Promise<Response> {
  return page.waitForResponse(
    (response) =>
      response.request().method() === "POST" &&
      new URL(response.url()).pathname.startsWith(
        `/api/v1/skill-bundles/${skillRef}/revisions/`,
      ) &&
      new URL(response.url()).pathname.endsWith(`/${action}`),
  );
}

function escapeRegExp(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}
