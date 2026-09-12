import { constants } from "node:fs";
import { open } from "node:fs/promises";
import {
  SessionBoundaryDiagnostics,
  sessionPreflight,
} from "./session-boundary-diagnostics";
import {
  ConsoleErrorDiagnostics,
  installConsoleErrorDiagnostics,
} from "./console-error-diagnostics";
import { createHash } from "node:crypto";

import {
  expect,
  test,
  type APIRequestContext,
  type Page,
  type Response,
} from "@playwright/test";

import {
  loadLifecycleConfiguration,
  openLifecycleJournal,
  operationOutcome,
  permittedLifecycleRequest,
  type LifecycleIdentity,
  type LifecycleOperation,
} from "./configuration-lifecycle-proof";
import { versionsFromEnvironment } from "./ui-acceptance-proof";

type Kind = "PROMPT_TEMPLATE" | "INTEGRATION_DEFINITION";
interface Revision {
  ref: string;
  revision: number;
  state:
    | "DRAFT"
    | "VALID"
    | "INVALID"
    | "PUBLISHED"
    | "SUPERSEDED"
    | "DISCARDED";
  digest: string;
}
interface Configuration {
  ref: string;
  name: string;
  kind: Kind;
  version: number;
  managedBy: "UI" | "GIT" | "SHIPPED";
  archived: boolean;
  projectRef?: string;
  currentRevision?: Revision;
}
interface History {
  configuration: Configuration;
  items: Revision[];
}

const configuration = await loadLifecycleConfiguration(
  process.env,
  versionsFromEnvironment(process.env),
);
const hash = (value: string) =>
  createHash("sha256").update(value).digest("hex");
const promptName = `${configuration.prefix}-prompt`;
const integrationName = `${configuration.prefix}-integration`;
const copyName = `${configuration.prefix}-integration-copy`;
const promptContent = `Synthetic prompt ${configuration.prefix}`;
const integrationSource = configuration.syntheticSource
  .replace("origin: SHIPPED", "origin: UI")
  .replace("name: Synthetic HTTP", `name: ${integrationName}`);

function object(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== "object" || Array.isArray(value))
    throw new Error("Invalid configuration lifecycle response");
  return value as Record<string, unknown>;
}

function parsedConfiguration(
  value: unknown,
  kind: Kind,
  name: string,
): Configuration {
  const item = object(value);
  const current =
    item.currentRevision === undefined
      ? undefined
      : parsedRevision(item.currentRevision);
  if (
    typeof item.ref !== "string" ||
    item.kind !== kind ||
    item.name !== name ||
    !Number.isSafeInteger(item.version) ||
    Number(item.version) < 1 ||
    item.managedBy !== "UI" ||
    typeof item.archived !== "boolean" ||
    (item.projectRef !== undefined &&
      (typeof item.projectRef !== "string" ||
        !/^[a-zA-Z0-9_-]{8,128}$/.test(item.projectRef)))
  )
    throw new Error("Configuration lifecycle identity mismatch");
  return {
    ref: item.ref,
    kind,
    name,
    version: Number(item.version),
    managedBy: "UI",
    archived: item.archived,
    ...(typeof item.projectRef === "string"
      ? { projectRef: item.projectRef }
      : {}),
    ...(current ? { currentRevision: current } : {}),
  };
}

function parsedRevision(value: unknown): Revision {
  const item = object(value);
  if (
    typeof item.ref !== "string" ||
    !Number.isSafeInteger(item.revision) ||
    Number(item.revision) < 1 ||
    ![
      "DRAFT",
      "VALID",
      "INVALID",
      "PUBLISHED",
      "SUPERSEDED",
      "DISCARDED",
    ].includes(String(item.state)) ||
    typeof item.digest !== "string" ||
    !/^[a-f0-9]{64}$/.test(item.digest)
  )
    throw new Error("Configuration lifecycle revision mismatch");
  return item as unknown as Revision;
}

