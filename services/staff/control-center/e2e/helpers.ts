import { expect, type Locator, type Page } from "@playwright/test";

export async function gotoWithRetry(
  page: Page,
  url: string,
  options?: Parameters<Page["goto"]>[1],
): Promise<void> {
  const retryDelays = [0, 200, 600];
  for (const [index, delay] of retryDelays.entries()) {
    if (delay > 0) await page.waitForTimeout(delay);
    try {
      await page.goto(url, options);
      return;
    } catch (error) {
      if (
        index === retryDelays.length - 1 ||
        !(error instanceof Error) ||
        !error.message.includes("net::ERR_NETWORK_CHANGED")
      ) {
        throw error;
      }
    }
  }
}

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

export async function waitForConnected(
  page: Page,
  timeout = 30_000,
): Promise<void> {
  const connection = page.locator('.connection-badge[data-state="CONNECTED"]');
  await expect(connection).toBeVisible({ timeout });
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
  await gotoWithRetry(page, `/projects/${projectRef}/agents`);
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

export async function publishAgent(page: Page): Promise<void> {
  const validate = page.getByRole("button", { name: "Проверить инструкции" });
  if ((await validate.count()) > 0) {
    await expect(validate).toBeEnabled();
    await validate.click();
  }

  const publish = page.getByRole("button", { name: "Опубликовать инструкции" });
  if ((await publish.count()) > 0) {
    await expect(publish).toBeEnabled();
    await publish.click();
  }
  await expect(
    page.locator(".panel").filter({ hasText: "Инструкции" }),
  ).toContainText("Опубликован");
}

export async function ensureAgentCapability(
  page: Page,
  projectRef: string,
  agentRef: string,
  capabilityName: string | RegExp,
): Promise<void> {
  await gotoWithRetry(page, `/projects/${projectRef}/agents/${agentRef}`);
  const capability = page.getByRole("checkbox", { name: capabilityName });
  await expect(capability).toBeVisible();
  if (!(await capability.isChecked())) {
    const response = page.waitForResponse(
      (candidate) =>
        candidate.request().method() === "POST" &&
        candidate.url().includes(`/api/v1/agents/${agentRef}/commands`),
    );
    await capability.check();
    expect((await response).ok()).toBe(true);
    await expect(page.getByText("Сохраняем возможность…")).toHaveCount(0);
  }
  await expect(capability).toBeChecked();
}

export async function launchAgent(page: Page, task: string): Promise<string> {
  const panel = page.locator(".launch-panel");
  await panel.getByLabel("Задание").fill(task);
  await panel.getByRole("button", { name: "Запустить", exact: true }).click();
  await expect(page).toHaveURL(/\/runs\/[^/]+$/);
  return routeRef(page, "runs");
}

export async function waitForTerminalSuccess(page: Page): Promise<void> {
  await page.waitForFunction(
    () => {
      const state = document
        .querySelector(".page-header__actions .status-badge")
        ?.getAttribute("data-state");
      return ["SUCCEEDED", "FAILED", "CANCELLED"].includes(state ?? "");
    },
    undefined,
    { timeout: 600_000 },
  );
  const state = await runStatus(page).getAttribute("data-state");
  expect(
    state,
    `Run reached unexpected terminal state ${state ?? "UNKNOWN"}`,
  ).toBe("SUCCEEDED");
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
