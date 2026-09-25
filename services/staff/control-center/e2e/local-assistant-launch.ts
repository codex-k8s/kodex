import { randomUUID } from "node:crypto";

import { expect, test, type Page } from "@playwright/test";

import { authenticateOwner } from "./auth-flow";
import { loadE2EAuthEnvironment } from "./environment";
import { gotoWithRetry } from "./helpers";

const environment = loadE2EAuthEnvironment();

function safeBrowserFailure(error: Error): string {
  const detail =
    /^(Cannot (?:set|read) properties of (?:null|undefined) \((?:setting|reading) '[A-Za-z0-9_]+'\))/.exec(
      error.message,
    )?.[0];
  return `${error.name}: ${detail ?? "redacted"}`;
}

test("повторно открывает применённый план и карточку запуска без повторного действия", async ({
  page,
}) => {
  const projectRef = process.env.KODEX_E2E_ASSISTANT_READBACK_PROJECT ?? "";
  const conversationRef =
    process.env.KODEX_E2E_ASSISTANT_READBACK_CONVERSATION ?? "";
  test.skip(
    !projectRef || !conversationRef,
    "Нужны точные ссылки собственной тестовой фикстуры",
  );
  const failures: string[] = [];
  page.on("pageerror", (error) => failures.push(safeBrowserFailure(error)));
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
    .locator(`[data-conversation-ref="${conversationRef}"]`)
    .last()
    .click();
  const plan = assistant.locator(".assistant-plan-card").last();
  await expect(plan).toBeVisible();
  await plan.getByRole("button", { name: "Открыть план" }).click();
  await expect(assistant.locator(".assistant-plan-editor")).toBeVisible();
  await assistant.getByRole("button", { name: "Вернуться к диалогу" }).click();
  try {
    await expect(assistant.locator(".assistant-run-card")).toBeVisible({
      timeout: 15_000,
    });
  } catch {
    throw new Error(
      `Applied plan readback card missing; browser failures: ${failures.join("; ") || "none"}`,
    );
  }
  expect(failures).toEqual([]);
});

interface Run {
  ref: string;
  version: number;
  state: string;
  source: string;
  projectRef: string;
  target: { type: string; ref: string };
  resultSummary?: string;
  safeErrorCode?: string;
}