async function history(
  request: APIRequestContext,
  configurationRef: string,
  expectedProjectRef?: string,
): Promise<History> {
  const response = await request.get(
    `/api/v1/managed-configurations/${encodeURIComponent(configurationRef)}/revisions?pageSize=30`,
    { failOnStatusCode: false, timeout: 10_000 },
  );
  try {
    if (response.status() !== 200)
      throw new Error("Configuration lifecycle readback failed");
    const body = object(await response.json());
    if (!Array.isArray(body.items))
      throw new Error("Configuration lifecycle history mismatch");
    const rawConfiguration = object(body.configuration);
    const kind = rawConfiguration.kind;
    const name = rawConfiguration.name;
    if (
      !["PROMPT_TEMPLATE", "INTEGRATION_DEFINITION"].includes(String(kind)) ||
      typeof name !== "string"
    )
      throw new Error("Configuration lifecycle history scope mismatch");
    const configuration = parsedConfiguration(
      rawConfiguration,
      kind as Kind,
      name,
    );
    if (configuration.projectRef !== expectedProjectRef)
      throw new Error("Configuration lifecycle project scope mismatch");
    return {
      configuration,
      items: body.items.map(parsedRevision),
    };
  } finally {
    await response.dispose();
  }
}

async function findConfiguration(
  request: APIRequestContext,
  kind: Kind,
  name: string,
  projectRef?: string,
): Promise<Configuration | undefined> {
  const query = new URLSearchParams({ kind, query: name, pageSize: "30" });
  if (projectRef) query.set("projectRef", projectRef);
  const response = await request.get(
    `/api/v1/managed-configurations?${query.toString()}`,
    {
      failOnStatusCode: false,
      timeout: 10_000,
    },
  );
  try {
    if (response.status() !== 200)
      throw new Error("Configuration lifecycle catalog readback failed");
    const body = object(await response.json());
    if (!Array.isArray(body.items))
      throw new Error("Configuration lifecycle catalog mismatch");
    if (body.nextPageToken !== undefined && body.nextPageToken !== "")
      throw new Error(
        "Configuration lifecycle catalog is incomplete; bounded readback is required",
      );
    const matches = body.items.filter(
      (item) => object(item).name === name && object(item).kind === kind,
    );
    if (matches.length > 1)
      throw new Error("Duplicate configuration lifecycle fixture");
    const result = matches[0]
      ? parsedConfiguration(matches[0], kind, name)
      : undefined;
    if (result && result.projectRef !== projectRef)
      throw new Error("Configuration lifecycle project scope mismatch");
    return result;
  } finally {
    await response.dispose();
  }
}

function identity(value: History): LifecycleIdentity {
  const newest = value.items.reduce<Revision | undefined>(
    (selected, item) =>
      !selected || item.revision > selected.revision ? item : selected,
    undefined,
  );
  return {
    ref: value.configuration.ref,
    version: value.configuration.version,
    ...(newest ? { revision: newest.revision } : {}),
    ...(value.configuration.currentRevision
      ? { publishedRef: value.configuration.currentRevision.ref }
      : {}),
  };
}

async function mutation(
  page: Page,
  journal: Awaited<ReturnType<typeof openLifecycleJournal>>,
  operation: LifecycleOperation,
  input: string,
  path: RegExp,
  action: () => Promise<void>,
  readback: () => Promise<LifecycleIdentity>,
): Promise<Response> {
  const sequence = await journal.intent(operation, input);
  let requestBody: string | undefined;
  const responsePromise = page.waitForResponse(
    (response) => {
      const request = response.request();
      const matches =
        request.method() === "POST" &&
        path.test(new URL(response.url()).pathname);
      if (matches) requestBody = request.postData() ?? undefined;
      return matches;
    },
    { timeout: 20_000 },
  );
  let response: Response | undefined;
  try {
    await action();
    response = await responsePromise;
    const outcome = operationOutcome(response.status());
    if (outcome !== "PASS") {
      await journal.stop(
        sequence,
        operation,
        outcome,
        undefined,
        response.status(),
        requestBody,
      );
      throw new Error("Configuration lifecycle command was not accepted");
    }
    const current = await readback();
    await journal.stop(
      sequence,
      operation,
      "PASS",
      current,
      response.status(),
      requestBody,
    );
    return response;
  } catch (error) {
    if (
      journal.pending().some((item) => item.sequence === sequence) &&
      journal.latestOutcome(sequence) !== "UNKNOWN"
    )
      await journal.stop(
        sequence,
        operation,
        "UNKNOWN",
        undefined,
        undefined,
        requestBody,
      );
    throw error;
  }
}

async function sourceEditor(page: Page) {
  const editor = page
    .locator(".configuration-editor .cm-content[contenteditable=true]")
    .first();
  await expect(editor).toBeVisible();
  return editor;
}

