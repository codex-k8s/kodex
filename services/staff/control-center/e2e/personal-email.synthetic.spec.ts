import { expect, test, type Page } from "@playwright/test";
import type {
  EmailMailboxCredential,
  EmailMailboxOwnership,
  IntegrationConnection,
  MyEmailMailbox,
} from "../src/shared/api/generated/openapi/types.gen";
import { installSyntheticCsrf } from "./fixtures/browser-session";
import { handleSyntheticRuntimeRequest } from "./synthetic-runtime-route";

// Только browser consumer qualification: backend ответы синтетические. Native
// authority, SMTP/IMAP и PostgreSQL проверяются отдельными публичными контурами.
async function fixture(
  page: Page,
  options: {
    existing?: boolean;
    disabled?: boolean;
    malformed?: boolean;
    lostCreate?: boolean;
    lostCredential?: boolean;
  } = {},
) {
  const failures: string[] = [],
    creates: unknown[] = [],
    configures: unknown[] = [],
    credentials: unknown[] = [];
  let owner: EmailMailboxOwnership = {
    connectionRef: "int_mailbox_owner",
    connectionVersion: 3,
    deliveryKind: "MAILBOX_CONFIGURATION",
    policy: "INITIATOR_REQUIRED",
    state: "ACTIVE",
    generation: 1,
    ownerEligible: true,
  };
  let connection: IntegrationConnection = {
    ref: owner.connectionRef,
    version: 3,
    definitionKey: "email",
    credentialPolicy: "INITIATOR_REQUIRED",
    name: "Личная почта fixture",
    state: options.disabled ? "DISABLED" : "NOT_CONNECTED",
    credentialsConfigured: false,
    credentialsHint: "",
    capabilities: [],
    grants: [],
    nextActions: options.disabled ? ["OPEN", "ENABLE"] : ["OPEN", "DISABLE"],
    definitionVersion: "1.5.0",
    definitionDigest: "a".repeat(64),
    publicConfiguration: {
      from_address: "owner@example.test",
      mailbox_id: "mailbox_fixture",
    },
  };
  let exists = options.existing ?? false,
    createReceipt: MyEmailMailbox | undefined,
    credentialReceipt: EmailMailboxCredential | undefined,
    credentialReads = 0;
  const mailbox = (): MyEmailMailbox => ({
    ownership: { ...owner },
    name: connection.name,
    enabled: connection.state !== "DISABLED",
    available: false,
  });
  page.on("pageerror", (error) => failures.push(error.message));
  await installSyntheticCsrf(page.context(), "m");
  await page.route("**/*", async (route) => {
    const request = route.request(),
      url = new URL(request.url());
    if (await handleSyntheticRuntimeRequest(route, url, failures)) return;
    if (url.pathname === "/api/v1/personal-email-mailboxes") {
      if (request.method() === "GET") {
        await route.fulfill({ json: { items: exists ? [mailbox()] : [] } });
        return;
      }
      const body: unknown = request.postDataJSON();
      creates.push(body);
      expect(request.headers()["x-csrf-token"]).toBe("m".repeat(43));
      expect(request.headers()["idempotency-key"]).toBeTruthy();
      expect(Object.keys(body as object).sort()).toEqual(["name", "sender"]);
      exists = true;
      connection = {
        ...connection,
        name: (body as { name: string }).name,
        version: 1,
        state: "NOT_CONNECTED",
      };
      owner = { ...owner, connectionVersion: 1 };
      createReceipt = mailbox();
      if (options.lostCreate) {
        options.lostCreate = false;
        await route.abort();
        return;
      }
      await route.fulfill({ json: createReceipt, status: 201 });
      return;
    }
    if (url.pathname === "/api/v1/personal-email-mailboxes/mutation-receipt") {
      if (url.searchParams.get("operation") === "CREATE" && createReceipt) {
        await route.fulfill({
          json: { operation: "CREATE", mailbox: createReceipt },
        });
        return;
      }
      await route.fulfill({
        status: 404,
        json: { status: 404, code: "NOT_FOUND", retryable: false },
      });
      return;
    }
    const base = `/api/v1/integration-connections/${connection.ref}`;
    if (url.pathname === base && request.method() === "GET") {
      await route.fulfill({ json: connection });
      return;
    }
    if (url.pathname === `${base}/email-mailbox/ownership`) {
      if (request.method() === "GET") {
        const value: Partial<EmailMailboxOwnership> = { ...owner };
        if (options.malformed) delete value.ownerEligible;
        await route.fulfill({ json: value });
        return;
      }
      expect(request.headers()["if-match"]).toBe(`"${connection.version}"`);
      expect(request.headers()["x-csrf-token"]).toBe("m".repeat(43));
      if (request.method() === "PUT") {
        const body = request.postDataJSON() as {
          policy: string;
          serviceSubjectRef?: string;
        };
        configures.push(body);
        if (body.policy === "PROJECT_SHARED") {
          await route.fulfill({
            status: 403,
            json: { status: 403, code: "FORBIDDEN", retryable: false },
          });
          return;
        }
        expect(Object.keys(body)).toEqual(["policy"]);
        owner = {
          ...owner,
          generation: owner.generation + 1,
          ownerEligible: true,
          state: "ACTIVE",
        };
      } else
        owner = {
          ...owner,
          generation: owner.generation + 1,
          ownerEligible: false,
          state: "REVOKED",
        };
      connection = { ...connection, version: connection.version + 1 };
      owner = { ...owner, connectionVersion: connection.version };
      await route.fulfill({ json: owner });
      return;
    }
    if (
      url.pathname === `${base}/email-mailbox/credential` &&
      request.method() === "PUT"
    ) {
      const body = request.postDataJSON() as {
        kind: EmailMailboxCredential["kind"];
        value: string;
      };
      credentials.push(body);
      expect(Object.keys(body).sort()).toEqual(["kind", "value"]);
      expect(request.headers()["if-match"]).toBe(`"${connection.version}"`);
      connection = { ...connection, version: connection.version + 1 };
      owner = { ...owner, connectionVersion: connection.version };
      credentialReceipt = {
        connectionRef: connection.ref,
        connectionVersion: connection.version,
        generation: connection.version,
        name: "email_synthetic_descriptor",
        kind: body.kind,
      };
      if (options.lostCredential) {
        options.lostCredential = false;
        await route.abort();
        return;
      }
      await route.fulfill({ json: credentialReceipt });
      return;
    }
    if (url.pathname === `${base}/email-mailbox/credential-receipt`) {
      credentialReads++;
      if (credentialReceipt) {
        await route.fulfill({ json: credentialReceipt });
        return;
      }
      await route.fulfill({
        status: 404,
        json: { status: 404, code: "NOT_FOUND", retryable: false },
      });
      return;
    }
    if (url.pathname.startsWith("/api/")) {
      failures.push("Unexpected API path");
      await route.abort();
      return;
    }
    await route.fulfill({
      response: await route.fetch({
        url: `http://127.0.0.1:43122${url.pathname}${url.search}`,
      }),
    });
  });
  return {
    failures,
    creates,
    configures,
    credentials,
    get credentialReads() {
      return credentialReads;
    },
    disableOwner() {
      owner = { ...owner, ownerEligible: false };
    },
    get connection() {
      return connection;
    },
  };
}

