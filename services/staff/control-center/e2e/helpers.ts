import { expect, type Locator, type Page } from "@playwright/test";

export function routeRef(page: Page, segment: string): string {
  const parts = new URL(page.url()).pathname.split("/").filter(Boolean);
  const index = parts.indexOf(segment);
  const value = index >= 0 ? parts[index + 1] : undefined;
  if (!value) throw new Error(`Route does not contain ${segment} reference`);
  return value;
}

export async function expectPageHeading(
  page: Page,
  name: string | RegExp,
): Promise<void> {
  await expect(page.getByRole("heading", { level: 1, name })).toBeVisible();
}

export function runStatus(page: Page): Locator {
  return page.locator(".page-header__actions .status-badge").first();
}

export async function expectRunState(
  page: Page,
  state: string | RegExp,
  timeout = 600_000,
): Promise<void> {
  await expect(runStatus(page)).toHaveText(state, { timeout });
}

export async function createAgent(
  page: Page,
  projectRef: string,
  input: {
    name: string;
    purpose: string;
    role: string;
    instructions: string;
  },
): Promise<string> {
  await page.goto(`/projects/${projectRef}/agents`);
  await page.getByRole("button", { name: "Новый сотрудник" }).first().click();
  const dialog = page.getByRole("dialog", { name: "Новый сотрудник" });
  await dialog.getByLabel("Название").fill(input.name);
  await dialog.getByLabel("Назначение").fill(input.purpose);
  await dialog.getByLabel("Роль и описание").fill(input.role);
  await dialog.getByLabel("Инструкции").fill(input.instructions);
  await dialog.getByRole("button", { name: "Создать", exact: true }).click();
  await expect(page).toHaveURL(/\/agents\/[^/]+$/);
  return routeRef(page, "agents");
}

export async function publishAndPrepareAgent(page: Page): Promise<void> {
  const validate = page.getByRole("button", { name: "Проверить инструкции" });
  if (await validate.isEnabled()) await validate.click();

  const publish = page.getByRole("button", { name: "Опубликовать инструкции" });
  await expect(publish).toBeEnabled();
  await publish.click();
  await expect(
    page.locator(".panel").filter({ hasText: "Инструкции" }),
  ).toContainText("Опубликован");

  const environment = page.locator(".role-environment-panel");
  const recommended = environment.getByRole("radio").first();
  if (!(await recommended.isChecked())) await recommended.check();
  await environment
    .getByRole("button", { name: "Сохранить и подготовить окружение" })
    .click();
  await expect(environment).toContainText("Готово", { timeout: 600_000 });
}

export async function launchAgent(page: Page, task: string): Promise<string> {
  const panel = page.locator(".launch-panel");
  await panel.getByLabel("Задание").fill(task);
  await panel.getByRole("button", { name: "Запустить", exact: true }).click();
  await expect(page).toHaveURL(/\/runs\/[^/]+$/);
  return routeRef(page, "runs");
}

export async function waitForTerminalSuccess(page: Page): Promise<void> {
  await expectRunState(page, "Завершён");
}

export async function assertNoDuplicateGraphNodes(page: Page): Promise<void> {
  const refs = await page
    .locator(".canvas-node")
    .evaluateAll((nodes) =>
      nodes.map((node) => node.getAttribute("data-node-ref") ?? ""),
    );
  expect(refs.every(Boolean)).toBe(true);
  expect(new Set(refs).size).toBe(refs.length);
}
