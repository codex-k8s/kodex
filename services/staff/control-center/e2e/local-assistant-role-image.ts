import { randomUUID } from "node:crypto";

import { expect, test, type Locator, type Page } from "@playwright/test";

import type {
  Agent,
  RoleImageRecipeDetail,
  RoleImageRecipePage,
} from "../src/shared/api/generated/openapi/types.gen";
import { authenticateOwner } from "./auth-flow";
import { loadE2EAuthEnvironment } from "./environment";
import { gotoWithRetry } from "./helpers";

const environment = loadE2EAuthEnvironment();

test("помощник создаёт рецепт образа и показывает итог сборки", async ({
  page,
}) => {
  let projectRef = process.env.KODEX_E2E_ASSISTANT_ROLE_IMAGE_PROJECT ?? "";
  let agentRef = process.env.KODEX_E2E_ASSISTANT_ROLE_IMAGE_AGENT ?? "";
  test.skip(
    process.env.KODEX_E2E_ASSISTANT_ROLE_IMAGE !== "1",
    "Сборка образа разрешена только явно",
  );
  test.setTimeout(600_000);
  const mobile = process.env.KODEX_E2E_ASSISTANT_ROLE_IMAGE_MOBILE === "1";
  if (mobile) await page.setViewportSize({ width: 390, height: 844 });
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
  if (projectRef || agentRef) {
    expect(
      projectRef,
      "Тестовый проект и сотрудник задаются вместе",
    ).toBeTruthy();
    expect(
      agentRef,
      "Тестовый проект и сотрудник задаются вместе",
    ).toBeTruthy();
  } else {
    const suffix = randomUUID().slice(0, 8);
    const project = await mutate<{ ref: string }>(page, "/api/v1/projects", {
      name: `Локальная проверка образа ${suffix}`,
      purpose: "Безопасная локальная проверка сборки образа помощником.",
      language: "ru",
    });
    projectRef = project.ref;
    const createdAgent = await mutate<Agent>(
      page,
      `/api/v1/projects/${projectRef}/agents`,
      {
        name: `Исполнитель образа ${suffix}`,
        purpose: "Проверить собственную сборку образа в локальном контуре.",
        roleDescription: "Тестовый сотрудник проверки образов.",
        initialInstructions: "Выполняй только задания локальной проверки.",
      },
    );
    agentRef = createdAgent.ref;
  }
  const agent = await read<Agent>(page, `/api/v1/agents/${agentRef}`);
  expect(agent).toMatchObject({ ref: agentRef, projectRef, state: "READY" });
  const resumeConversationRef =
    process.env.KODEX_E2E_ASSISTANT_ROLE_IMAGE_CONVERSATION ?? "";
  let recipeName = `Образ локальной проверки ${randomUUID().slice(0, 8)}`;

  await gotoWithRetry(page, `/projects/${projectRef}`);
  await page.getByRole("button", { name: "Открыть Kodex" }).click();
  const assistant = page.getByRole("dialog", { name: "Kodex" });
  await expect(assistant).toBeVisible();
  if (resumeConversationRef) {
    await assistant
      .locator(`[data-conversation-ref="${resumeConversationRef}"]`)
      .last()
      .click();
    await expect(assistant).toHaveAttribute(
      "data-conversation-ref",
      resumeConversationRef,
    );
  } else {
    const previousConversationRef = await assistant.getAttribute(
      "data-conversation-ref",
    );
    const createdConversationResponse = page.waitForResponse(
      (response) =>
        response.request().method() === "POST" &&
        new URL(response.url()).pathname === "/api/v1/assistant-conversations",
      { timeout: 30_000 },
    );
    await assistant
      .locator(".assistant-drawer__header")
      .getByRole("button", { name: "Новый диалог" })
      .click();
    expect((await createdConversationResponse).status()).toBe(201);
    await expect
      .poll(() => assistant.getAttribute("data-conversation-ref"))
      .not.toBe(previousConversationRef);
    await assistant
      .getByRole("textbox", {
        name: "Опишите, что нужно настроить или запустить",
      })
      .fill(
        `Для существующего сотрудника «${agent.name}» в текущем проекте предложи ровно один план CREATE_ROLE_IMAGE_RECIPE с именем «${recipeName}». Выбери разрешённую среду из каталога. Не добавляй другие операции, не создавай новый проект или сотрудника и не применяй план до моего подтверждения.`,
      );
    await assistant
      .getByRole("button", { name: "Отправить помощнику" })
      .click();
  }

  const plan = assistant.locator(".assistant-plan-card").last();
  await expect(plan).toBeVisible({ timeout: 180_000 });
  await expect(
    plan.locator(".assistant-plan-card__operations > li"),
  ).toHaveCount(1);
  await expect(plan).toContainText("ROLE_IMAGE_RECIPE");
  if (resumeConversationRef) {
    recipeName = (
      await plan.locator(".assistant-plan-card__target").innerText()
    ).trim();
    expect(recipeName).toMatch(/^Образ локальной проверки /);
  }
  await expect(plan).toContainText(recipeName);
  await plan.getByRole("button", { name: "Открыть план" }).click();
  const editor = assistant.locator(".assistant-plan-editor");
  await expect(editor).toBeVisible();
  if (mobile) await expectMobileViewport(page, assistant);
  await expect(editor).toContainText("CREATE_ROLE_IMAGE_RECIPE");
  await expect(editor).toContainText(agent.name);
  await editor.getByRole("button", { name: "Проверить ревизию" }).click();
  const apply = editor.getByRole("button", { name: "Применить атомарно" });
  await expect(apply).toBeEnabled({ timeout: 30_000 });
  await apply.click();
  await expect(editor.locator(".assistant-plan-receipt")).toBeVisible({
    timeout: 90_000,
  });
  await editor.getByRole("button", { name: "Вернуться к диалогу" }).click();

  const card = assistant.locator(".assistant-build-card").last();
  await expect(card).toBeVisible({ timeout: 30_000 });
  if (mobile) await expectMobileViewport(page, assistant);
  const recipeLink = card.locator(
    `a[href^="/projects/${projectRef}/role-images/imgrec_"]`,
  );
  await expect(recipeLink).toBeVisible();
  const recipeRef =
    (await recipeLink.getAttribute("href"))?.split("/").at(-1) ?? "";
  expect(recipeRef).toMatch(/^imgrec_/);
  const path = `/api/v1/projects/${projectRef}/role-image-recipes/${recipeRef}`;

  if (process.env.KODEX_E2E_ASSISTANT_ROLE_IMAGE_CANCEL === "1") {
    const stop = card.getByRole("button", { name: "Остановить сборку" });
    await expect(stop).toBeVisible({ timeout: 30_000 });
    page.once("dialog", (dialog) => void dialog.accept());
    await stop.click();
    await expect
      .poll(
        async () => {
          const detail = await read<RoleImageRecipeDetail>(page, path);
          return detail.builds.at(-1)?.stage ?? "NO_BUILD";
        },
        { timeout: 60_000, intervals: [500, 1_000, 2_000] },
      )
      .toBe("CANCELLED");
    await page.waitForTimeout(2_000);
    const cancelled = await read<RoleImageRecipeDetail>(page, path);
    expect(cancelled.builds.at(-1)?.stage).toBe("CANCELLED");
    expect(cancelled.recipe.promotedImageReady).toBe(false);
    expect(browserFailures).toEqual([]);
    return;
  }

  const conversationRef = await assistant.getAttribute("data-conversation-ref");
  if (!conversationRef)
    throw new Error("Assistant conversation ref is missing");
  await page.reload({ waitUntil: "domcontentloaded" });
  await expect(assistant).toBeVisible({ timeout: 30_000 });
  await assistant
    .locator(`[data-conversation-ref="${conversationRef}"]`)
    .last()
    .click({ timeout: 30_000 });
  await expect(assistant).toHaveAttribute(
    "data-conversation-ref",
    conversationRef,
  );
  await expect(card).toBeVisible({ timeout: 30_000 });
  await expect(recipeLink).toHaveAttribute(
    "href",
    `/projects/${projectRef}/role-images/${recipeRef}`,
  );

  await expect
    .poll(
      async () => {
        const detail = await read<RoleImageRecipeDetail>(page, path);
        expect(detail.recipe).toMatchObject({ ref: recipeRef, projectRef });
        const build = detail.builds.at(-1);
        if (!build) return "NO_BUILD";
        if (
          ["FAILED", "CANCELLED", "EXPIRED", "DEAD_LETTER"].includes(
            build.stage,
          )
        )
          throw new Error(
            `Assistant role image build ended with ${build.safeErrorCode ?? build.stage}`,
          );
        if (detail.promotionCandidate?.admissionVerdict === "REJECTED")
          throw new Error(
            "Assistant role image admission rejected the artifact",
          );
        return build.stage === "COMPLETED" &&
          detail.promotionCandidate?.admissionVerdict === "ACCEPTED"
          ? "READY_TO_PROMOTE"
          : build.stage;
      },
      { timeout: 360_000, intervals: [1_000, 2_000, 5_000] },
    )
    .toBe("READY_TO_PROMOTE");
  await recipeLink.click();
  await expect(page).toHaveURL(
    new RegExp(`/projects/${projectRef}/role-images/${recipeRef}$`),
  );
  await expect(assistant).toBeHidden({ timeout: 10_000 });
  await (await promotionButton(page)).click({ timeout: 10_000 });
  await expect
    .poll(
      async () =>
        (await read<RoleImageRecipeDetail>(page, path)).recipe
          .promotedImageReady,
      { timeout: 180_000, intervals: [1_000, 2_000, 5_000] },
    )
    .toBe(true);
  expect(browserFailures).toEqual([]);
});

