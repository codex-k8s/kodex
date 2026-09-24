import { randomUUID } from "node:crypto";

import { expect, test } from "@playwright/test";

import { authenticateOwner } from "./auth-flow";
import { loadE2EAuthEnvironment } from "./environment";
import { gotoWithRetry } from "./helpers";

const environment = loadE2EAuthEnvironment();

test("помощник создаёт проект только после проверки и подтверждения плана", async ({
  page,
}) => {
  test.skip(
    process.env.KODEX_E2E_ASSISTANT_LIVE !== "1",
    "Реальный вызов модели запускается только явно",
  );
  test.setTimeout(240_000);
  const browserFailures: string[] = [];
  page.on("pageerror", (error) => browserFailures.push(error.name));
  await authenticateOwner(
    page,
    {
      username: environment.ownerUsername,
      password: environment.ownerPassword,
    },
    { mode: "local" },
  );
  await gotoWithRetry(page, "/projects");
  await page.getByRole("button", { name: "Открыть Kodex" }).click();
  const assistant = page.getByRole("dialog", { name: "Kodex" });
  await expect(assistant).toBeVisible();
  await assistant
    .locator(".assistant-drawer__header")
    .getByRole("button", { name: "Новый диалог" })
    .click();

  const projectName = `Локальная проверка помощника ${randomUUID().slice(0, 8)}`;
  await assistant
    .getByRole("textbox", {
      name: "Опишите, что нужно настроить или запустить",
    })
    .fill(
      `Предложи ровно один план создания проекта «${projectName}» с целью «Безопасная проверка создания проекта через помощника» и языком ru. Не добавляй другие операции и ничего не применяй без моего подтверждения.`,
    );
  await assistant.getByRole("button", { name: "Отправить помощнику" }).click();
  await expect(
    assistant.getByRole("status", { name: "Kodex отвечает" }),
  ).toBeVisible({ timeout: 30_000 });

  const plan = assistant.locator(".assistant-plan-card").last();
  await expect(plan).toBeVisible({ timeout: 180_000 });
  await expect(
    plan.locator(".assistant-plan-card__operations > li"),
  ).toHaveCount(1);
  await expect(plan).toContainText(projectName);
  await expect(plan).toContainText("PROJECT");
  await expect(plan).toContainText("Создать");
  await plan.getByRole("button", { name: "Открыть план" }).click();
  const editor = assistant.locator(".assistant-plan-editor");
  await expect(editor).toBeVisible();
  await editor.getByRole("button", { name: "Проверить ревизию" }).click();
  const apply = editor.getByRole("button", { name: "Применить атомарно" });
  await expect(apply).toBeEnabled({ timeout: 30_000 });
  await apply.click();
  await expect
    .poll(
      async () =>
        page.evaluate(async (name) => {
          const query = new URLSearchParams({ query: name, pageSize: "30" });
          const response = await fetch(`/api/v1/projects?${query.toString()}`);
          if (!response.ok) return false;
          const page = (await response.json()) as {
            items: Array<{ name: string; lifecycle: string }>;
          };
          return page.items.some(
            (item) => item.name === name && item.lifecycle === "ACTIVE",
          );
        }, projectName),
      { timeout: 30_000, intervals: [500, 1_000, 2_000] },
    )
    .toBe(true);
  await editor.getByRole("button", { name: "Вернуться к диалогу" }).click();
  await assistant.getByRole("button", { name: "Закрыть" }).click();
  await expect(page.getByRole("link", { name: projectName })).toBeVisible({
    timeout: 30_000,
  });
  expect(browserFailures).toEqual([]);
});
