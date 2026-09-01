import { expect, type Locator, type Page } from "@playwright/test";

export type BrowserJsonReadback<T> = {
  readonly body: T;
  readonly status: number;
};

export async function readJsonWithNetworkRetry<T>(
  page: Page,
  path: string,
): Promise<BrowserJsonReadback<T>> {
  if (!path.startsWith("/api/")) {
    throw new Error("Read-only browser JSON path must start with /api/");
  }

  const retryDelays = [0, 200, 600];
  for (const [index, delay] of retryDelays.entries()) {
    if (delay > 0) await page.waitForTimeout(delay);
    try {
      return await page.evaluate(async (requestPath) => {
        const response = await fetch(requestPath);
        const rawBody = await response.text();
        if (!rawBody) {
          throw new Error(
            `Read-only browser JSON response is empty: status=${String(response.status)}`,
          );
        }
        return {
          body: JSON.parse(rawBody) as T,
          status: response.status,
        };
      }, path);
    } catch (error) {
      if (
        index === retryDelays.length - 1 ||
        !isTransientBrowserFetchFailure(error)
      ) {
        throw error;
      }
    }
  }

  throw new Error("Read-only browser JSON retry budget exhausted");
}

function isTransientBrowserFetchFailure(error: unknown): boolean {
  if (!(error instanceof Error)) return false;
  return /(?:TypeError: Failed to fetch|NetworkError when attempting to fetch resource|Load failed|net::ERR_(?:CONNECTION_RESET|NETWORK_CHANGED|CONNECTION_CLOSED))/.test(
    error.message,
  );
}

export async function gotoWithRetry(
  page: Page,
  url: string,
  options?: Parameters<Page["goto"]>[1],
  requirements: { readonly appShell?: boolean } = {},
): Promise<void> {
  const retryDelays = [0, 200, 600];
  for (const [index, delay] of retryDelays.entries()) {
    if (delay > 0) await page.waitForTimeout(delay);
    try {
      const response = await page.goto(url, options);
      if (!response) {
        throw new Error(
          `Navigation did not return a main document response for ${safeNavigationPath(page.url())}`,
        );
      }
      const contentType = response.headers()["content-type"] ?? "missing";
      if (!response.ok() || !contentType.toLowerCase().includes("text/html")) {
        throw new Error(
          [
            "Main document navigation failed",
            `path=${safeNavigationPath(response.url())}`,
            `status=${String(response.status())}`,
            `content-type=${safeContentType(contentType)}`,
          ].join("; "),
        );
      }
      if (requirements.appShell !== false) {
        await expect(page.locator("#app")).toBeVisible();
        await expect(page.locator("#main-content")).toBeVisible();
      }
      return;
    } catch (error) {
      const emptyAppShell =
        requirements.appShell !== false && (await hasEmptyAppShell(page));
      if (
        index === retryDelays.length - 1 ||
        !(error instanceof Error) ||
        (!error.message.includes("net::ERR_NETWORK_CHANGED") && !emptyAppShell)
      ) {
        throw error;
      }
    }
  }
}

async function hasEmptyAppShell(page: Page): Promise<boolean> {
  const app = page.locator("#app");
  if ((await app.count()) !== 1) return false;
  return app.evaluate((element) => element.childElementCount === 0);
}

function safeNavigationPath(raw: string): string {
  try {
    return new URL(raw).pathname.slice(0, 512);
  } catch {
    return "invalid-url";
  }
}

function safeContentType(value: string): string {
  return value.replace(/[^A-Za-z0-9!#$&^_.+\-/;= ]/g, "?").slice(0, 160);
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
  const connection = page.getByRole("status", { name: "Подключено" });
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
  let capability = page.getByRole("checkbox", { name: capabilityName });
  if ((await capability.count()) === 0) {
    await page.getByRole("tab", { name: "Возможности" }).click();
    capability = page.getByRole("checkbox", { name: capabilityName });
  }
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
  let panel = page.locator(".launch-panel");
  if ((await panel.count()) === 0) {
    await page.getByRole("tab", { name: "Профиль сотрудника" }).click();
    panel = page.locator(".launch-panel");
  }
  await expect(panel).toBeVisible();
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
    .getByRole("region", { name: "Граф выполнения" })
    .locator('[role="button"][data-node-ref]')
    .evaluateAll((nodes) =>
      nodes.map((node) => node.getAttribute("data-node-ref") ?? ""),
    );
  expect(refs.every(Boolean)).toBe(true);
  expect(new Set(refs).size).toBe(refs.length);
}
