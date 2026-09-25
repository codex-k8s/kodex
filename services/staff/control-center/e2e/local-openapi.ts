import { randomUUID } from "node:crypto";

import { expect, test, type Page } from "@playwright/test";

import { authenticateOwner } from "./auth-flow";
import { loadE2EAuthEnvironment } from "./environment";
import { gotoWithRetry } from "./helpers";

const environment = loadE2EAuthEnvironment();
const source = `openapi: 3.1.0
info: {title: Локальная проверка OpenAPI, version: 1.0.0}
servers:
  - url: https://jsonplaceholder.typicode.com
paths:
  /posts/1:
    get:
      operationId: getHealth
      summary: Проверить чтение
      responses:
        '200': {description: OK}
  /posts:
    post:
      operationId: echoWrite
      summary: Проверить согласованную запись
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              additionalProperties: false
              properties:
                marker: {type: string, maxLength: 64}
                note: {type: string, maxLength: 64}
              required: [marker]
      responses:
        '201': {description: Created}
`;
const credentialSource = source.replace(
  "paths:\n",
  `components:
  securitySchemes:
    token:
      type: apiKey
      in: header
      name: X-Service-Key
security:
  - token: []
paths:
`,
);

interface ConfigurationResult {
  configuration: { ref: string; version: number; kind: string };
  revision: { ref: string; state: string; content: string };
}

interface Connection {
  ref: string;
  version: number;
  state: string;
}

interface ListedConnection extends Connection {
  name: string;
  definitionKey: string;
}

interface Run {
  ref: string;
  version: number;
  state: string;
  gateRefs: string[];
  safeErrorCode?: string;
}

const testRunRefs: string[] = [];

interface OwnerGate {
  ref: string;
  version: number;
  state: string;
  runRef: string;
}

interface ModelCapabilityPage {
  catalogRevision: string;
  catalogDigest: string;
  catalogStatus?: { state: string; expiresAt?: string };
  items: Array<{
    id: string;
    available: boolean;
    providerDefinitionKey: string;
    eligibleProviderAccountRefs: string[];
  }>;
}

interface AgentRuntimeView {
  agentVersion: number;
  configuration: {
    runtimeProfileRef: string;
    model: string;
  };
}

test.afterEach(async ({ page }) => {
  if (
    process.env.KODEX_E2E_GATE_READ_REF ||
    process.env.KODEX_E2E_OPENAPI_CANCEL_RUN_REF
  )
    return;
  if (page.url().startsWith(environment.baseURL)) {
    for (const ref of testRunRefs) {
      const run = await read<Run>(page, `/api/v1/runs/${ref}`);
      if (!["SUCCEEDED", "FAILED", "CANCELLED"].includes(run.state))
        await mutate(
          page,
          `/api/v1/runs/${ref}/commands`,
          { action: "CANCEL" },
          run.version,
        );
    }
    await disablePreviousLocalOpenAPIFixtures(page);
  }
});

test("локально отменяется только собственный незавершённый OpenAPI запуск", async ({
  page,
}) => {
  const runRef = process.env.KODEX_E2E_OPENAPI_CANCEL_RUN_REF;
  test.skip(!runRef, "Нужен ref только тестового OpenAPI запуска");
  if (!runRef) throw new Error("Local OpenAPI run ref is missing");
  await authenticateOwner(
    page,
    {
      username: environment.ownerUsername,
      password: environment.ownerPassword,
    },
    { mode: "local" },
  );
  const run = await read<Run>(page, `/api/v1/runs/${runRef}`);
  expect(run.state).toBe("WAITING_HUMAN");
  const cancelled = await mutate<{ run: Run }>(
    page,
    `/api/v1/runs/${runRef}/commands`,
    { action: "CANCEL" },
    run.version,
  );
  expect(cancelled.run.state).toBe("CANCELLED");
});

test("локальное чтение OpenAPI Human Gate сохраняет выбранные параметры", async ({
  page,
}) => {
  const gateRef = process.env.KODEX_E2E_GATE_READ_REF;
  test.skip(!gateRef, "Нужен ref только тестового открытого Human Gate");
  if (!gateRef) throw new Error("Local OpenAPI gate ref is missing");
  await authenticateOwner(
    page,
    {
      username: environment.ownerUsername,
      password: environment.ownerPassword,
    },
    { mode: "local" },
  );
  const gate = await read<
    OwnerGate & {
      integrationIntent?: {
        resourceScope?: { kind: string };
        effectPreview?: {
          approvalScope?: { selected?: Array<{ path: string; type: string }> };
        };
      };
    }
  >(page, `/api/v1/owner-gates/${gateRef}`);
  expect(gate.state).toBe("OPEN");
  expect(gate.integrationIntent?.resourceScope?.kind).toBe("HTTPS_RESOURCE");
  expect(
    gate.integrationIntent?.effectPreview?.approvalScope?.selected,
  ).toEqual(
    expect.arrayContaining([
      expect.objectContaining({ path: "/body/marker", type: "string" }),
    ]),
  );
});

