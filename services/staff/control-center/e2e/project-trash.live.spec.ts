import { expect, test } from "@playwright/test";

import { authenticateOwner } from "./auth-flow";
import { createAgent } from "./helpers";

test("диагностика доступности корзины", async ({ page }) => {
  const credentials = {
    username: process.env.KODEX_E2E_OWNER_USERNAME ?? "",
    password: process.env.KODEX_E2E_OWNER_PASSWORD ?? "",
  };
  await authenticateOwner(page, credentials, { mode: "local" });
  const response = page.waitForResponse((item) => new URL(item.url()).pathname === "/api/v1/projects/trash");
  await page.goto("/projects?trash=1");
  const result = await response;
  const body = await result.json() as Record<string, unknown>;
  const plain = await page.request.get("/api/v1/projects/trash");
  const plainBody = await plain.json() as Record<string, unknown>;
  expect(result.status(), JSON.stringify({ code: body.code, query: new URL(result.url()).search,
    plainStatus: plain.status(), plainCode: plainBody.code })).toBe(200);
});

test("проект с сотрудником проходит корзину, восстановление и физическое удаление", async ({ page }) => {
  const credentials = {
    username: process.env.KODEX_E2E_OWNER_USERNAME ?? "",
    password: process.env.KODEX_E2E_OWNER_PASSWORD ?? "",
  };
  if (!credentials.username || !credentials.password) throw new Error("Local owner credentials are required");
  await authenticateOwner(page, credentials, { mode: "local" });

  const projectName = `local-trash-${Date.now()}`;
  await page.goto("/projects");
  await page.getByRole("button", { name: "Новый Проект" }).click();
  const create = page.getByRole("dialog", { name: "Новый Проект" });
  await create.getByLabel("Название").fill(projectName);
  await create.getByLabel("Назначение").fill("Изолированная проверка корзины и дочерних объектов");
  await create.getByRole("button", { name: "Создать", exact: true }).click();
  await expect(page).toHaveURL(/\/projects\/[^/]+$/);
  const projectRef = new URL(page.url()).pathname.split("/").at(-1) ?? "";
  expect(projectRef).toMatch(/^prj_/);

  const agentRef = await createAgent(page, projectRef, {
    name: `${projectName}-agent`,
    purpose: "Проверка каскадного жизненного цикла",
    role: "Тестовый сотрудник проекта",
    instructions: "Работай только в рамках тестового проекта.",
  });
  expect(agentRef).toMatch(/^agt_/);

  await page.goto("/projects");
  const active = page.locator(".project-list__item").filter({ hasText: projectName });
  await expect(active).toBeVisible();
  await active.getByRole("button", { name: "Переместить в корзину" }).click();
  await page.getByRole("dialog", { name: "Переместить в корзину" })
    .getByRole("button", { name: "Переместить в корзину" }).click();
  await expect(active).toHaveCount(0);

  await page.getByRole("button", { name: "Корзина" }).click();
  const trashed = page.locator(".project-list__item").filter({ hasText: projectName });
  await expect(trashed).toBeVisible();
  await trashed.getByRole("button", { name: "Восстановить Проект" }).click();
  await page.getByRole("dialog", { name: "Восстановить Проект" })
    .getByRole("button", { name: "Восстановить Проект" }).click();
  await expect(trashed).toHaveCount(0);

  await page.getByRole("button", { name: "К Проектам" }).click();
  await expect(active).toBeVisible();
  await page.goto(`/projects/${projectRef}/agents/${agentRef}`);
  await expect(page.getByText(`${projectName}-agent`, { exact: true }).first()).toBeVisible();

  await page.goto("/projects");
  await active.getByRole("button", { name: "Переместить в корзину" }).click();
  await page.getByRole("dialog", { name: "Переместить в корзину" })
    .getByRole("button", { name: "Переместить в корзину" }).click();
  await page.getByRole("button", { name: "Корзина" }).click();
  await expect(trashed).toBeVisible();
  await trashed.getByRole("button", { name: "Удалить безвозвратно" }).click();
  const purge = page.getByRole("dialog", { name: "Удалить безвозвратно" });
  await purge.getByLabel("Название проекта для подтверждения").fill(projectName);
  await purge.getByRole("button", { name: "Удалить безвозвратно" }).click();
  await expect(trashed).toHaveCount(0, { timeout: 60_000 });
  const project = await page.request.get(`/api/v1/projects/${projectRef}`);
  expect([403, 404]).toContain(project.status());
  const agent = await page.request.get(`/api/v1/agents/${agentRef}`);
  expect([403, 404]).toContain(agent.status());
});