test("личный mailbox: lost create ACK сверяется без повторного POST и без generic ключа", async ({
  page,
}) => {
  const state = await fixture(page, { lostCreate: true });
  await page.goto("/e2e/fixtures/personal-email.html");
  await page.getByLabel("Название", { exact: true }).fill("я".repeat(161));
  await page.getByLabel("Адрес отправителя").fill("owner@example.test");
  await expect(
    page.getByRole("button", { name: "Создать личное почтовое подключение" }),
  ).toBeDisabled();
  await page.getByLabel("Название", { exact: true }).fill("я".repeat(160));
  await page
    .getByRole("button", { name: "Создать личное почтовое подключение" })
    .click();
  await expect(
    page.getByRole("button", { name: "Проверить квитанцию" }),
  ).toBeVisible();
  expect(state.creates).toHaveLength(1);
  const stored = await page.evaluate(() =>
    JSON.stringify({ ...sessionStorage }),
  );
  expect(stored).not.toContain("owner@example.test");
  expect(stored).not.toContain("Мой новый ящик");
  await page.getByRole("button", { name: "Проверить квитанцию" }).click();
  await expect(
    page.getByText("Подключение создано.", { exact: false }),
  ).toBeVisible();
  expect(state.creates).toHaveLength(1);
  await page.getByRole("link", { name: "Настроить ящик" }).first().click();
  await expect(
    page.getByRole("heading", { name: "Владелец почтовых реквизитов" }),
  ).toBeVisible();
  await expect(page.locator('input[type="password"]')).toHaveCount(1);
  await expect(
    page.getByRole("combobox", { name: "Тип", exact: true }),
  ).toBeVisible();
  expect(state.failures).toEqual([]);
});

test("обычный self owner не получает SERVICE switch и может после отказа выбрать личный профиль", async ({
  page,
}) => {
  const state = await fixture(page, { existing: true, disabled: true });
  await page.goto(
    "/e2e/fixtures/personal-email.html?connectionRef=int_mailbox_owner",
  );
  await page.getByLabel("Режим владения").selectOption("PROJECT_SHARED");
  await page
    .getByLabel("Сервисная учётная запись", { exact: true })
    .fill("subject_service_fixture");
  await page.getByRole("button", { name: "Назначить владельца" }).click();
  await expect.poll(() => state.configures.length).toBe(1);
  await expect(
    page.getByRole("button", { name: "Назначить владельца" }),
  ).toBeEnabled();
  await page.getByLabel("Режим владения").selectOption("INITIATOR_REQUIRED");
  await page.getByRole("button", { name: "Назначить владельца" }).click();
  await expect.poll(() => state.configures.length).toBe(2);
  expect(state.configures[1]).toEqual({ policy: "INITIATOR_REQUIRED" });
  expect(state.failures).toEqual([]);
});