test("карточка собственной сборки восстанавливается после reload", async ({
  page,
}) => {
  const projectRef = process.env.KODEX_E2E_ASSISTANT_ROLE_IMAGE_PROJECT ?? "";
  const conversationRef =
    process.env.KODEX_E2E_ASSISTANT_ROLE_IMAGE_CONVERSATION ?? "";
  const recipeRef = process.env.KODEX_E2E_ASSISTANT_ROLE_IMAGE_RECIPE_REF ?? "";
  test.skip(
    !projectRef || !conversationRef || !recipeRef,
    "Нужны точные ссылки только собственной тестовой сборки",
  );
  await authenticateOwner(
    page,
    {
      username: environment.ownerUsername,
      password: environment.ownerPassword,
    },
    { mode: "local" },
  );
  await gotoWithRetry(page, `/projects/${projectRef}`);
  await page.getByRole("button", { name: "Открыть Kodex" }).click();
  const assistant = page.getByRole("dialog", { name: "Kodex" });
  const conversation = assistant
    .locator(`[data-conversation-ref="${conversationRef}"]`)
    .last();
  await conversation.click();
  const card = assistant.locator(".assistant-build-card").last();
  const recipeLink = card.locator(
    `a[href="/projects/${projectRef}/role-images/${recipeRef}"]`,
  );
  await expect(recipeLink).toBeVisible();
  await page.reload({ waitUntil: "domcontentloaded" });
  await expect(assistant).toBeVisible({ timeout: 30_000 });
  await conversation.click();
  await expect(assistant).toHaveAttribute(
    "data-conversation-ref",
    conversationRef,
  );
  await expect(recipeLink).toBeVisible();
});