async function openNew(page: Page, kind: Kind, projectRef?: string) {
  const query = projectRef
    ? `?${new URLSearchParams({ projectRef }).toString()}`
    : "";
  await page.goto(`/configurations/${kind}/new${query}`, {
    waitUntil: "domcontentloaded",
  });
  const editor = page.locator(".configuration-editor");
  await expect(editor).toBeVisible();
  return editor;
}

async function projectPreflight(request: APIRequestContext) {
  const response = await request.get(
    `/api/v1/projects/${encodeURIComponent(configuration.projectRef)}`,
    { failOnStatusCode: false, timeout: 10_000 },
  );
  try {
    const body: unknown =
      response.status() === 200 ? await response.json() : undefined;
    const item = object(body);
    if (
      response.status() !== 200 ||
      item.ref !== configuration.projectRef ||
      !Number.isSafeInteger(item.version) ||
      Number(item.version) < 1 ||
      item.lifecycle !== "ACTIVE"
    )
      throw new Error("Configuration lifecycle project preflight failed");
    return {
      refSHA256: configuration.projectRefSHA256,
      status: response.status(),
      version: Number(item.version),
      lifecycle: "ACTIVE" as const,
    };
  } finally {
    await response.dispose();
  }
}

async function reconcile(
  request: APIRequestContext,
  operation: LifecycleOperation,
): Promise<LifecycleIdentity | undefined> {
  const prompt = operation.startsWith("PROMPT_");
  const name =
    operation === "INTEGRATION_COPY" || operation === "INTEGRATION_ARCHIVE"
      ? copyName
      : prompt
        ? promptName
        : integrationName;
  const kind: Kind = prompt ? "PROMPT_TEMPLATE" : "INTEGRATION_DEFINITION";
  const projectRef = prompt ? configuration.projectRef : undefined;
  const found = await findConfiguration(request, kind, name, projectRef);
  if (!found) return;
  const value = await history(request, found.ref, projectRef);
  const states = new Set(value.items.map((item) => item.state));
  const proven =
    operation.endsWith("CREATE") ||
    (operation.endsWith("VALIDATE") &&
      (states.has("VALID") || states.has("PUBLISHED"))) ||
    (operation.endsWith("PUBLISH") && !!value.configuration.currentRevision) ||
    (operation === "INTEGRATION_RESTORE" && states.has("DRAFT")) ||
    operation === "INTEGRATION_COPY" ||
    (operation === "INTEGRATION_ARCHIVE" && value.configuration.archived);
  return proven ? identity(value) : undefined;
}