test("unknown owner metadata закрывает mailbox editor, не предлагает TOKEN fallback", async ({
  page,
}) => {
  const state = await fixture(page, { existing: true, malformed: true });
  await page.goto(
    "/e2e/fixtures/personal-email.html?connectionRef=int_mailbox_owner",
  );
  await expect(
    page.getByRole("heading", { name: "Владелец почтовых реквизитов" }),
  ).toBeVisible();
  await expect(page.getByRole("alert")).toBeVisible();
  await expect(page.locator('input[type="password"]')).toHaveCount(0);
  await expect(page.getByLabel("Профиль почтового провайдера")).toHaveCount(0);
  expect(state.credentials).toEqual([]);
  expect(state.failures).toEqual([]);
});

test("revoke сохраняет uncertainty старой записи credentials и не повторяет её в новом owner epoch", async ({
  page,
}) => {
  const state = await fixture(page, { existing: true, lostCredential: true });
  await page.goto(
    "/e2e/fixtures/personal-email.html?connectionRef=int_mailbox_owner",
  );
  await page.locator('input[type="password"]').fill("synthetic-app-password");
  await page.getByRole("button", { name: "Сохранить", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "Проверить результат попытки" }),
  ).toBeVisible();
  await page
    .getByRole("button", { name: "Обновить", exact: true })
    .last()
    .click();
  await expect.poll(() => state.connection.version).toBe(4);
  await page.getByRole("button", { name: "Отозвать доступ ящика" }).click();
  await expect(
    page.getByRole("button", { name: "Начать настройку нового поколения" }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Проверить результат попытки" }),
  ).toBeDisabled();
  await expect(page.locator('input[type="password"]')).toHaveCount(0);
  await page
    .getByRole("button", { name: "Начать настройку нового поколения" })
    .click();
  await expect(
    page.getByRole("button", { name: "Проверить результат попытки" }),
  ).toHaveCount(0);
  expect(state.credentials).toHaveLength(1);
  expect(state.credentialReads).toBe(0);
  expect(state.failures).toEqual([]);
});

test("Gmail app password остаётся явным PASSWORD профилем без OAuth claims и адресатов по умолчанию", async ({
  page,
}) => {
  const state = await fixture(page, { existing: true });
  await page.goto(
    "/e2e/fixtures/personal-email.html?connectionRef=int_mailbox_owner",
  );
  await page
    .getByLabel("Профиль почтового провайдера")
    .selectOption("GMAIL_APP_PASSWORD");
  await expect(
    page.getByText("Обычный пароль Google не подходит.", { exact: false }),
  ).toBeVisible();
  const output = page.getByLabel("Синтетический почтовый черновик");
  await expect(output).toContainText('"host":"smtp.gmail.com"');
  await expect(output).toContainText('"serverName":"imap.gmail.com"');
  await expect(output).toContainText('"authMethod":"PASSWORD"');
  await expect(output).toContainText('"recipients":[]');
  await page
    .getByRole("button", { name: "Заполнить политики выбранного профиля" })
    .click();
  await expect(output).toContainText(
    '"operation":"SEND","policy":"HUMAN_GATE"',
  );
  await expect(output).toContainText(
    '"operation":"DRAFT_CREATE","policy":"ALLOW"',
  );
  expect(state.configures).toEqual([]);
  expect(state.credentials).toEqual([]);
  expect(state.failures).toEqual([]);
});

test("write-only credential: lost ACK после reload восстанавливается receipt без значения и второго PUT", async ({
  page,
}) => {
  const state = await fixture(page, { existing: true, lostCredential: true });
  await page.goto(
    "/e2e/fixtures/personal-email.html?connectionRef=int_mailbox_owner",
  );
  await page.locator('input[type="password"]').fill("synthetic-app-password");
  await page.getByRole("button", { name: "Сохранить", exact: true }).click();
  await expect(page.locator('input[type="password"]')).toHaveValue("");
  await expect(
    page.getByRole("button", { name: "Проверить результат попытки" }),
  ).toBeVisible();
  expect(state.credentials).toHaveLength(1);
  expect(
    await page.evaluate(() => JSON.stringify({ ...sessionStorage })),
  ).not.toContain("synthetic-app-password");
  await page.reload();
  await page
    .getByRole("button", { name: "Проверить результат попытки" })
    .click();
  await expect.poll(() => state.credentialReads).toBe(1);
  await expect(
    page.getByRole("button", { name: "Проверить результат попытки" }),
  ).toHaveCount(0);
  expect(state.credentials).toHaveLength(1);
  state.disableOwner();
  await page
    .getByRole("button", { name: "Обновить", exact: true })
    .last()
    .click();
  await expect(page.locator('input[type="password"]')).toHaveCount(0);
  await expect(
    page.getByRole("button", { name: "Проверить SMTP/IMAP" }),
  ).toBeDisabled();
  expect(state.failures).toEqual([]);
});
