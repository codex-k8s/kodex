import { randomUUID } from "node:crypto";

import { expect, test } from "@playwright/test";

import { authenticateOwner } from "./auth-flow";
import { loadE2EAuthEnvironment } from "./environment";
import { gotoWithRetry } from "./helpers";

const environment = loadE2EAuthEnvironment();

test("помощник открывает защищённую форму и публикует секрет без утечки значения", async ({
  page,
}) => {
  const projectRef = process.env.KODEX_E2E_ASSISTANT_SECRET_PROJECT ?? "";
  test.skip(
    process.env.KODEX_E2E_ASSISTANT_SECRET !== "1" || !projectRef,
    "Создание синтетического секрета разрешено только явно в собственной локальной фикстуре",
  );
  test.setTimeout(240_000);
  const value = `local-fixture-${randomUUID()}`;
  const name = `Локальный секрет помощника ${randomUUID().slice(0, 8)}`;
  const leakedURLs: string[] = [];
  page.on("request", (request) => {
    if (request.url().includes(value))
      leakedURLs.push(new URL(request.url()).pathname);
  });
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
  await assistant
    .getByRole("link", { name: "Открыть защищённую форму нового секрета" })
    .click();
  await expect(assistant).toBeHidden();
  await expect(page).toHaveURL(
    new RegExp(`/projects/${projectRef}/secrets\\?assistantCreateSecret=1$`),
  );
  expect(page.url()).not.toContain(value);

  const create = page.getByRole("dialog", { name: "Новый секрет" });
  await expect(create).toBeVisible();
  await expect(create).toContainText(
    "Значение отправляется напрямую в защищённый API",
  );
  async function submitDraft() {
    await create.getByRole("textbox", { name: "Название" }).fill(name);
    await create
      .getByRole("textbox", { name: "Секретное значение" })
      .fill(value);
    const response = page.waitForResponse(
      (candidate) =>
        candidate.request().method() === "POST" &&
        new URL(candidate.url()).pathname ===
          `/api/v1/projects/${projectRef}/runtime-secret-drafts`,
    );
    await create.getByRole("button", { name: "Сохранить черновик" }).click();
    return await response;
  }
  let saved = await submitDraft();
  if (saved.status() === 403) {
    const problem = (await saved.json()) as { code?: string };
    expect(problem.code).toBe("FRESH_AUTHENTICATION_REQUIRED");
    await expect(
      create.getByRole("textbox", { name: "Секретное значение" }),
    ).toHaveValue("");
    await create
      .getByRole("button", { name: "Войти заново для работы с секретами" })
      .click();
    const username = page.locator('input[name="username"]');
    const password = page.locator('input[name="password"]');
    await expect(password).toBeVisible();
    if (await username.isVisible())
      await username.fill(environment.ownerUsername);
    await password.fill(environment.ownerPassword);
    await page
      .locator('button[type="submit"], input[type="submit"]')
      .first()
      .click();
    await expect(page).toHaveURL(
      new RegExp(`/projects/${projectRef}/secrets\\?assistantCreateSecret=1$`),
    );
    await expect(create).toBeVisible();
    saved = await submitDraft();
  }
  if (saved.status() !== 201) {
    const problem = (await saved.json()) as { code?: string };
    throw new Error(
      `Secret draft rejected: ${String(saved.status())} ${problem.code ?? "UNKNOWN"}`,
    );
  }
  expect(saved.status()).toBe(201);
  expect(saved.headers()["cache-control"]).toBe("no-store");
  const draft = (await saved.json()) as {
    ref: string;
    secretRef: string;
    name: string;
    state: string;
  };
  expect(draft.name).toBe(name);
  expect(draft.state).toBe("DRAFT");
  expect(JSON.stringify(draft)).not.toContain(value);

  const editor = page.getByRole("dialog", { name: "Черновик секрета" });
  await expect(editor).toBeVisible();
  expect(
    await page.evaluate(
      (secret) => document.documentElement.outerHTML.includes(secret),
      value,
    ),
  ).toBe(false);
  await editor.getByRole("button", { name: "Проверить черновик" }).click();
  await expect(editor).toContainText("VALID");
  await editor
    .getByRole("button", { name: "Подготовить план влияния" })
    .click();
  const publish = editor.getByRole("button", {
    name: "Опубликовать без замены",
  });
  await expect(publish).toBeEnabled({ timeout: 30_000 });
  await publish.click();
  await expect(editor).toContainText("PUBLISHED", { timeout: 30_000 });
  const secret = await page.evaluate(async (ref) => {
    const response = await fetch(`/api/v1/runtime-secrets/${ref}`, {
      cache: "no-store",
    });
    if (!response.ok || response.headers.get("Cache-Control") !== "no-store")
      throw new Error("Secret metadata readback failed");
    return (await response.json()) as Record<string, unknown>;
  }, draft.secretRef);
  expect(secret.ref).toBe(draft.secretRef);
  expect(secret.name).toBe(name);
  expect(JSON.stringify(secret)).not.toContain(value);
  expect(leakedURLs).toEqual([]);
  const stored = await page.evaluate((secretValue) => {
    for (const storage of [window.localStorage, window.sessionStorage]) {
      for (let index = 0; index < storage.length; index += 1) {
        const key = storage.key(index);
        if (key && storage.getItem(key)?.includes(secretValue)) return true;
      }
    }
    return false;
  }, value);
  expect(stored).toBe(false);
});