test("существующий образ проходит допуск и явную публикацию", async ({
  page,
}) => {
  const projectRef = process.env.KODEX_E2E_ASSISTANT_ROLE_IMAGE_PROJECT ?? "";
  const recipeName =
    process.env.KODEX_E2E_ASSISTANT_ROLE_IMAGE_RECIPE_NAME ?? "";
  test.skip(
    !projectRef || !recipeName,
    "Повторная проверка требует точный проект и имя собственной сборки",
  );
  test.setTimeout(240_000);
  await authenticateOwner(
    page,
    {
      username: environment.ownerUsername,
      password: environment.ownerPassword,
    },
    { mode: "local" },
  );
  const catalog = await read<RoleImageRecipePage>(
    page,
    `/api/v1/projects/${projectRef}/role-image-recipes?${new URLSearchParams({ pageSize: "40", query: recipeName })}`,
  );
  const matches = catalog.items.filter((item) => item.name === recipeName);
  expect(matches).toHaveLength(1);
  const recipe = matches[0];
  if (!recipe) throw new Error("Role image recipe was not found");
  const recipeRef = recipe.ref;
  const path = `/api/v1/projects/${projectRef}/role-image-recipes/${recipeRef}`;
  await expect
    .poll(
      async () => {
        const detail = await read<RoleImageRecipeDetail>(page, path);
        if (detail.promotionCandidate?.admissionVerdict === "REJECTED")
          throw new Error("Role image admission rejected the artifact");
        return detail.promotionCandidate?.admissionVerdict ?? "PENDING";
      },
      { timeout: 120_000, intervals: [1_000, 2_000, 5_000] },
    )
    .toBe("ACCEPTED");
  await gotoWithRetry(page, `/projects/${projectRef}/role-images/${recipeRef}`);
  await (await promotionButton(page)).click({ timeout: 10_000 });
  await expect
    .poll(
      async () =>
        (await read<RoleImageRecipeDetail>(page, path)).recipe
          .promotedImageReady,
      { timeout: 120_000, intervals: [1_000, 2_000, 5_000] },
    )
    .toBe(true);
});

