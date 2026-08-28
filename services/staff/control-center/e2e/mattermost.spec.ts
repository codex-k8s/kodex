import { readFile } from "node:fs/promises";

import {
  type APIRequestContext,
  type Locator,
  type Page,
} from "@playwright/test";

import {
  loadE2EEnvironment,
  type MattermostE2EEnvironment,
} from "./environment";
import { expect, test } from "./fixtures";
import {
  createAgent,
  expectRunState,
  gotoWithRetry,
  launchAgent,
  publishAgent,
  routeRef,
  waitForTerminalSuccess,
} from "./helpers";

const environment = loadE2EEnvironment();
const mattermost = requireMattermost(environment.mattermost);

const projectName = `${environment.resourcePrefix} — клиентский сервис`;
const agentName = `${environment.resourcePrefix} — менеджер клиентов`;
const runTitle = `${environment.resourcePrefix} — итог для клиента`;

function requireMattermost(
  value: MattermostE2EEnvironment | undefined,
): MattermostE2EEnvironment {
  if (!value) throw new Error("Mattermost E2E profile is required");
  return value;
}

test("необязательная Mattermost-доставка зеркалирует результат и не меняет core Run при outage", async ({
  page,
  request,
}) => {
  test.skip(environment.checkOnly, "check-only validates discovery and types");
  if (!mattermost.tokenFile)
    throw new Error("Mattermost token file is required");

  const token = (await readFile(mattermost.tokenFile, "utf8")).trim();
  if (!token || token.length > 4096 || /[\r\n]/.test(token)) {
    throw new Error("Mattermost token file must contain one bounded token");
  }

  const projectRef = await createProject(page);
  const agentRef = await createAgent(page, projectRef, {
    name: agentName,
    purpose: "Готовить безопасные итоги для клиентов и сотрудников.",
    role: "Менеджер клиентского сервиса.",
    instructions:
      "Работай только с заданием, отвечай по-русски и создавай безопасный текстовый файл с итогом.",
  });
  await publishAgent(page);

  await grantCapability(
    page,
    mattermost.healthyConnectionName,
    projectName,
    agentName,
    "Уведомления",
  );
  await grantCapability(
    page,
    mattermost.healthyConnectionName,
    projectName,
    agentName,
    "Зеркало результатов",
  );
  await grantCapability(
    page,
    mattermost.outageConnectionName,
    projectName,
    agentName,
    "Уведомления",
  );

  await gotoWithRetry(page, `/projects/${projectRef}/agents/${agentRef}`);
  const runRef = await launchAgent(
    page,
    `Подготовь итог с точным заголовком «${runTitle}» и создай файл customer-result.txt.`,
  );
  expect(routeRef(page, "runs")).toBe(runRef);
  await waitForTerminalSuccess(page);

  const incident = page
    .locator(".incident-row")
    .filter({ hasText: "Не удалось доставить сообщение" });
  await expect(incident).toBeVisible({ timeout: 180_000 });
  await expect(incident).toContainText(
    /Доставка будет автоматически повторена|Автоматические попытки исчерпаны/,
  );
  await expect(
    page.getByText("Основной результат и файлы доступны."),
  ).toBeVisible();
  await expectRunState(page, "Завершён");
  await expect(page.locator(".artifact-row").first()).toBeVisible();

  await expect
    .poll(
      () =>
        countMatchingMattermostPosts(request, token, [
          `Kodex: «${runTitle}»`,
          `Kodex: результат «${runTitle}»`,
        ]),
      { timeout: 180_000, intervals: [1_000, 2_000, 5_000] },
    )
    .toBe(2);
});

async function createProject(page: Page): Promise<string> {
  await gotoWithRetry(page, "/projects");
  await page.getByRole("button", { name: "Новый Проект" }).first().click();
  const dialog = page.getByRole("dialog", { name: "Новый Проект" });
  await dialog.getByLabel("Название").fill(projectName);
  await dialog
    .getByLabel("Назначение")
    .fill("Готовить клиентские итоги без обязательной внешней системы.");
  await dialog.getByRole("button", { name: "Создать", exact: true }).click();
  await expect(page).toHaveURL(/\/projects\/[^/]+$/);
  return routeRef(page, "projects");
}

async function grantCapability(
  page: Page,
  connectionName: string,
  project: string,
  agent: string,
  capability: string,
): Promise<void> {
  await gotoWithRetry(page, "/integrations");
  const card = page
    .locator(".connection-card")
    .filter({ hasText: connectionName });
  await expect(card).toHaveCount(1);
  await card.getByRole("button", { name: "Настроить разрешения" }).click();

  const panel = page.locator(".grant-panel");
  await expect(panel).toContainText(connectionName);
  await panel.getByLabel("Проект").selectOption({ label: project });
  const target = panel.getByLabel("Получатель разрешения");
  await expect(target.locator("option", { hasText: agent })).toHaveCount(1);
  await target.selectOption({ label: agent });
  await selectOptionContaining(panel.getByLabel("Возможность"), capability);
  await panel.getByRole("button", { name: "Выдать разрешение" }).click();
  await expect(
    panel
      .locator(".grant-list .entity-row")
      .filter({ hasText: agent })
      .filter({ hasText: capability }),
  ).toHaveCount(1);
}

async function selectOptionContaining(
  select: Locator,
  text: string,
): Promise<void> {
  const option = select.locator("option").filter({ hasText: text });
  await expect(option).toHaveCount(1);
  const value = await option.getAttribute("value");
  if (!value) throw new Error("Capability option has no value");
  await select.selectOption(value);
}

async function countMatchingMattermostPosts(
  request: APIRequestContext,
  token: string,
  expectedMessages: readonly string[],
): Promise<number> {
  const headers = { Authorization: `Bearer ${token}` };
  const channelResponse = await request.get(
    `${mattermost.origin}/api/v4/teams/name/${encodeURIComponent(mattermost.teamName)}/channels/name/${encodeURIComponent(mattermost.channelName)}`,
    { headers },
  );
  if (!channelResponse.ok()) {
    throw new Error(
      `Mattermost channel lookup failed with HTTP ${String(channelResponse.status())}`,
    );
  }
  const channel = (await channelResponse.json()) as { id?: unknown };
  if (typeof channel.id !== "string" || !channel.id) {
    throw new Error("Mattermost channel lookup returned no channel identity");
  }
  const postsResponse = await request.get(
    `${mattermost.origin}/api/v4/channels/${encodeURIComponent(channel.id)}/posts?per_page=100`,
    { headers },
  );
  if (!postsResponse.ok()) {
    throw new Error(
      `Mattermost post read failed with HTTP ${String(postsResponse.status())}`,
    );
  }
  const body = (await postsResponse.json()) as { posts?: unknown };
  if (
    !body.posts ||
    typeof body.posts !== "object" ||
    Array.isArray(body.posts)
  ) {
    throw new Error(
      "Mattermost post read returned an invalid bounded response",
    );
  }
  const messages = Object.values(body.posts)
    .map((value) =>
      value && typeof value === "object" && "message" in value
        ? (value as { message?: unknown }).message
        : undefined,
    )
    .filter((value): value is string => typeof value === "string");
  return expectedMessages.filter((expected) =>
    messages.some((message) => message.includes(expected)),
  ).length;
}