test("помощник запрашивает переход и запускает сотрудника для недельной сводки только после плана", async ({
  page,
}) => {
  test.skip(
    process.env.KODEX_E2E_ASSISTANT_LAUNCH !== "1",
    "Модельный запуск разрешён только явно",
  );
  test.setTimeout(480_000);
  const browserFailures: string[] = [];
  let ownRunRef = "";
  page.on("pageerror", (error) =>
    browserFailures.push(safeBrowserFailure(error)),
  );
  await authenticateOwner(
    page,
    {
      username: environment.ownerUsername,
      password: environment.ownerPassword,
    },
    { mode: "local" },
  );

  const suffix = randomUUID().slice(0, 8);
  const projectName = `Локальная недельная сводка ${suffix}`;
  const agentName = `Аналитик сводки ${suffix}`;
  const project = await mutate<{ ref: string }>(page, "/api/v1/projects", {
    name: projectName,
    purpose:
      "Безопасная локальная проверка поиска исполнителя и запуска через помощника.",
    language: "ru",
  });
  const agent = await mutate<{ ref: string; state: string }>(
    page,
    `/api/v1/projects/${project.ref}/agents`,
    {
      name: agentName,
      purpose:
        "Готовить недельную сводку по текущему проекту без внешних действий.",
      roleDescription: "Тестовый аналитик только для чтения и краткого ответа.",
      initialInstructions: [
        "Ты готовишь краткую сводку только по материалам текущего проекта.",
        "Для локальной проверки ответь одной фразой: «Сводка за неделю подготовлена; изменений нет».",
        "Не вызывай инструменты, не создавай и не изменяй объекты, не делегируй работу.",
      ].join(" "),
    },
  );
  expect(agent.state).toBe("READY");
  await pinExecutableTestModel(page, agent.ref);

  try {
    await gotoWithRetry(page, "/projects");
    await page.getByRole("button", { name: "Открыть Kodex" }).click();
    let assistant = page.getByRole("dialog", { name: "Kodex" });
    await assistant
      .locator(".assistant-drawer__header")
      .getByRole("button", { name: "Новый диалог" })
      .click();
    await send(
      assistant,
      `Найди проект «${projectName}» для недельной сводки. Сейчас открыт общий список проектов. Предложи перейти в найденный проект по точной ссылке; ничего не запускай и не создавай.`,
    );
    const projectLink = assistant
      .locator(`a[href="/projects/${project.ref}"]`)
      .first();
    await expect(projectLink).toBeVisible({ timeout: 180_000 });
    await expect(assistant.locator(".assistant-plan-card")).toHaveCount(0);
    await projectLink.click();
    await expect(page).toHaveURL(
      new RegExp(`/projects/${project.ref}(?:[/?#]|$)`),
    );

    await expect(page.locator("#assistant-workspace")).toBeVisible({
      timeout: 30_000,
    });
    assistant = page.getByRole("dialog", { name: "Kodex" });
    await assistant
      .locator(".assistant-drawer__header")
      .getByRole("button", { name: "Новый диалог" })
      .click();
    await send(
      assistant,
      `В текущем проекте «${projectName}» найди сотрудника «${agentName}». Подготовь ровно один план запуска этого сотрудника для сводки за последние семь дней. Задание — только кратко ответить, без изменения данных и внешних действий. Ничего не запускай до моего подтверждения.`,
    );
    const plan = assistant.locator(".assistant-plan-card").last();
    await expect(plan).toBeVisible({ timeout: 180_000 });
    await expect(
      plan.locator(".assistant-plan-card__operations > li"),
    ).toHaveCount(1);
    await expect(plan).toContainText("Выполнить");
    await expect(plan).toContainText(agentName);
    await plan.getByRole("button", { name: "Открыть план" }).click();
    const editor = assistant.locator(".assistant-plan-editor");
    await expect(editor.locator(".assistant-plan-operation")).toHaveCount(1);
    await expect(editor.locator(".assistant-plan-friendly")).toContainText(
      agentName,
    );
    await editor.getByRole("button", { name: "Проверить ревизию" }).click();
    const apply = editor.getByRole("button", { name: "Применить атомарно" });
    await expect(apply).toBeEnabled({ timeout: 30_000 });
    await apply.click();
    await expect(editor.locator(".assistant-plan-receipt")).toBeVisible({
      timeout: 90_000,
    });
    await expect(
      editor.getByRole("button", { name: "Вернуться к диалогу" }),
    ).toBeEnabled();
    await editor.getByRole("button", { name: "Вернуться к диалогу" }).click();
    const card = assistant.locator(".assistant-run-card").last();
    try {
      await expect(card).toBeVisible({ timeout: 30_000 });
    } catch {
      const state = await assistant.evaluate((element) => ({
        chat: Boolean(element.querySelector(".assistant-chat-view")),
        plan: Boolean(element.querySelector(".assistant-plan-editor")),
        busy: element.getAttribute("aria-busy"),
        selected: Boolean(element.getAttribute("data-conversation-ref")),
      }));
      throw new Error(
        `Assistant launch result missing; state=${JSON.stringify(state)}; browser failures=${browserFailures.join("; ") || "none"}`,
      );
    }
    const runLink = card.locator(
      `a[href^="/projects/${project.ref}/runs/run_"]`,
    );
    await expect(runLink).toBeVisible({ timeout: 30_000 });
    ownRunRef = (await runLink.getAttribute("href"))?.split("/").at(-1) ?? "";
    expect(ownRunRef).toMatch(/^run_/);
    await expect
      .poll(
        async () => {
          const run = await read<Run>(page, `/api/v1/runs/${ownRunRef}`);
          if (["FAILED", "CANCELLED"].includes(run.state))
            throw new Error(
              `Assistant-launched Run ended with ${run.safeErrorCode ?? run.state}`,
            );
          return run.state;
        },
        { timeout: 180_000, intervals: [500, 1_000, 2_000] },
      )
      .toBe("SUCCEEDED");
    const completed = await read<Run>(page, `/api/v1/runs/${ownRunRef}`);
    expect(completed.projectRef).toBe(project.ref);
    expect(completed.target).toMatchObject({ type: "AGENT", ref: agent.ref });
    expect(completed.source).toBe("SYSTEM_ASSISTANT");
    expect(completed.resultSummary).not.toBe("");
    await runLink.click();
    await expect(page).toHaveURL(
      new RegExp(`/projects/${project.ref}/runs/${ownRunRef}$`),
    );
    expect(browserFailures).toEqual([]);
  } finally {
    if (ownRunRef && page.url().startsWith(environment.baseURL)) {
      const run = await read<Run>(page, `/api/v1/runs/${ownRunRef}`);
      if (!["SUCCEEDED", "FAILED", "CANCELLED"].includes(run.state))
        await mutate(
          page,
          `/api/v1/runs/${ownRunRef}/commands`,
          { action: "CANCEL" },
          run.version,
        );
    }
  }
});