async function promotionButton(page: Page): Promise<Locator> {
  const button = page.getByRole("button", { name: "Публикация образа" });
  const retry = page.getByRole("button", { name: "Повторить" });
  let retries = 0;
  await expect
    .poll(
      async () => {
        if (await button.isVisible()) return true;
        if (retries < 2 && (await retry.isVisible())) {
          retries += 1;
          await retry.click();
        }
        return false;
      },
      { timeout: 30_000, intervals: [500, 1_000, 2_000] },
    )
    .toBe(true);
  return button;
}

async function read<T>(page: Page, path: string): Promise<T> {
  return page.evaluate(async (url) => {
    for (let attempt = 0; attempt < 5; attempt += 1) {
      try {
        const response = await fetch(url, { cache: "no-store" });
        if (!response.ok)
          throw new Error(
            `API read failed: ${String(response.status)} ${new URL(url, location.origin).pathname}`,
          );
        return (await response.json()) as T;
      } catch (error) {
        if (!(error instanceof TypeError) || attempt === 4) throw error;
        await new Promise((resolve) => setTimeout(resolve, 400));
      }
    }
    throw new Error("API read retries exhausted");
  }, path);
}

async function mutate<T>(page: Page, path: string, body: unknown): Promise<T> {
  const result = await page.evaluate(
    async (input) => {
      const prefix = `${encodeURIComponent("__Host-kodex-csrf")}=`;
      const csrf = document.cookie
        .split(";")
        .map((item) => item.trim())
        .find((item) => item.startsWith(prefix))
        ?.slice(prefix.length);
      if (!csrf)
        return { status: 0, code: "CSRF_UNAVAILABLE", body: undefined };
      const response = await fetch(input.path, {
        method: "POST",
        headers: {
          Accept: "application/json",
          "Content-Type": "application/json",
          "Idempotency-Key": input.key,
          "X-CSRF-Token": decodeURIComponent(csrf),
        },
        body: JSON.stringify(input.body),
      });
      const decoded = (await response.json()) as Record<string, unknown>;
      return {
        status: response.status,
        code: typeof decoded.code === "string" ? decoded.code : "UNKNOWN",
        body: response.ok ? decoded : undefined,
      };
    },
    { path, body, key: randomUUID() },
  );
  if (result.status < 200 || result.status >= 300 || !result.body)
    throw new Error(
      `API mutation failed: ${String(result.status)} ${result.code}`,
    );
  return result.body as T;
}

async function expectMobileViewport(
  page: Page,
  assistant: Locator,
): Promise<void> {
  const bounds = await assistant.boundingBox();
  expect(bounds).not.toBeNull();
  expect(bounds?.x ?? -1).toBeGreaterThanOrEqual(0);
  expect((bounds?.x ?? 390) + (bounds?.width ?? 390)).toBeLessThanOrEqual(391);
  const overflow = await page.evaluate(
    () => document.documentElement.scrollWidth - window.innerWidth,
  );
  expect(overflow).toBeLessThanOrEqual(1);
}