test("локальный импорт OpenAPI закрывает private origin до создания черновика", async ({
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
  await gotoWithRetry(
    page,
    "/configurations/INTEGRATION_DEFINITION?assistantImportOpen=1",
  );
  const dialog = page.getByRole("dialog", {
    name: "Импорт интеграции из OpenAPI",
  });
  await expect(dialog).toBeVisible();
  await dialog
    .getByLabel("Контракт OpenAPI JSON или YAML")
    .fill(
      source.replace(
        "https://jsonplaceholder.typicode.com",
        "https://127.0.0.1",
      ),
    );
  const inspected = page.waitForResponse(
    (response) =>
      response.request().method() === "POST" &&
      new URL(response.url()).pathname ===
        "/api/v1/integration-definition-configurations/openapi-inspections",
  );
  await dialog.getByRole("button", { name: "Проверить контракт" }).click();
  expect((await inspected).status()).toBe(200);
  const operations = dialog.locator(".openapi-operation");
  await expect(operations).toHaveCount(2);
  for (const operation of await operations.all()) {
    await expect(operation.locator('input[type="checkbox"]')).toBeDisabled();
    await expect(operation).toContainText("SERVER_ORIGIN_UNSUPPORTED");
  }
  await expect(
    dialog.getByRole("button", { name: "Создать черновик" }),
  ).toBeDisabled();
});

test("помощник открывает защищённый импорт новой OpenAPI-интеграции", async ({
  page,
}) => {
  test.skip(
    process.env.KODEX_E2E_OPENAPI_ASSISTANT_IMPORT !== "1",
    "Реальный ответ модели запускается только явно",
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
  await gotoWithRetry(page, "/projects");
  await page.getByRole("button", { name: "Открыть Kodex" }).click();
  const assistant = page.getByRole("dialog", { name: "Kodex" });
  await assistant
    .locator(".assistant-drawer__header")
    .getByRole("button", { name: "Новый диалог" })
    .click();
  await assistant
    .getByRole("textbox", {
      name: "Опишите, что нужно настроить или запустить",
    })
    .fill(
      "У меня есть OpenAPI 3.1 контракт нового HTTPS JSON-сервиса, которого нет в каталоге. Покажи, где открыть защищённую форму импорта, чтобы я выбрал операции. Контракт и ключ в чат отправлять не буду; план и подключение пока не создавай.",
    );
  await assistant.getByRole("button", { name: "Отправить помощнику" }).click();
  const importLink = assistant.locator(
    'a[href="/configurations/INTEGRATION_DEFINITION"]',
  );
  await expect(importLink.last()).toBeVisible({ timeout: 180_000 });
  await importLink.last().click();
  await expect(page).toHaveURL(
    /\/configurations\/INTEGRATION_DEFINITION\?assistantImportOpen=1$/,
  );
  await expect(
    page.getByRole("dialog", { name: "Импорт интеграции из OpenAPI" }),
  ).toBeVisible();
});

test("локальный OpenAPI импорт, первая привязка и HTTPS test", async ({
  page,
}) => {
  test.setTimeout(
    process.env.KODEX_E2E_OPENAPI_INVOKE === "1" ? 600_000 : 240_000,
  );
  const browserFailures: string[] = [];
  page.on("pageerror", (error) => browserFailures.push(error.name));
  page.on("response", (response) => {
    if (response.status() >= 500)
      browserFailures.push(
        `${String(response.status())} ${new URL(response.url()).pathname}`,
      );
  });
  await authenticateOwner(
    page,
    {
      username: environment.ownerUsername,
      password: environment.ownerPassword,
    },
    { mode: "local" },
  );
  await disablePreviousLocalOpenAPIFixtures(page);
  if (process.env.KODEX_E2E_OPENAPI_CLEANUP_ONLY === "1") return;
  await gotoWithRetry(
    page,
    "/configurations/INTEGRATION_DEFINITION?assistantImportOpen=1",
  );
  const dialog = page.getByRole("dialog", {
    name: "Импорт интеграции из OpenAPI",
  });
  await expect(dialog).toBeVisible();
  await dialog.getByLabel("Контракт OpenAPI JSON или YAML").fill(source);
  const inspected = page.waitForResponse(
    (response) =>
      response.request().method() === "POST" &&
      new URL(response.url()).pathname ===
        "/api/v1/integration-definition-configurations/openapi-inspections",
  );
  await dialog.getByRole("button", { name: "Проверить контракт" }).click();
  expect((await inspected).status()).toBe(200);
  await dialog
    .locator(".openapi-operation")
    .filter({ hasText: "GET /posts/1" })
    .locator('input[type="checkbox"]')
    .check();
  const write = dialog
    .locator(".openapi-operation")
    .filter({ hasText: "POST /posts" });
  await write.locator('input[type="checkbox"]').check();
  await write.locator("select").last().selectOption("HUMAN_SCOPED");
  await dialog.locator("select").last().selectOption("getHealth");
  await dialog
    .getByLabel("Название интеграции")
    .fill(`Локальная проверка OpenAPI ${randomUUID().slice(0, 8)}`);
  const createdResponse = page.waitForResponse(
    (response) =>
      response.request().method() === "POST" &&
      new URL(response.url()).pathname ===
        "/api/v1/integration-definition-configurations/drafts",
  );
  await dialog.getByRole("button", { name: "Создать черновик" }).click();
  const created = await createdResponse;
  expect(created.status()).toBe(201);
  const draft = (await created.json()) as ConfigurationResult;
  expect(draft.configuration.kind).toBe("INTEGRATION_DEFINITION");
  expect(draft.revision.state).toBe("DRAFT");
  await expect(page).toHaveURL(
    new RegExp(
      `/configurations/INTEGRATION_DEFINITION/${draft.configuration.ref}$`,
    ),
  );

  const validated = await mutate<ConfigurationResult>(
    page,
    `/api/v1/integration-definition-configurations/${draft.configuration.ref}/revisions/${draft.revision.ref}/validation`,
    undefined,
    draft.configuration.version,
  );
  expect(validated.revision.state).toBe("VALID");
  const published = await mutate<ConfigurationResult>(
    page,
    `/api/v1/integration-definition-configurations/${draft.configuration.ref}/revisions/${draft.revision.ref}/publication`,
    undefined,
    validated.configuration.version,
  );
  expect(published.revision.state).toBe("PUBLISHED");

  const connectionName = `Локальный OpenAPI ${randomUUID().slice(0, 8)}`;
  const connection = await mutate<Connection>(
    page,
    "/api/v1/integration-connections",
    {
      definitionKey: "openapi-mcp",
      name: connectionName,
      publicConfiguration: { base_url: "https://jsonplaceholder.typicode.com" },
    },
  );
  const impact = await read<{
    digest: string;
    consumers: Array<{ ref: string }>;
    total: number;
  }>(
    page,
    `/api/v1/managed-configurations/${draft.configuration.ref}/revisions/${draft.revision.ref}/impact?pageSize=40`,
  );
  expect(impact.digest).toMatch(/^[a-f0-9]{64}$/);
  expect(impact.consumers.some((item) => item.ref === connection.ref)).toBe(
    false,
  );
  await page.reload({ waitUntil: "domcontentloaded" });
  await page.getByRole("button", { name: "Влияние ревизии" }).click();
  const impactDialog = page.getByRole("dialog", { name: "Влияние ревизии" });
  await expect(
    impactDialog.getByRole("button", { name: "Новое подключение" }),
  ).toBeEnabled();
  await impactDialog.getByRole("button", { name: "Новое подключение" }).click();
  await impactDialog
    .getByRole("option", { name: new RegExp(connectionName) })
    .click();
  const bindingResponse = page.waitForResponse(
    (response) =>
      response.request().method() === "POST" &&
      new URL(response.url()).pathname.endsWith("/consumer-bindings"),
  );
  await impactDialog
    .getByRole("button", { name: "Привязать подключение" })
    .click();
  const boundResponse = await bindingResponse;
  expect(boundResponse.status()).toBe(200);
  await expect(impactDialog).toHaveCount(0);

  const bound = await read<Connection>(
    page,
    `/api/v1/integration-connections/${connection.ref}`,
  );
  expect(bound.ref).toBe(connection.ref);
  expect(bound.version).toBeGreaterThanOrEqual(connection.version);
  await mutate<Connection>(
    page,
    `/api/v1/integration-connections/${connection.ref}/commands`,
    { action: "TEST" },
    bound.version,
  );
  await expect
    .poll(
      async () =>
        (
          await read<Connection>(
            page,
            `/api/v1/integration-connections/${connection.ref}`,
          )
        ).state,
      { timeout: 100_000, intervals: [500, 2_000, 5_000] },
    )
    .toBe("CONNECTED");
  if (process.env.KODEX_E2E_OPENAPI_INVOKE === "1") {
    const executable = JSON.parse(published.revision.content) as {
      spec: {
        capabilities: Array<{
          key: string;
          risk: string;
          openapi?: { operationId: string };
        }>;
      };
    };
    const readKey = executable.spec.capabilities.find(
      (item) =>
        item.openapi?.operationId === "getHealth" && item.risk === "READ",
    )?.key;
    const writeKey = executable.spec.capabilities.find(
      (item) =>
        item.openapi?.operationId === "echoWrite" && item.risk === "WRITE",
    )?.key;
    expect(readKey).toMatch(/^op\.[a-f0-9]{16}$/);
    expect(writeKey).toMatch(/^op\.[a-f0-9]{16}$/);
    if (!readKey || !writeKey)
      throw new Error("Imported OpenAPI capabilities are missing");
    await verifyImportedMCPInvocation(page, connection.ref, readKey, writeKey);
  }
  expect(browserFailures).toEqual([]);
});

test("помощник ведёт к credential-форме OpenAPI без утечки синтетического ключа", async ({
  page,
}) => {
  test.skip(
    process.env.KODEX_E2E_OPENAPI_CREDENTIAL !== "1",
    "Синтетический ключ на локальной установке используется только явно",
  );
  test.setTimeout(300_000);
  const credentialValue = `local-fixture-${randomUUID()}`;
  const connectionName = `Локальный OpenAPI ${randomUUID().slice(0, 8)}`;
  const leakedURLs: string[] = [];
  page.on("request", (request) => {
    if (request.url().includes(credentialValue))
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
  await gotoWithRetry(
    page,
    "/configurations/INTEGRATION_DEFINITION?assistantImportOpen=1",
  );
  const importDialog = page.getByRole("dialog", {
    name: "Импорт интеграции из OpenAPI",
  });
  await expect(importDialog).toBeVisible();
  await importDialog
    .getByLabel("Контракт OpenAPI JSON или YAML")
    .fill(credentialSource);
  await importDialog
    .getByRole("button", { name: "Проверить контракт" })
    .click();
  const readOperation = importDialog
    .locator(".openapi-operation")
    .filter({ hasText: "GET /posts/1" });
  await expect(readOperation).toBeVisible();
  await readOperation.locator('input[type="checkbox"]').check();
  await importDialog.locator("select").last().selectOption("getHealth");
  await importDialog
    .getByLabel("Название интеграции")
    .fill(`Локальная интеграция с ключом ${randomUUID().slice(0, 8)}`);
  const createdResponse = page.waitForResponse(
    (response) =>
      response.request().method() === "POST" &&
      new URL(response.url()).pathname ===
        "/api/v1/integration-definition-configurations/drafts",
  );
  await importDialog.getByRole("button", { name: "Создать черновик" }).click();
  const created = await createdResponse;
  expect(created.status()).toBe(201);
  const draft = (await created.json()) as ConfigurationResult;
  const validated = await mutate<ConfigurationResult>(
    page,
    `/api/v1/integration-definition-configurations/${draft.configuration.ref}/revisions/${draft.revision.ref}/validation`,
    undefined,
    draft.configuration.version,
  );
  expect(validated.revision.state).toBe("VALID");
  const published = await mutate<ConfigurationResult>(
    page,
    `/api/v1/integration-definition-configurations/${draft.configuration.ref}/revisions/${draft.revision.ref}/publication`,
    undefined,
    validated.configuration.version,
  );
  expect(published.revision.state).toBe("PUBLISHED");
  const executable = JSON.parse(published.revision.content) as {
    spec: { credential?: { secretKey: string } };
  };
  expect(executable.spec.credential?.secretKey).toBe("token");

  const connection = await mutate<Connection>(
    page,
    "/api/v1/integration-connections",
    {
      definitionKey: "openapi-mcp",
      name: connectionName,
      publicConfiguration: {
        base_url: "https://jsonplaceholder.typicode.com",
      },
    },
  );
  await page.reload({ waitUntil: "domcontentloaded" });
  await page.getByRole("button", { name: "Влияние ревизии" }).click();
  const impactDialog = page.getByRole("dialog", { name: "Влияние ревизии" });
  await impactDialog.getByRole("button", { name: "Новое подключение" }).click();
  await impactDialog
    .getByRole("option", { name: new RegExp(connectionName) })
    .click();
  await impactDialog
    .getByRole("button", { name: "Привязать подключение" })
    .click();
  await expect(impactDialog).toHaveCount(0);

  await gotoWithRetry(
    page,
    `/integrations?assistantCredentialRef=${connection.ref}`,
  );
  const credentialDialog = page
    .getByRole("dialog")
    .filter({ has: page.locator('input[type="password"]') });
  await expect(credentialDialog).toBeVisible();
  await credentialDialog
    .locator('input[type="password"]')
    .fill(credentialValue);
  const configuredResponse = page.waitForResponse(
    (response) =>
      response.request().method() === "PUT" &&
      new URL(response.url()).pathname ===
        `/api/v1/integration-connections/${connection.ref}/credential`,
  );
  await credentialDialog
    .locator('button[form="integration-form"][type="submit"]')
    .click();
  const configured = await configuredResponse;
  if (configured.status() !== 200) {
    const failure = (await configured.json()) as {
      code?: string;
      error?: { code?: string };
      problem?: { code?: string };
    };
    throw new Error(
      `Credential configuration failed: ${String(configured.status())} ${failure.code ?? failure.error?.code ?? failure.problem?.code ?? "UNKNOWN"}`,
    );
  }
  expect(
    JSON.stringify(await configured.json()).includes(credentialValue),
  ).toBe(false);
  await expect(credentialDialog).toBeHidden();
  const metadata = await read<Connection & { credentialsConfigured: boolean }>(
    page,
    `/api/v1/integration-connections/${connection.ref}`,
  );
  expect(metadata.credentialsConfigured).toBe(true);
  expect(JSON.stringify(metadata).includes(credentialValue)).toBe(false);
  expect(leakedURLs).toEqual([]);
  const stored = await page.evaluate((value) => {
    for (const storage of [window.localStorage, window.sessionStorage]) {
      for (let index = 0; index < storage.length; index += 1) {
        const key = storage.key(index);
        if (key && storage.getItem(key)?.includes(value)) return true;
      }
    }
    return false;
  }, credentialValue);
  expect(stored).toBe(false);
  const tested = await mutate<Connection>(
    page,
    `/api/v1/integration-connections/${connection.ref}/commands`,
    { action: "TEST" },
    metadata.version,
  );
  expect(["TESTING", "CONNECTED"]).toContain(tested.state);
  await expect
    .poll(
      async () =>
        (
          await read<Connection>(
            page,
            `/api/v1/integration-connections/${connection.ref}`,
          )
        ).state,
      { timeout: 100_000, intervals: [500, 2_000, 5_000] },
    )
    .toBe("CONNECTED");
});

async function read<T>(page: Page, path: string): Promise<T> {
  return page.evaluate(async (url) => {
    const response = await fetch(url);
    if (!response.ok)
      throw new Error(
        `API read failed: ${String(response.status)} ${new URL(url, location.origin).pathname}`,
      );
    return (await response.json()) as T;
  }, path);
}

async function disablePreviousLocalOpenAPIFixtures(page: Page): Promise<void> {
  let cursor = "";
  for (let index = 0; index < 10; index++) {
    const query = new URLSearchParams({ pageSize: "100" });
    if (cursor) query.set("pageToken", cursor);
    const listing = await read<{
      items: ListedConnection[];
      nextPageToken: string;
    }>(page, `/api/v1/integration-connections?${query.toString()}`);
    for (const connection of listing.items) {
      if (
        connection.state !== "DISABLED" &&
        connection.name.match(/^Локальный OpenAPI [0-9a-f]{8}$/) &&
        connection.definitionKey === "openapi-mcp"
      ) {
        const disabled = await mutate<Connection>(
          page,
          `/api/v1/integration-connections/${connection.ref}/commands`,
          { action: "DISABLE" },
          connection.version,
        );
        expect(disabled.state).toBe("DISABLED");
      }
    }
    if (!listing.nextPageToken) return;
    if (listing.nextPageToken === cursor)
      throw new Error("Integration fixture pagination did not advance");
    cursor = listing.nextPageToken;
  }
  throw new Error("Integration fixture pagination exceeded bound");
}

async function verifyImportedMCPInvocation(
  page: Page,
  connectionRef: string,
  readKey: string,
  writeKey: string,
): Promise<void> {
  const suffix = randomUUID().slice(0, 8);
  const project = await mutate<{ ref: string }>(page, "/api/v1/projects", {
    name: `Локальная проверка MCP ${suffix}`,
    purpose: "Безопасная проверка импортированной интеграции через сотрудника.",
    language: "ru",
  });
  const agent = await mutate<{ ref: string; state: string }>(
    page,
    `/api/v1/projects/${project.ref}/agents`,
    {
      name: `Исполнитель OpenAPI ${suffix}`,
      purpose: "Выполнять только точные вызовы тестовой интеграции.",
      roleDescription: "Локальный исполнитель проверки MCP и Human Gate.",
      initialInstructions: [
        "В каждом задании сначала вызови get_integration_catalog с точными connection_ref и capability_key.",
        "Из найденного гранта возьми definition_version, definition_digest и input_schema_sha256.",
        "После этого вызови invoke_integration с этими полями и указанным input ровно столько раз, сколько требует задание.",
        "Не используй shell, curl и прямой HTTP.",
        "После результата кратко сообщи итог и заверши работу.",
      ].join(" "),
    },
  );
  expect(agent.state).toBe("READY");
  await pinExecutableTestModel(page, agent.ref);
  let connection = await read<Connection>(
    page,
    `/api/v1/integration-connections/${connectionRef}`,
  );
  expect(connection.state).toBe("CONNECTED");
  connection = await mutate<Connection>(
    page,
    `/api/v1/integration-connections/${connectionRef}/grants`,
    { capabilityKey: readKey, agentRef: agent.ref, enabled: true },
    connection.version,
  );
  await grantScopedOpenAPIThroughUI(
    page,
    connectionRef,
    agent.ref,
    writeKey,
    suffix,
  );
  connection = await read<Connection>(
    page,
    `/api/v1/integration-connections/${connectionRef}`,
  );

  const run = async (
    title: string,
    capabilityKey: string,
    input: Record<string, unknown>,
    secondInput?: Record<string, unknown>,
  ): Promise<Run> => {
    const created = await mutate<{ run: Run }>(page, "/api/v1/runs", {
      projectRef: project.ref,
      targetRef: agent.ref,
      targetType: "AGENT",
      title,
      task: [
        "Сначала найди точный грант через get_integration_catalog, затем вызови invoke_integration для:",
        JSON.stringify({
          connection_ref: connectionRef,
          capability_key: capabilityKey,
          input,
        }),
        ...(secondInput
          ? [
              "После результата первого вызова выполни второй invoke_integration для:",
              JSON.stringify({
                connection_ref: connectionRef,
                capability_key: capabilityKey,
                input: secondInput,
              }),
            ]
          : []),
        "Не меняй эти значения, скопируй обязательные version/digest/schema из каталога и заверши ответ.",
      ].join("\n"),
    });
    testRunRefs.push(created.run.ref);
    return created.run;
  };
  const readRun = await run(`Локальный OpenAPI READ ${suffix}`, readKey, {});
  await waitForRun(page, readRun.ref, "SUCCEEDED");
  await expectToolCalls(page, readRun.ref, 1);

  const writeRun = await run(
    `Локальный OpenAPI WRITE ${suffix}`,
    writeKey,
    {
      body: { marker: `kodex-local-${suffix}`, note: "first" },
    },
    {
      body: { marker: `kodex-local-${suffix}`, note: "second" },
    },
  );
  let gate: OwnerGate | undefined;
  await expect
    .poll(
      async () => {
        const current = await read<Run>(page, `/api/v1/runs/${writeRun.ref}`);
        if (["FAILED", "CANCELLED"].includes(current.state))
          throw new Error(
            `Run ended before Human Gate: ${current.safeErrorCode ?? current.state}`,
          );
        for (const gateRef of current.gateRefs) {
          const candidate = await read<OwnerGate>(
            page,
            `/api/v1/owner-gates/${gateRef}`,
          );
          if (candidate.runRef === writeRun.ref && candidate.state === "OPEN") {
            gate = candidate;
            return true;
          }
        }
        return false;
      },
      { timeout: 180_000, intervals: [500, 1_000, 2_000] },
    )
    .toBe(true);
  if (!gate) throw new Error("OpenAPI Human Gate was not opened");
  const gateRef = gate.ref;
  await gotoWithRetry(page, `/decisions?gateRef=${gateRef}`);
  const decision = page.locator(".decision-detail");
  await expect(decision).toBeVisible();
  await expect(decision.locator(".decision-approval-scope")).toContainText(
    "/body/marker",
  );
  const resolution = page.waitForResponse(
    (response) =>
      response.request().method() === "POST" &&
      new URL(response.url()).pathname ===
        `/api/v1/owner-gates/${gateRef}/resolution`,
  );
  await decision.getByRole("button", { name: "Одобрить", exact: true }).click();
  expect((await resolution).status()).toBe(200);
  await expect
    .poll(
      async () =>
        (await read<OwnerGate>(page, `/api/v1/owner-gates/${gateRef}`)).state,
    )
    .toBe("APPROVED");
  await waitForRun(page, writeRun.ref, "SUCCEEDED");
  await expectToolCalls(page, writeRun.ref, 2);
  const finalRun = await read<Run>(page, `/api/v1/runs/${writeRun.ref}`);
  expect(finalRun.gateRefs).toEqual([gateRef]);
}

async function grantScopedOpenAPIThroughUI(
  page: Page,
  connectionRef: string,
  agentRef: string,
  capabilityKey: string,
  suffix: string,
): Promise<void> {
  const connection = await read<Connection & { name: string }>(
    page,
    `/api/v1/integration-connections/${connectionRef}`,
  );
  await gotoWithRetry(page, "/integrations");
  await page.getByRole("tab", { name: /^Разрешения/ }).click();
  const panel = page.locator(".grant-panel");
  const choose = async (label: string, query: string): Promise<void> => {
    await panel.getByRole("button", { name: label, exact: true }).click();
    const popover = page.getByRole("dialog", { name: label, exact: true });
    await popover.getByRole("combobox").fill(query);
    await popover.getByRole("option", { name: new RegExp(query) }).click();
  };
  await choose("Подключение", connection.name);
  await choose("Проект", `Локальная проверка MCP ${suffix}`);
  await choose("Получатель разрешения", `Исполнитель OpenAPI ${suffix}`);
  await choose("Возможность", "Проверить согласованную запись");
  await panel
    .locator(".approval-scope-option")
    .filter({ hasText: "/body/marker" })
    .locator('input[type="checkbox"]')
    .check();
  const created = page.waitForResponse(
    (response) =>
      response.request().method() === "POST" &&
      new URL(response.url()).pathname ===
        `/api/v1/integration-connections/${connectionRef}/grants`,
  );
  await panel.getByRole("button", { name: "Выдать разрешение" }).click();
  expect((await created).status()).toBe(200);
  const updated = await read<
    Connection & {
      grants: Array<{
        agentRef?: string;
        capabilityKey: string;
        approvalScopePaths: string[];
      }>;
    }
  >(page, `/api/v1/integration-connections/${connectionRef}`);
  expect(updated.grants).toEqual(
    expect.arrayContaining([
      expect.objectContaining({
        agentRef,
        capabilityKey,
        approvalScopePaths: ["/body/marker"],
      }),
    ]),
  );
  await page.reload({ waitUntil: "domcontentloaded" });
  await page.getByRole("tab", { name: /^Разрешения/ }).click();
  await choose("Подключение", connection.name);
  await expect(
    panel
      .locator(".grant-list .entity-row")
      .filter({
        hasText: `Исполнитель OpenAPI ${suffix}`,
      })
      .filter({ hasText: "Проверить согласованную запись" }),
  ).toContainText("/body/marker");
}

async function pinExecutableTestModel(
  page: Page,
  agentRef: string,
): Promise<void> {
  const model = "gpt-5.6-sol";
  const accounts = await read<{ items: Array<{ ref: string; state: string }> }>(
    page,
    "/api/v1/provider-accounts?pageSize=100",
  );
  let accountRef = "";
  let exact: ModelCapabilityPage | undefined;
  let providerDefinitionKey = "";
  for (const account of accounts.items) {
    if (account.state !== "AUTHORIZED") continue;
    const candidate = await read<ModelCapabilityPage>(
      page,
      `/api/v1/model-capabilities?providerAccountRef=${encodeURIComponent(account.ref)}&query=${encodeURIComponent(model)}&pageSize=100`,
    );
    const capability = candidate.items.find(
      (item) =>
        item.id === model &&
        item.available &&
        item.eligibleProviderAccountRefs.includes(account.ref),
    );
    if (candidate.catalogStatus?.state !== "READY" || !capability) continue;
    accountRef = account.ref;
    exact = candidate;
    providerDefinitionKey = capability.providerDefinitionKey;
    break;
  }
  if (!accountRef || !exact)
    throw new Error("No authorized account offers the local test model");
  expect(exact.catalogStatus?.state).toBe("READY");
  expect(Date.parse(exact.catalogStatus?.expiresAt ?? "")).toBeGreaterThan(
    Date.now(),
  );
  expect(exact.catalogRevision).toMatch(/^mcat_[a-f0-9]{64}$/);
  expect(exact.catalogDigest).toMatch(/^[a-f0-9]{64}$/);
  const path = `/api/v1/agents/${encodeURIComponent(agentRef)}/runtime-configuration`;
  const current = await read<AgentRuntimeView>(page, path);
  let saved: AgentRuntimeView | undefined;
  try {
    saved = await mutate<AgentRuntimeView>(
      page,
      path,
      {
        runtimeProfileRef: current.configuration.runtimeProfileRef,
        model,
        providerPolicyMode: "FIXED",
        providerAccounts: [
          {
            accountRef,
            weight: 1,
            catalogRevision: exact.catalogRevision,
            catalogDigest: exact.catalogDigest,
            providerDefinitionKey,
          },
        ],
      },
      current.agentVersion,
      "PUT",
    );
  } catch (writeError) {
    // Не повторяем PUT с неопределённым исходом после смены сети браузера.
    for (let attempt = 0; attempt < 6; attempt += 1) {
      try {
        const observed = await read<AgentRuntimeView>(page, path);
        if (
          observed.agentVersion > current.agentVersion &&
          observed.configuration.model === model
        ) {
          saved = observed;
          break;
        }
      } catch {
        // Только безопасное чтение до восстановления соединения.
      }
      await page.waitForTimeout(500);
    }
    if (!saved) throw writeError;
  }
  expect(saved.configuration.model).toBe(model);
}

async function waitForRun(
  page: Page,
  runRef: string,
  expected: string,
): Promise<void> {
  await expect
    .poll(
      async () => {
        const run = await read<Run>(page, `/api/v1/runs/${runRef}`);
        if (
          ["SUCCEEDED", "FAILED", "CANCELLED"].includes(run.state) &&
          run.state !== expected
        )
          throw new Error(`Run ended with ${run.safeErrorCode ?? run.state}`);
        return run.state;
      },
      { timeout: 180_000, intervals: [500, 1_000, 2_000] },
    )
    .toBe(expected);
}

async function expectToolCalls(
  page: Page,
  runRef: string,
  count: number,
): Promise<void> {
  const events = await read<{
    items: Array<{
      messageKind?: string;
      toolCall?: {
        tool: string;
        state: string;
        auditRef: string;
        safeResult?: string;
      };
    }>;
  }>(page, `/api/v1/runs/${runRef}/events?afterSequence=0&limit=500`);
  const calls = events.items.filter(
    (event) =>
      event.messageKind === "TOOL_CALL" &&
      event.toolCall?.tool === "invoke_integration",
  );
  expect(calls).toHaveLength(count);
  const invocationRefs = new Set<string>();
  const inputDigests = new Set<string>();
  for (const call of calls) {
    expect(call.toolCall?.state).toBe("SUCCEEDED");
    expect(call.toolCall?.auditRef).not.toBe("");
    const result = JSON.parse(call.toolCall?.safeResult ?? "null") as {
      invocationRef?: string;
      state?: string;
      inputSHA256?: string;
    } | null;
    expect(result?.state).toBe("SUCCEEDED");
    expect(result?.invocationRef).toMatch(/^inv_/);
    expect(result?.inputSHA256).toMatch(/^[a-f0-9]{64}$/);
    invocationRefs.add(result?.invocationRef ?? "");
    inputDigests.add(result?.inputSHA256 ?? "");
  }
  expect(invocationRefs.size).toBe(count);
  expect(inputDigests.size).toBe(count);
}

async function mutate<T>(
  page: Page,
  path: string,
  body?: unknown,
  version?: number,
  method: "POST" | "PUT" = "POST",
): Promise<T> {
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
      const headers: Record<string, string> = {
        Accept: "application/json",
        "Content-Type": "application/json",
        "Idempotency-Key": input.key,
        "X-CSRF-Token": decodeURIComponent(csrf),
      };
      if (input.version !== undefined)
        headers["If-Match"] = `"${String(input.version)}"`;
      const response = await fetch(input.path, {
        method: input.method,
        headers,
        body: input.body === undefined ? undefined : JSON.stringify(input.body),
      });
      const decoded = (await response.json()) as Record<string, unknown>;
      return {
        status: response.status,
        code: typeof decoded.code === "string" ? decoded.code : "UNKNOWN",
        body: response.ok ? decoded : undefined,
      };
    },
    { path, body, version, method, key: randomUUID() },
  );
  if (result.status < 200 || result.status >= 300 || !result.body)
    throw new Error(
      `API mutation failed: ${String(result.status)} ${result.code}`,
    );
  return result.body as T;
}