interface WorkflowFixture {
  ref: string;
  version: number;
  state: string;
  validationMessages: string[];
  launchReadiness: { allowedToSubmit: boolean; reason: string };
}

test("помощник запускает опубликованный процесс только после подтверждения плана", async ({
  page,
}) => {
  const projectRef = process.env.KODEX_E2E_ASSISTANT_WORKFLOW_PROJECT ?? "";
  const agentRef = process.env.KODEX_E2E_ASSISTANT_WORKFLOW_AGENT ?? "";
  test.skip(
    process.env.KODEX_E2E_ASSISTANT_WORKFLOW !== "1" ||
      !projectRef ||
      !agentRef,
    "Процесс запускается только явно в собственной локальной фикстуре",
  );
  test.setTimeout(600_000);
  const browserFailures: string[] = [];
  let ownRunRef = "";
  page.on("pageerror", (error) =>
    browserFailures.push(safeBrowserFailure(error)),
  );
  await authenticateOwner(
    page,
    {
      username: environment.ownerUsername,
      password: environment.ownerPassword,
    },
    { mode: "local" },
  );
  const agent = await read<{ ref: string; projectRef: string; state: string }>(
    page,
    `/api/v1/agents/${agentRef}`,
  );
  expect(agent).toMatchObject({ ref: agentRef, projectRef, state: "READY" });
  const workflowName = `Локальная сводка процесса ${randomUUID().slice(0, 8)}`;
  const workflowPath = `/api/v1/projects/${projectRef}/workflows`;
  const created = await mutate<WorkflowFixture>(page, workflowPath, {
    name: workflowName,
    purpose: "Подготовить краткую безопасную сводку по тестовому проекту.",
    coordinatorAgentRef: agentRef,
    inputFields: [],
    steps: [
      {
        position: 1,
        name: "Подготовить сводку",
        purpose: "Ответить краткой фразой без изменения данных.",
        agentRef,
        parallel: false,
        parallelGroup: 0,
        timeoutSeconds: 600,
        expectedResult: "Краткий ответ о состоянии тестового проекта.",
        humanGate: false,
        gateDecisions: [],
        requiredCapabilityKeys: [],
      },
    ],
    maxConcurrency: 1,
    timeoutSeconds: 1200,
    completionCriteria: "Один краткий ответ, без внешних действий.",
  });
  expect(created.state).toBe("DRAFT");
  const commandPath = `/api/v1/workflows/${created.ref}/commands`;
  const validated = await mutate<WorkflowFixture>(
    page,
    commandPath,
    { action: "VALIDATE" },
    created.version,
  );
  expect(validated.validationMessages).toEqual([]);
  expect(validated.state).toBe("VALID");
  const published = await mutate<WorkflowFixture>(
    page,
    commandPath,
    { action: "PUBLISH" },
    validated.version,
  );
  expect(published.state).toBe("PUBLISHED");
  await expect
    .poll(
      async () =>
        (await read<WorkflowFixture>(page, `/api/v1/workflows/${created.ref}`))
          .launchReadiness.allowedToSubmit,
      { timeout: 30_000, intervals: [500, 1_000, 2_000] },
    )
    .toBe(true);

  try {
    await gotoWithRetry(page, `/projects/${projectRef}`);
    await page.getByRole("button", { name: "Открыть Kodex" }).click();
    const assistant = page.getByRole("dialog", { name: "Kodex" });
    await assistant
      .locator(".assistant-drawer__header")
      .getByRole("button", { name: "Новый диалог" })
      .click();
    await send(
      assistant,
      `Найди в текущем проекте опубликованный Процесс «${workflowName}». Предложи ровно один план LAUNCH_RUN для этого Процесса, чтобы дать краткую сводку за последние семь дней. Не создавай и не меняй объекты и ничего не запускай до моего подтверждения.`,
    );
    const plan = assistant.locator(".assistant-plan-card").last();
    await expect(plan).toBeVisible({ timeout: 180_000 });
    await expect(
      plan.locator(".assistant-plan-card__operations > li"),
    ).toHaveCount(1);
    await expect(plan).toContainText(workflowName);
    await plan.getByRole("button", { name: "Открыть план" }).click();
    const editor = assistant.locator(".assistant-plan-editor");
    await expect(editor.locator(".assistant-plan-operation")).toHaveCount(1);
    await expect(
      editor.locator(".assistant-launch-form select").first(),
    ).toHaveValue("WORKFLOW");
    await editor.getByRole("button", { name: "Проверить ревизию" }).click();
    const apply = editor.getByRole("button", { name: "Применить атомарно" });
    await expect(apply).toBeEnabled({ timeout: 30_000 });
    await apply.click();
    await expect(editor.locator(".assistant-plan-receipt")).toBeVisible({
      timeout: 90_000,
    });
    await editor.getByRole("button", { name: "Вернуться к диалогу" }).click();
    const card = assistant.locator(".assistant-run-card").last();
    await expect(card).toBeVisible({ timeout: 30_000 });
    const runLink = card.locator(
      `a[href^="/projects/${projectRef}/runs/run_"]`,
    );
    await expect(runLink).toBeVisible();
    ownRunRef = (await runLink.getAttribute("href"))?.split("/").at(-1) ?? "";
    expect(ownRunRef).toMatch(/^run_/);
    await expect
      .poll(
        async () => {
          const run = await read<Run>(page, `/api/v1/runs/${ownRunRef}`);
          if (["FAILED", "CANCELLED"].includes(run.state))
            throw new Error(
              `Assistant-launched Workflow ended with ${run.safeErrorCode ?? run.state}`,
            );
          return run.state;
        },
        { timeout: 240_000, intervals: [1_000, 2_000, 5_000] },
      )
      .toBe("SUCCEEDED");
    const completed = await read<Run>(page, `/api/v1/runs/${ownRunRef}`);
    expect(completed.projectRef).toBe(projectRef);
    expect(completed.target).toMatchObject({
      type: "WORKFLOW",
      ref: created.ref,
    });
    expect(completed.source).toBe("SYSTEM_ASSISTANT");
    expect(completed.resultSummary).not.toBe("");
    expect(browserFailures).toEqual([]);
  } finally {
    if (ownRunRef && page.url().startsWith(environment.baseURL)) {
      const run = await read<Run>(page, `/api/v1/runs/${ownRunRef}`);
      if (!["SUCCEEDED", "FAILED", "CANCELLED"].includes(run.state))
        await mutate(
          page,
          `/api/v1/runs/${ownRunRef}/commands`,
          { action: "CANCEL" },
          run.version,
        );
    }
  }
});

