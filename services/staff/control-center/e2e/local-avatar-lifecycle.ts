import { expect, test, type Page } from "@playwright/test";
import { fileURLToPath } from "node:url";

import type {
  Agent,
  AgentPage,
  Problem,
  Project,
  ProjectPage,
} from "../src/shared/api/generated/openapi/types.gen";
import { authenticateOwner } from "./auth-flow";
import { loadE2EAuthEnvironment } from "./environment";
import { gotoWithRetry } from "./helpers";

const environment = loadE2EAuthEnvironment();
const sourceRevision = process.env.KODEX_E2E_SOURCE_REVISION ?? "";
if (!/^[a-f0-9]{40}$/.test(sourceRevision))
  throw new Error("KODEX_E2E_SOURCE_REVISION must be an exact commit SHA");
const fixtureKey =
  process.env.KODEX_E2E_AVATAR_FIXTURE_KEY ?? sourceRevision.slice(0, 10);
if (!/^[a-f0-9]{10}$/.test(fixtureKey))
  throw new Error(
    "KODEX_E2E_AVATAR_FIXTURE_KEY must be 10 lowercase hex digits",
  );
const projectName = "Локальная приёмка первого запуска";
const agentName = `Локальный сотрудник ${fixtureKey}`;
const logoPath = fileURLToPath(new URL("../public/logo.png", import.meta.url));
const tinyPNG = Buffer.from(
  "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9WlD8SoAAAAASUVORK5CYII=",
  "base64",
);

test("Avatar отклоняет тип, загружается, заменяется и удаляется", async ({
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
  let agent = await exactAgent(page, project.ref);
  await gotoWithRetry(
    page,
    `/projects/${encodeURIComponent(project.ref)}/agents/${encodeURIComponent(agent.ref)}`,
  );
  await expect(
    page.getByRole("heading", { name: agentName, level: 1 }),
  ).toBeVisible();

  let avatarMutations = 0;
  page.on("request", (request) => {
    const path = new URL(request.url()).pathname;
    if (
      path.endsWith(`/agents/${agent.ref}/avatar`) &&
      ["DELETE", "PUT"].includes(request.method())
    )
      avatarMutations += 1;
  });
  const beforeInvalid = avatarMutations;
  await page
    .getByLabel("Загрузить изображение", { exact: true })
    .setInputFiles({
      name: "avatar.svg",
      mimeType: "image/svg+xml",
      buffer: Buffer.from('<svg xmlns="http://www.w3.org/2000/svg"/>', "utf8"),
    });
  await expect(
    page.getByRole("alert").filter({
      hasText: "Выберите JPEG, PNG или WebP размером не более 10 МБ.",
    }),
  ).toBeVisible();
  expect(avatarMutations).toBe(beforeInvalid);

  agent = await uploadAvatar(page, project.ref, agent.ref, {
    name: "avatar-tiny.png",
    mimeType: "image/png",
    buffer: tinyPNG,
  });
  const firstArtifactRef = exactAvatarArtifactRef(agent);

  agent = await uploadAvatar(page, project.ref, agent.ref, logoPath);
  const secondArtifactRef = exactAvatarArtifactRef(agent);
  expect(secondArtifactRef).not.toBe(firstArtifactRef);
  expect(agent.avatar?.artifactRevision).toBeGreaterThan(0);
  expect(
    await page.evaluate(async (contentPath) => {
      const response = await fetch(contentPath);
      return {
        status: response.status,
        contentType: response.headers.get("content-type"),
      };
    }, agent.avatar?.contentPath ?? ""),
  ).toEqual({ status: 200, contentType: "image/png" });

  const removal = page.waitForResponse(
    (response) =>
      response.request().method() === "DELETE" &&
      new URL(response.url()).pathname === `/api/v1/agents/${agent.ref}/avatar`,
  );
  await page
    .getByRole("button", { name: "Удалить аватар", exact: true })
    .click();
  const dialog = page.getByRole("dialog", {
    name: "Удалить аватар?",
    exact: true,
  });
  await dialog
    .getByRole("button", { name: "Удалить аватар", exact: true })
    .click();
  const removalResponse = await removal;
  expect(removalResponse.status()).toBe(200);
  agent = (await removalResponse.json()) as Agent;
  expect(agent.avatar).toMatchObject({ source: "FALLBACK" });
  expect(agent.avatar?.artifactRef).toBeUndefined();
  await expect(
    page.getByText("Используются инициалы", { exact: true }),
  ).toBeVisible();
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
        throw new Error("Avatar agent fixture is not unique");
      return matches[0] as Agent;
    },
    { name: agentName, project: projectRef },
  );
}

async function uploadAvatar(
  page: Page,
  projectRef: string,
  agentRef: string,
  file:
    | string
    | {
        readonly name: string;
        readonly mimeType: string;
        readonly buffer: Buffer;
      },
): Promise<Agent> {
  await page
    .getByLabel("Загрузить изображение", { exact: true })
    .setInputFiles(file);
  const dialog = page.getByRole("dialog", {
    name: "Настроить аватар",
    exact: true,
  });
  await expect(dialog).toBeVisible();
  const upload = page.waitForResponse(
    (response) =>
      response.request().method() === "PUT" &&
      new URL(response.url()).pathname ===
        `/api/v1/projects/${projectRef}/agents/${agentRef}/avatar`,
  );
  await dialog
    .getByRole("button", { name: "Обрезать и загрузить", exact: true })
    .click();
  const response = await upload;
  if (response.status() !== 200) {
    const body = (await response.json()) as Partial<Problem>;
    throw new Error(
      `Avatar upload failed: status=${String(response.status())} code=${body.code ?? "UNKNOWN"} retryable=${String(body.retryable ?? false)}`,
    );
  }
  const agent = (await response.json()) as Agent;
  expect(agent.ref).toBe(agentRef);
  expect(agent.projectRef).toBe(projectRef);
  expect(agent.avatar).toMatchObject({ source: "ARTIFACT" });
  await expect(dialog).not.toBeVisible();
  return agent;
}

function exactAvatarArtifactRef(agent: Agent): string {
  const ref = agent.avatar?.artifactRef;
  if (!ref || agent.avatar?.source !== "ARTIFACT")
    throw new Error("Agent avatar artifact is unavailable");
  return ref;
}