test("UI lifecycle шаблона и Synthetic IntegrationDefinition без Git", async ({
  context,
  page,
}) => {
  const journal = await openLifecycleJournal(configuration);
  let diagnostics: Awaited<ReturnType<typeof open>> | undefined;
  const fixtureReadbacks: {
    kind: Kind;
    nameSHA256: string;
    present: boolean;
    version?: number;
  }[] = [];
  let projectEligibility:
    | Awaited<ReturnType<typeof projectPreflight>>
    | undefined;
  const sessionBoundary = new SessionBoundaryDiagnostics();
  const consoleErrors = new ConsoleErrorDiagnostics(configuration.baseURL);
  installConsoleErrorDiagnostics(page, consoleErrors, 0);
  const finishSessionBoundary = sessionBoundary.install(
    page,
    configuration.baseURL,
  );
  try {
    if (!configuration.resume)
      diagnostics = await open(
        `${configuration.journalPath}.diagnostics.json`,
        constants.O_WRONLY |
          constants.O_CREAT |
          constants.O_EXCL |
          constants.O_NOFOLLOW,
        0o600,
      );
    if (configuration.resume) {
      const pending = journal.pending();
      expect(pending.length).toBeGreaterThan(0);
      for (const item of pending) {
        const found = await reconcile(context.request, item.operation);
        await journal.stop(
          item.sequence,
          item.operation,
          found ? "PASS" : "UNKNOWN",
          found,
        );
      }
      return;
    }
    let blockedMutation = "";
    await context.route("**/api/v1/**", async (route) => {
      const request = route.request();
      const path = new URL(request.url()).pathname;
      const allowed =
        permittedLifecycleRequest(request.method(), path) &&
        (!configuration.readOnly ||
          ["GET", "HEAD", "OPTIONS"].includes(request.method()));
      if (!allowed) {
        blockedMutation = `${request.method()}:${hash(path)}`;
        await route.abort("blockedbyclient");
        return;
      }
      await route.continue();
    });

    const preflight = await context.request.get("/api/v1/session", {
      timeout: 10_000,
      failOnStatusCode: false,
    });
    try {
      const body: unknown =
        preflight.status() === 200 ? await preflight.json() : undefined;
      sessionBoundary.observe("PREFLIGHT", preflight.status(), body);
      sessionPreflight(preflight.status(), body);
    } finally {
      await preflight.dispose();
    }
    projectEligibility = await projectPreflight(context.request);
    // Ранее отвергнутый/неизвестный effect проверяется отдельно владельцем. Новый
    // запуск также не создаёт второй объект при случайном повторении prefix.
    for (const [kind, name] of [
      ["PROMPT_TEMPLATE", promptName],
      ["INTEGRATION_DEFINITION", integrationName],
      ["INTEGRATION_DEFINITION", copyName],
    ] as const) {
      const found = await findConfiguration(
        context.request,
        kind,
        name,
        kind === "PROMPT_TEMPLATE" ? configuration.projectRef : undefined,
      );
      fixtureReadbacks.push({
        kind,
        nameSHA256: hash(name),
        present: !!found,
        ...(found ? { version: found.version } : {}),
      });
      if (found && !configuration.readOnly)
        throw new Error(
          "Configuration lifecycle fixture already exists; readback is required",
        );
    }
    if (configuration.readOnly) return;
    const promptEditor = await openNew(
      page,
      "PROMPT_TEMPLATE",
      configuration.projectRef,
    );
    await expect(page.locator(".app-shell")).toBeVisible();
    expect(consoleErrors.failed()).toBe(false);
    await promptEditor
      .locator(".configuration-editor__fields input")
      .fill(promptName);
    await (await sourceEditor(page)).fill(promptContent);
    await mutation(
      page,
      journal,
      "PROMPT_CREATE",
      promptContent,
      /^\/api\/v1\/prompt-template-configurations\/drafts$/,
      () =>
        promptEditor
          .getByRole("button", { name: "Сохранить черновик", exact: true })
          .click(),
      async () => {
        const item = await findConfiguration(
          context.request,
          "PROMPT_TEMPLATE",
          promptName,
          configuration.projectRef,
        );
        if (!item) throw new Error("Prompt create readback is missing");
        return identity(
          await history(context.request, item.ref, configuration.projectRef),
        );
      },
    );
    const prompt = await findConfiguration(
      context.request,
      "PROMPT_TEMPLATE",
      promptName,
      configuration.projectRef,
    );
    if (!prompt) throw new Error("Prompt fixture is missing");
    await mutation(
      page,
      journal,
      "PROMPT_VALIDATE",
      promptContent,
      new RegExp(
        `^/api/v1/prompt-template-configurations/${prompt.ref}/revisions/[^/]+/validation$`,
      ),
      () =>
        promptEditor
          .getByRole("button", { name: "Проверить", exact: true })
          .click(),
      async () =>
        identity(
          await history(context.request, prompt.ref, configuration.projectRef),
        ),
    );
    const promptPublishSequence = await journal.intent(
      "PROMPT_PUBLISH",
      promptContent,
    );
    let finalPromptBody: string | undefined;
    try {
      const prepare = page.waitForResponse(
        (response) =>
          response.request().method() === "POST" &&
          /\/prompt-template-configurations\/[^/]+\/revisions\/[^/]+\/impact-plans$/.test(
            new URL(response.url()).pathname,
          ),
      );
      await promptEditor
        .getByRole("button", { name: "Опубликовать", exact: true })
        .click();
      expect((await prepare).status()).toBe(200);
      const dialog = page.getByRole("dialog", {
        name: "План публикации",
        exact: true,
      });
      await expect(dialog.locator(".publication-impact")).toHaveAttribute(
        "aria-busy",
        "false",
      );
      const publication = page.waitForResponse((response) => {
        const matches =
          response.request().method() === "POST" &&
          /\/prompt-template-configurations\/[^/]+\/revisions\/[^/]+\/publication$/.test(
            new URL(response.url()).pathname,
          );
        if (matches)
          finalPromptBody = response.request().postData() ?? undefined;
        return matches;
      });
      await dialog.locator(".publication-impact .button--primary").click();
      const response = await publication;
      const outcome = operationOutcome(response.status());
      if (outcome !== "PASS") {
        await journal.stop(
          promptPublishSequence,
          "PROMPT_PUBLISH",
          outcome,
          undefined,
          response.status(),
          finalPromptBody,
        );
        throw new Error("Prompt publication was not accepted");
      }
      const promptHistory = await history(
        context.request,
        prompt.ref,
        configuration.projectRef,
      );
      if (!promptHistory.configuration.currentRevision)
        throw new Error("Prompt publication readback is missing");
      await journal.stop(
        promptPublishSequence,
        "PROMPT_PUBLISH",
        "PASS",
        identity(promptHistory),
        response.status(),
        finalPromptBody,
      );
    } catch (error) {
      if (
        journal
          .pending()
          .some((item) => item.sequence === promptPublishSequence)
      )
        await journal.stop(
          promptPublishSequence,
          "PROMPT_PUBLISH",
          "UNKNOWN",
          undefined,
          undefined,
          finalPromptBody,
        );
      throw error;
    }
    await promptEditor
      .getByRole("button", { name: "История", exact: true })
      .click();
    await expect(
      page
        .getByRole("dialog", { name: "История", exact: true })
        .locator(".configuration-editor__revision"),
    ).not.toHaveCount(0);
    await page.keyboard.press("Escape");

    const integrationEditor = await openNew(page, "INTEGRATION_DEFINITION");
    await integrationEditor
      .locator(".configuration-editor__fields input")
      .fill(integrationName);
    await integrationEditor
      .getByRole("button", { name: "Источник", exact: true })
      .click();
    await (await sourceEditor(page)).fill(integrationSource);
    await integrationEditor
      .getByRole("button", { name: "Форма", exact: true })
      .click();
    await expect(
      integrationEditor.locator(".package-object").first(),
    ).toBeVisible();
    await integrationEditor
      .getByRole("button", { name: "Источник", exact: true })
      .click();
    await expect(await sourceEditor(page)).toContainText("SYNTHETIC_HTTP");
    await mutation(
      page,
      journal,
      "INTEGRATION_CREATE",
      integrationSource,
      /^\/api\/v1\/integration-definition-configurations\/drafts$/,
      () =>
        integrationEditor
          .getByRole("button", { name: "Сохранить черновик", exact: true })
          .click(),
      async () => {
        const item = await findConfiguration(
          context.request,
          "INTEGRATION_DEFINITION",
          integrationName,
        );
        if (!item) throw new Error("Integration create readback is missing");
        return identity(await history(context.request, item.ref));
      },
    );
    const integration = await findConfiguration(
      context.request,
      "INTEGRATION_DEFINITION",
      integrationName,
    );
    if (!integration) throw new Error("Integration fixture is missing");
    await mutation(
      page,
      journal,
      "INTEGRATION_VALIDATE",
      integrationSource,
      new RegExp(
        `^/api/v1/integration-definition-configurations/${integration.ref}/revisions/[^/]+/validation$`,
      ),
      () =>
        integrationEditor
          .getByRole("button", { name: "Проверить", exact: true })
          .click(),
      async () => identity(await history(context.request, integration.ref)),
    );
    await mutation(
      page,
      journal,
      "INTEGRATION_PUBLISH",
      integrationSource,
      new RegExp(
        `^/api/v1/integration-definition-configurations/${integration.ref}/revisions/[^/]+/publication$`,
      ),
      () =>
        integrationEditor
          .getByRole("button", { name: "Опубликовать", exact: true })
          .click(),
      async () => {
        const current = await history(context.request, integration.ref);
        if (!current.configuration.currentRevision)
          throw new Error("Integration publication readback is missing");
        return identity(current);
      },
    );
    const published = await history(context.request, integration.ref);
    const publishedRef = published.configuration.currentRevision?.ref;
    if (!publishedRef) throw new Error("Published pointer is missing");
    await integrationEditor
      .getByRole("button", { name: "История", exact: true })
      .click();
    const historyDialog = page.getByRole("dialog", {
      name: "История",
      exact: true,
    });
    await expect(
      historyDialog.locator(".configuration-editor__revision"),
    ).not.toHaveCount(0);
    await historyDialog
      .locator(".configuration-editor__revision")
      .filter({ hasText: "PUBLISHED" })
      .first()
      .click();
    await mutation(
      page,
      journal,
      "INTEGRATION_RESTORE",
      publishedRef,
      /^\/api\/v1\/integration-definition-configurations\/drafts$/,
      () =>
        integrationEditor
          .getByRole("button", {
            name: "Создать черновик из этой ревизии",
            exact: true,
          })
          .click(),
      async () => {
        const current = await history(context.request, integration.ref);
        if (
          current.configuration.currentRevision?.ref !== publishedRef ||
          !current.items.some(
            (item) =>
              item.state === "DRAFT" &&
              item.revision >
                (published.configuration.currentRevision?.revision ?? 0),
          )
        )
          throw new Error("Forward restore readback mismatch");
        return identity(current);
      },
    );
    await page.reload({ waitUntil: "domcontentloaded" });
    const restored = await history(context.request, integration.ref);
    if (restored.configuration.currentRevision?.ref !== publishedRef)
      throw new Error("Forward restore changed published pointer");
    await mutation(
      page,
      journal,
      "INTEGRATION_COPY",
      copyName,
      /^\/api\/v1\/integration-definition-configurations\/copies$/,
      async () => {
        await page
          .locator(".configuration-editor")
          .getByRole("button", { name: "Создать копию", exact: true })
          .click();
        const dialog = page.getByRole("dialog", {
          name: "Создать копию",
          exact: true,
        });
        await dialog.getByLabel("Название", { exact: true }).fill(copyName);
        await dialog
          .getByRole("button", { name: "Создать копию", exact: true })
          .click();
      },
      async () => {
        const item = await findConfiguration(
          context.request,
          "INTEGRATION_DEFINITION",
          copyName,
        );
        if (!item) throw new Error("Integration copy readback is missing");
        return identity(await history(context.request, item.ref));
      },
    );
    const copied = await findConfiguration(
      context.request,
      "INTEGRATION_DEFINITION",
      copyName,
    );
    if (!copied) throw new Error("Copied integration is missing");
    await expect(page).toHaveURL(
      new RegExp(`/configurations/INTEGRATION_DEFINITION/${copied.ref}`),
    );
    await mutation(
      page,
      journal,
      "INTEGRATION_ARCHIVE",
      copied.ref,
      new RegExp(
        `^/api/v1/integration-definition-configurations/${copied.ref}/archive$`,
      ),
      async () => {
        await page
          .locator(".configuration-editor")
          .getByRole("button", { name: "Архивировать", exact: true })
          .click();
        const dialog = page.getByRole("dialog", {
          name: "Архивировать",
          exact: true,
        });
        await dialog
          .getByRole("button", { name: "Архивировать", exact: true })
          .click();
      },
      async () => {
        const current = await history(context.request, copied.ref);
        if (!current.configuration.archived)
          throw new Error("Integration archive readback is missing");
        return identity(current);
      },
    );
    await page.locator(".current-user-menu__trigger").click();
    await page
      .locator(".current-user-menu__languages")
      .getByRole("button", { name: "EN", exact: true })
      .click();
    await expect(page.locator("html")).toHaveAttribute("lang", "en");
    await expect(
      page
        .locator(".configuration-editor")
        .getByRole("button", { name: "History", exact: true }),
    ).toBeVisible();
    await page.locator(".current-user-menu__trigger").click();
    await page
      .locator(".current-user-menu__languages")
      .getByRole("button", { name: "RU", exact: true })
      .click();
    await expect(page.locator("html")).toHaveAttribute("lang", "ru");
    expect(blockedMutation).toBe("");
    expect(consoleErrors.failed()).toBe(false);
    expect(sessionBoundary.snapshot().overflow).toBe(0);
  } finally {
    await finishSessionBoundary();
    try {
      if (diagnostics) {
        await diagnostics.writeFile(
          `${JSON.stringify({ versions: configuration.versions, timestampUTC: new Date().toISOString(), projectEligibility, fixtureReadbacks, sessionBoundary: sessionBoundary.snapshot(), consoleErrors: consoleErrors.snapshot() })}\n`,
        );
        await diagnostics.sync();
      }
    } finally {
      await diagnostics?.close();
      await journal.close();
    }
  }
});