async function send(
  assistant: ReturnType<Page["getByRole"]>,
  prompt: string,
): Promise<void> {
  await assistant
    .getByRole("textbox", {
      name: "Опишите, что нужно настроить или запустить",
    })
    .fill(prompt);
  await assistant.getByRole("button", { name: "Отправить помощнику" }).click();
  await expect(
    assistant.getByRole("status", { name: "Kodex отвечает" }),
  ).toBeVisible({ timeout: 30_000 });
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
  let selection:
    | {
        accountRef: string;
        catalogRevision: string;
        catalogDigest: string;
        providerDefinitionKey: string;
      }
    | undefined;
  for (const account of accounts.items) {
    if (account.state !== "AUTHORIZED") continue;
    const catalog = await read<{
      catalogRevision: string;
      catalogDigest: string;
      catalogStatus?: { state: string; expiresAt?: string };
      items: Array<{
        id: string;
        available: boolean;
        providerDefinitionKey: string;
        eligibleProviderAccountRefs: string[];
      }>;
    }>(
      page,
      `/api/v1/model-capabilities?providerAccountRef=${encodeURIComponent(account.ref)}&query=${model}&pageSize=100`,
    );
    const capability = catalog.items.find(
      (item) =>
        item.id === model &&
        item.available &&
        item.eligibleProviderAccountRefs.includes(account.ref),
    );
    if (
      catalog.catalogStatus?.state !== "READY" ||
      !capability ||
      Date.parse(catalog.catalogStatus.expiresAt ?? "") <= Date.now()
    )
      continue;
    selection = {
      accountRef: account.ref,
      catalogRevision: catalog.catalogRevision,
      catalogDigest: catalog.catalogDigest,
      providerDefinitionKey: capability.providerDefinitionKey,
    };
    break;
  }
  if (!selection)
    throw new Error("No authorized account offers the local test model");
  const path = `/api/v1/agents/${agentRef}/runtime-configuration`;
  const current = await read<{
    agentVersion: number;
    configuration: { runtimeProfileRef: string };
  }>(page, path);
  const saved = await mutate<{ configuration: { model: string } }>(
    page,
    path,
    {
      runtimeProfileRef: current.configuration.runtimeProfileRef,
      model,
      providerPolicyMode: "FIXED",
      providerAccounts: [{ ...selection, weight: 1 }],
    },
    current.agentVersion,
    "PUT",
  );
  expect(saved.configuration.model).toBe(model);
}

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
