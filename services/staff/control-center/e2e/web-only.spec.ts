import {
  type Download,
  type Locator,
  type Page,
  type Request,
  type Response,
  type Route,
} from "@playwright/test";
import { execFile } from "node:child_process";
import { readFile } from "node:fs/promises";
import { resolve } from "node:path";
import { promisify } from "node:util";

import { loadE2EEnvironment } from "./environment";
import {
  discoveryMode,
  loadDiscoveryRefs,
  saveDiscoveryRefs,
  type DiscoveryRefs,
} from "./discovery-state";
import { expect, test } from "./fixtures";
import {
  assertNoDuplicateGraphNodes,
  createAgent,
  ensureAgentCapability,
  expectPageHeading,
  expectRunState,
  gotoWithRetry,
  launchAgent,
  publishAgent,
  routeRef,
  waitForConnected,
  waitForTerminalSuccess,
} from "./helpers";

const environment = loadE2EEnvironment();
const execFileAsync = promisify(execFile);
const projectName = `${environment.resourcePrefix} — отдел продаж`;
const coordinatorName = `${environment.resourcePrefix} — координатор продаж`;
const analystName = `${environment.resourcePrefix} — аналитик лидов`;
const writerName = `${environment.resourcePrefix} — автор предложений`;
const workflowName = `${environment.resourcePrefix} — квалификация лида`;
const uploadedFileName = `${environment.resourcePrefix}-lead-context.txt`;
const automationName = `${environment.resourcePrefix} — ежечасная проверка лидов`;
const automationTask =
  "Ответь точно одной строкой: KODEX_AUTOMATION_E2E_OK. Не используй файлы, внешние источники, интеграции, плагины и запросы пользовательского ввода.";
const automationEditedTask =
  "Ответь точно одной строкой: KODEX_AUTOMATION_E2E_UPDATED_OK. Не используй файлы, внешние источники, интеграции, плагины и запросы пользовательского ввода.";
const runtimeEnvironmentName = `${environment.resourcePrefix} — среда E2E`;
const runtimeOverlay = [
  'model_reasoning_effort = "high"',
  'personality = "pragmatic"',
  "allow_login_shell = false",
  "",
  "[history]",
  'persistence = "none"',
].join("\n");
const accessRoleName = `${environment.resourcePrefix} — точечный запуск сотрудника`;
const retryableProviderTurnResults = new Map([
  ["PROVIDERRESULTUNVERIFIABLE", "i18n:PROVIDER_RESULT_UNVERIFIABLE"],
  ["PROVIDERRESULTUNKNOWN", "i18n:PROVIDER_RESULT_UNKNOWN"],
  [
    "PROVIDERRESPONSEINVALIDPROVIDERRESULTUNVERIFIABLE",
    "PROVIDER_RESPONSE_INVALID / i18n:PROVIDER_RESULT_UNVERIFIABLE",
  ],
]);

const initialRefs = loadDiscoveryRefs(environment.resourcePrefix);
let projectRef = initialRefs.projectRef ?? "";
let coordinatorRef = initialRefs.coordinatorRef ?? "";
let analystRef = initialRefs.analystRef ?? "";
let automationRef = initialRefs.automationRef ?? "";
let writerRef = initialRefs.writerRef ?? "";
let workflowRef = initialRefs.workflowRef ?? "";
let firstRunRef = initialRefs.firstRunRef ?? "";
let continuationRunRef = initialRefs.continuationRunRef ?? "";
let instructionRunRef = initialRefs.instructionRunRef ?? "";
let publishedInstructionRef = initialRefs.publishedInstructionRef ?? "";
let runtimeEnvironmentRef = initialRefs.runtimeEnvironmentRef ?? "";
let scheduledRunRef = initialRefs.scheduledRunRef ?? "";
let workflowRunRef = initialRefs.workflowRunRef ?? "";
let uploadedArtifactRef = initialRefs.uploadedArtifactRef ?? "";

async function openKodex(page: Page, newConversation = false): Promise<void> {
  await page.getByRole("button", { name: "Открыть Kodex" }).click();
  const dialog = page.getByRole("dialog", { name: "Kodex" });
  await expect(dialog).toBeVisible();
  if (!newConversation) return;
  const createButton = dialog.getByRole("button", {
    name: "Новый диалог",
    exact: true,
  });
  await expect(createButton).toBeEnabled();
  const [created] = await Promise.all([
    page.waitForResponse(
      (response) =>
        response.request().method() === "POST" &&
        new URL(response.url()).pathname === "/api/v1/assistant-conversations",
    ),
    createButton.click(),
  ]);
  expect(created.status(), await created.text()).toBe(201);
  const conversation = (await created.json()) as { ref?: string };
  expect(conversation.ref).toMatch(/^cnv_[A-Za-z0-9_-]+$/);
  await expect(dialog).toHaveAttribute(
    "data-conversation-ref",
    conversation.ref ?? "",
  );
  await expect(dialog).toHaveAttribute("aria-busy", "false");
  await expect(dialog.locator("article.assistant-message")).toHaveCount(0);
}

async function closeKodex(page: Page): Promise<void> {
  const dialog = page.getByRole("dialog", { name: "Kodex" });
  await dialog.getByRole("button", { name: "Закрыть" }).click();
  await expect(dialog).toHaveCount(0);
}

async function requestLatestKodexPlan(
  page: Page,
  prompt: string,
  expectedText: string,
): Promise<Locator> {
  const dialog = page.getByRole("dialog", { name: "Kodex" });
  const composer = dialog.getByRole("textbox", {
    name: "Опишите, что нужно настроить или запустить",
  });

  for (let attempt = 1; attempt <= 2; attempt += 1) {
    await composer.fill(prompt);
    const appendResponse = page.waitForResponse((response) => {
      const request = response.request();
      return (
        request.method() === "POST" &&
        /^\/api\/v1\/assistant-conversations\/[^/]+\/turns$/.test(
          new URL(response.url()).pathname,
        )
      );
    });
    await dialog.getByRole("button", { name: "Отправить помощнику" }).click();
    const appended = await appendResponse;
    expect(appended.status(), await appended.text()).toBe(202);
    const conversation = (await appended.json()) as {
      turns?: Array<{
        ref?: string;
        role?: string;
        sequence?: number;
        content?: string;
      }>;
    };
    const userTurn = conversation.turns
      ?.filter((turn) => turn.role === "USER" && turn.content === prompt)
      .toSorted((left, right) => (right.sequence ?? 0) - (left.sequence ?? 0))
      .at(0);
    expect(userTurn?.ref).toMatch(/^trn_[A-Za-z0-9_-]+$/);
    expect(
      Number.isSafeInteger(userTurn?.sequence) && (userTurn?.sequence ?? 0) > 0,
    ).toBe(true);
    const currentUserMessage = dialog.locator(
      `article.assistant-message--user[data-turn-ref="${userTurn?.ref ?? ""}"]`,
    );
    await expect(currentUserMessage).toHaveCount(1);
    const currentAssistantMessage = dialog.locator(
      `article.assistant-message--assistant[data-turn-sequence="${String((userTurn?.sequence ?? 0) + 1)}"]`,
    );
    await expect(currentAssistantMessage).toHaveCount(1, {
      timeout: 120_000,
    });

    const outcome = await waitForAssistantPlanAttempt(
      page,
      currentAssistantMessage,
      expectedText,
    );
    if (outcome.planCard) return outcome.planCard;

    const failedResult = outcome.failedResult ?? "UNKNOWN";
    const retryableResult = retryableProviderResult(failedResult);
    if (!retryableResult) {
      throw new Error(
        `System assistant turn failed without retry: ${failedResult}`,
      );
    }
    test.info().annotations.push({
      type: "provider-transient-retry",
      description: `Provider turn attempt ${String(attempt)} failed with ${retryableResult}; retrying through a fresh user turn`,
    });
    if (attempt === 2) {
      throw new Error(
        `Retryable provider failure persisted after two user turns: ${retryableResult}`,
      );
    }
  }

  throw new Error("System assistant plan attempt ended without an outcome");
}

async function waitForAssistantPlanAttempt(
  page: Page,
  assistantMessage: Locator,
  expectedText: string,
): Promise<{ failedResult?: string; planCard?: Locator }> {
  const deadline = Date.now() + 120_000;
  const matchingPlan = assistantMessage
    .locator(".assistant-plan-card")
    .filter({ hasText: expectedText })
    .first();
  while (Date.now() < deadline) {
    if ((await matchingPlan.count()) > 0 && (await matchingPlan.isVisible())) {
      return { planCard: matchingPlan };
    }

    if ((await assistantMessage.count()) > 0) {
      const state = await assistantMessage
        .locator(".status-badge[data-state]")
        .first()
        .getAttribute("data-state");
      const content = (
        await assistantMessage.locator(".safe-markdown").innerText()
      ).trim();
      if (["FAILED", "BLOCKED", "CANCELLED"].includes(state ?? "")) {
        return { failedResult: content };
      }
      if (state === "COMPLETED") {
        throw new Error(
          `System assistant completed without the expected plan: ${content}`,
        );
      }
    }
    await page.waitForTimeout(250);
  }
  throw new Error(
    `System assistant did not produce a terminal turn or plan containing ${expectedText}`,
  );
}

function retryableProviderResult(renderedResult: string): string | undefined {
  const normalized = renderedResult
    .normalize("NFKC")
    .replace(/[^A-Za-z]/g, "")
    .toUpperCase();
  return retryableProviderTurnResults.get(normalized);
}

async function applyLatestKodexPlan(
  page: Page,
  prompt: string,
  expectedText: string,
): Promise<void> {
  const dialog = page.getByRole("dialog", { name: "Kodex" });
  const planCard = await requestLatestKodexPlan(page, prompt, expectedText);
  await planCard.getByRole("button", { name: "Открыть план" }).click();
  const validate = dialog.getByRole("button", { name: "Проверить ревизию" });
  if ((await validate.count()) > 0) {
    const validation = page.waitForResponse(
      (response) =>
        response.request().method() === "POST" &&
        response.url().includes("/api/v1/assistant-plans/") &&
        response.url().endsWith("/validation"),
    );
    await validate.click();
    expect((await validation).status()).toBe(200);
  }
  const apply = dialog.getByRole("button", { name: "Применить атомарно" });
  const application = page.waitForResponse(
    (response) =>
      response.request().method() === "POST" &&
      response.url().includes("/api/v1/assistant-plans/") &&
      response.url().endsWith("/application"),
  );
  await apply.click();
  expect((await application).status()).toBe(200);
  await expect(apply).toHaveCount(0);
}

interface ExercisedAttachment {
  readonly fileName: string;
  readonly marker: string;
  readonly ref: string;
}

async function exerciseAttachmentComposer(
  page: Page,
  composer: Locator,
  uploadPath: string,
  surface: string,
): Promise<ExercisedAttachment> {
  await expect(composer).toBeVisible();
  const normalizedSurface = surface.replace(/[^a-z0-9-]/g, "-");
  const failedName = `${environment.resourcePrefix}-${normalizedSurface}-retry.txt`;
  const droppedName = `${environment.resourcePrefix}-${normalizedSurface}-drop.txt`;
  const finalName = `${environment.resourcePrefix}-${normalizedSurface}-input.txt`;
  const marker = `KODEX_E2E_${normalizedSurface.toUpperCase().replaceAll("-", "_")}_${environment.resourcePrefix}`;
  let rejected = false;
  const rejectFirstUpload = async (route: Route): Promise<void> => {
    const request = route.request();
    if (
      !rejected &&
      request.method() === "POST" &&
      new URL(request.url()).pathname === uploadPath &&
      request.headers()["x-file-name"] === failedName
    ) {
      rejected = true;
      await route.fulfill({
        status: 429,
        contentType: "application/problem+json",
        body: JSON.stringify({
          type: "https://kodex.invalid/problems/local-e2e-upload-retry",
          title: "Local E2E upload retry fixture",
          status: 429,
          detail: "The disposable local E2E rejected the first upload attempt.",
          code: "LOCAL_E2E_UPLOAD_RETRY",
        }),
      });
      return;
    }
    await route.continue();
  };

  await page.route("**/api/v1/**", rejectFirstUpload);
  try {
    const rejectedUpload = waitForArtifactUpload(page, uploadPath, failedName);
    await composer.locator('input[type="file"]').setInputFiles({
      name: failedName,
      mimeType: "text/plain",
      buffer: Buffer.from(`retry fixture for ${surface}`, "utf8"),
    });
    expect((await rejectedUpload).status()).toBe(429);
    const failedItem = composer
      .locator(".attachment-composer__item")
      .filter({ hasText: failedName });
    await expect(failedItem).toHaveClass(/attachment-composer__item--failed/);
    await page.unroute("**/api/v1/**", rejectFirstUpload);
    const retryResponse = waitForArtifactUpload(page, uploadPath, failedName);
    await failedItem
      .getByRole("button", {
        name: `Повторить загрузку файла «${failedName}»`,
      })
      .click();
    const retriedUpload = await retryResponse;
    expect(retriedUpload.status(), await retriedUpload.text()).toBe(201);
    await expect(
      failedItem.locator(".attachment-composer__ready"),
    ).toBeVisible();
    await failedItem
      .getByRole("button", {
        name: `Убрать загруженный файл «${failedName}» из вложений`,
      })
      .click();
    await expect(failedItem).toHaveCount(0);
  } finally {
    await page.unroute("**/api/v1/**", rejectFirstUpload);
  }
  expect(rejected, `retry fixture was not used on ${surface}`).toBe(true);

  const droppedResponse = waitForArtifactUpload(page, uploadPath, droppedName);
  const dataTransfer = await page.evaluateHandle(
    ({ content, fileName }) => {
      const transfer = new DataTransfer();
      transfer.items.add(
        new File([content], fileName, {
          type: "text/plain",
          lastModified: 1_700_000_000_000,
        }),
      );
      return transfer;
    },
    { content: `drop fixture for ${surface}`, fileName: droppedName },
  );
  try {
    await composer.dispatchEvent("dragenter", { dataTransfer });
    await expect(composer).toHaveClass(/attachment-composer--dragging/);
    await composer.dispatchEvent("drop", { dataTransfer });
  } finally {
    await dataTransfer.dispose();
  }
  expect((await droppedResponse).status()).toBe(201);
  const droppedItem = composer
    .locator(".attachment-composer__item")
    .filter({ hasText: droppedName });
  await expect(
    droppedItem.locator(".attachment-composer__ready"),
  ).toBeVisible();
  await droppedItem
    .getByRole("button", {
      name: `Убрать загруженный файл «${droppedName}» из вложений`,
    })
    .click();
  await expect(droppedItem).toHaveCount(0);

  const finalResponse = waitForArtifactUpload(page, uploadPath, finalName);
  await composer.locator('input[type="file"]').setInputFiles({
    name: finalName,
    mimeType: "text/plain",
    buffer: Buffer.from(`${marker}\n`, "utf8"),
  });
  const response = await finalResponse;
  expect(response.status(), await response.text()).toBe(201);
  const artifact = (await response.json()) as { ref?: string };
  expect(artifact.ref).toMatch(/^art_[A-Za-z0-9_-]+$/);
  const finalItem = composer
    .locator(".attachment-composer__item")
    .filter({ hasText: finalName });
  await expect(finalItem.locator(".attachment-composer__ready")).toBeVisible();
  return { fileName: finalName, marker, ref: artifact.ref ?? "" };
}

function waitForArtifactUpload(
  page: Page,
  uploadPath: string,
  fileName: string,
): Promise<Response> {
  return page.waitForResponse((response) => {
    const request = response.request();
    return (
      request.method() === "POST" &&
      new URL(response.url()).pathname === uploadPath &&
      request.headers()["x-file-name"] === fileName
    );
  });
}

interface VisualBox {
  readonly bottom: number;
  readonly height: number;
  readonly left: number;
  readonly right: number;
  readonly top: number;
  readonly width: number;
}

async function visualBox(locator: Locator, label: string): Promise<VisualBox> {
  const box = await locator.boundingBox();
  if (!box) throw new Error(`${label} is not visible`);
  return {
    bottom: box.y + box.height,
    height: box.height,
    left: box.x,
    right: box.x + box.width,
    top: box.y,
    width: box.width,
  };
}

function expectNear(
  actual: number,
  expected: number,
  label: string,
  tolerance = 2,
): void {
  expect(
    Math.abs(actual - expected),
    `${label}: expected ${String(expected)} +/- ${String(tolerance)}, received ${String(actual)}`,
  ).toBeLessThanOrEqual(tolerance);
}

function expectNoIntersection(
  left: { readonly box: VisualBox; readonly label: string },
  right: { readonly box: VisualBox; readonly label: string },
): void {
  const horizontal =
    Math.min(left.box.right, right.box.right) -
    Math.max(left.box.left, right.box.left);
  const vertical =
    Math.min(left.box.bottom, right.box.bottom) -
    Math.max(left.box.top, right.box.top);
  expect(
    horizontal <= 0.5 || vertical <= 0.5,
    `${left.label} intersects ${right.label}: ${JSON.stringify({ horizontal, vertical })}`,
  ).toBe(true);
}

async function expectInsideViewport(
  page: Page,
  locator: Locator,
  label: string,
): Promise<VisualBox> {
  const viewport = page.viewportSize();
  if (!viewport) throw new Error("browser viewport is unavailable");
  const box = await visualBox(locator, label);
  expect(box.left, `${label} left edge`).toBeGreaterThanOrEqual(-0.5);
  expect(box.top, `${label} top edge`).toBeGreaterThanOrEqual(-0.5);
  expect(box.right, `${label} right edge`).toBeLessThanOrEqual(
    viewport.width + 0.5,
  );
  expect(box.bottom, `${label} bottom edge`).toBeLessThanOrEqual(
    viewport.height + 0.5,
  );
  return box;
}

test.describe("web-only fresh installation", () => {
  test.describe.configure({ mode: discoveryMode ? "default" : "serial" });

  test("первый вход, горячий помощник и первый Проект", async ({ page }) => {
    await gotoWithRetry(page, "/onboarding");
    const preflight = await page.evaluate(async () => {
      const [bootstrapResponse, assistantResponse] = await Promise.all([
        fetch("/api/v1/bootstrap"),
        fetch("/api/v1/system-assistant"),
      ]);
      const bootstrap = bootstrapResponse.ok
        ? ((await bootstrapResponse.json()) as { onboardingComplete?: boolean })
        : undefined;
      const assistant = assistantResponse.ok
        ? ((await assistantResponse.json()) as { ref?: string })
        : undefined;
      return {
        assistantRef: assistant?.ref ?? "",
        assistantStatus: assistantResponse.status,
        bootstrapStatus: bootstrapResponse.status,
        onboardingComplete: bootstrap?.onboardingComplete ?? false,
      };
    });
    expect(preflight.bootstrapStatus).toBe(200);
    expect(preflight.assistantStatus).toBe(200);
    expect(preflight.assistantRef).toMatch(/^agt_[A-Za-z0-9_-]+$/);

    if (preflight.onboardingComplete) {
      await expect(page).toHaveURL(/\/$/);
      await expectPageHeading(page, /Добрый день/);
      await openKodex(page);
    } else {
      await expectPageHeading(page, "Настроим Kodex");
      await expect(
        page.getByRole("heading", { name: "Системный помощник готов" }),
      ).toBeVisible();
      await expect(
        page.getByText("Внешние интеграции не нужны для начала работы"),
      ).toBeVisible();
      await expect(
        page
          .locator("#main-content")
          .getByText("Готов к команде", { exact: true }),
      ).toBeVisible();
      await page.getByRole("button", { name: "Начать с помощником" }).click();
      await expect(page.getByRole("dialog", { name: "Kodex" })).toBeVisible();
    }

    await page.reload({ waitUntil: "domcontentloaded" });
    await expect(page.getByRole("dialog", { name: "Kodex" })).toBeVisible();

    if (discoveryMode && projectRef) {
      await gotoWithRetry(page, `/projects/${projectRef}`);
      await expectPageHeading(page, projectName);
      const currentUser = page.getByRole("button", {
        name: /owner.*роль: Владелец/i,
      });
      await expect(currentUser).toBeVisible();
      await expect(currentUser).toHaveAttribute(
        "aria-label",
        /owner.*роль: Владелец/i,
      );
      await waitForConnected(page);
      return;
    }

    const prompt = [
      `Создай один Проект с точным названием «${projectName}».`,
      "Назначение: квалификация входящих лидов и подготовка коммерческих предложений.",
      "Проект не связан с Git, Kubernetes, Mattermost или другой внешней интеграцией.",
      "Не создавай другие объекты.",
    ].join(" ");
    await applyLatestKodexPlan(page, prompt, projectName);

    await closeKodex(page);

    await gotoWithRetry(page, "/projects");
    const projectLink = page.getByRole("link", {
      name: new RegExp(projectName),
    });
    await expect(projectLink).toBeVisible();
    await projectLink.click();
    await expect(page).toHaveURL(/\/projects\/[^/]+$/);
    projectRef = routeRef(page, "projects");
    persistRefs();
    const currentUser = page.getByRole("button", {
      name: /owner.*роль: Владелец/i,
    });
    await expect(currentUser).toBeVisible();
    await expect(currentUser).toHaveAttribute(
      "aria-label",
      /owner.*роль: Владелец/i,
    );
    await waitForConnected(page);
  });

  test("глобальный и проектный Kodex принимают вложения через полный composer lifecycle", async ({
    page,
  }) => {
    requireRefs("projectRef");
    await gotoWithRetry(page, "/projects");
    await openKodex(page, true);
    let dialog = page.getByRole("dialog", { name: "Kodex" });
    await exerciseAttachmentComposer(
      page,
      dialog.locator(".attachment-composer"),
      "/api/v1/artifacts",
      "kodex-global",
    );
    await dialog.getByRole("button", { name: "Закрыть" }).click();
    await expect(dialog).toHaveCount(0);

    await gotoWithRetry(page, `/projects/${projectRef}`);
    await openKodex(page, true);
    dialog = page.getByRole("dialog", { name: "Kodex" });
    await exerciseAttachmentComposer(
      page,
      dialog.locator(".attachment-composer"),
      `/api/v1/projects/${projectRef}/artifacts`,
      "kodex-project",
    );
    await dialog.getByRole("button", { name: "Закрыть" }).click();
    await expect(dialog).toHaveCount(0);
  });

  test("ИИ-сотрудник выполняет первый запуск и отдаёт файл", async ({
    page,
    browserDiagnostics,
  }) => {
    requireRefs("projectRef");
    if (!discoveryMode || !coordinatorRef) {
      coordinatorRef = await createAgent(page, projectRef, {
        name: coordinatorName,
        purpose: "Координировать квалификацию лидов и собирать итоговый ответ.",
        role: "Руководитель процесса продаж, который распределяет работу между специалистами.",
        instructions:
          "Работай только с данными задания. Отвечай по-русски. При запросе результата создавай безопасный текстовый файл.",
      });
      persistRefs();
      await publishAgent(page);
    }
    await ensureAgentCapability(page, projectRef, coordinatorRef, /Файлы/);
    await ensureAuthorizedProviderAffinity(page, coordinatorRef);

    firstRunRef = await launchAgent(
      page,
      "Для нового B2B-лида используй код ALPHA-482. Подготовь краткий план квалификации и создай файл lead-plan.txt с итогом.",
    );
    persistRefs();
    await expectRunState(page, /В очереди|Выполняется/);
    await waitForTerminalSuccess(page);
    const usageBeforeReload = await readRunUsage(page, firstRunRef);
    assertValidMeasuredUsage(usageBeforeReload);
    const renderedUsage = page.getByLabel("Использование токенов");
    await expect(renderedUsage).toBeVisible();
    await expect(renderedUsage).toContainText(
      new Intl.NumberFormat("ru-RU").format(usageBeforeReload.totalTokens),
    );
    await gotoWithRetry(page, page.url());
    await waitForConnected(page);
    await expectRunState(page, "Завершён");
    expect(await readRunUsage(page, firstRunRef)).toEqual(usageBeforeReload);
    const runTools = page.getByRole("toolbar", {
      name: "Инструменты запуска",
    });
    await runTools.getByRole("button", { name: "Контекст узла" }).click();
    const nodeContext = page.getByRole("dialog", { name: "Контекст узла" });
    await expect(nodeContext).toContainText("lead-plan.txt");
    await nodeContext.getByRole("button", { name: "Закрыть" }).first().click();
    await runTools
      .getByRole("button", { name: "Ход работы", exact: true })
      .click();
    const activity = page.getByRole("dialog", { name: "Ход работы" });
    await expect(activity).toBeVisible();
    const artifact = activity
      .locator(".run-file-event")
      .filter({ hasText: "lead-plan.txt" })
      .first();
    await expect(artifact).toBeVisible();
    const artifactTraffic: string[] = [];
    const recordRequest = (request: Request): void => {
      const url = new URL(request.url());
      if (!url.pathname.startsWith("/api/v1/artifacts/")) return;
      artifactTraffic.push(
        `request:${request.method()}:${url.pathname}${url.search}`,
      );
    };
    const recordResponse = (response: Response): void => {
      const url = new URL(response.url());
      if (!url.pathname.startsWith("/api/v1/artifacts/")) return;
      artifactTraffic.push(
        `response:${String(response.status())}:${response.request().method()}:${url.pathname}${url.search}`,
      );
    };
    const recordRequestFailure = (request: Request): void => {
      const url = new URL(request.url());
      if (!url.pathname.startsWith("/api/v1/artifacts/")) return;
      artifactTraffic.push(
        `requestfailed:${request.method()}:${url.pathname}:${request.failure()?.errorText ?? "unknown"}`,
      );
    };
    page.on("request", recordRequest);
    page.on("response", recordResponse);
    page.on("requestfailed", recordRequestFailure);
    let download: Download;
    try {
      const [artifactContent, browserDownload] = await Promise.all([
        page.waitForResponse((response) => {
          const url = new URL(response.url());
          return (
            response.request().method() === "GET" &&
            url.pathname.startsWith("/api/v1/artifacts/") &&
            url.pathname.endsWith("/content")
          );
        }),
        page.waitForEvent("download"),
        artifact.getByRole("button", { name: "Скачать" }).click(),
      ]);
      expect(artifactContent.status(), await artifactContent.text()).toBe(200);
      download = browserDownload;
    } catch (error) {
      const visibleProblem = await page
        .locator(".problem-notice")
        .allTextContents()
        .catch(() => [] as string[]);
      throw new Error(
        [
          error instanceof Error ? error.message : String(error),
          `artifact traffic: ${artifactTraffic.join(" | ") || "none"}`,
          `visible problem: ${visibleProblem.join(" | ") || "none"}`,
        ].join("\n"),
      );
    } finally {
      page.off("request", recordRequest);
      page.off("response", recordResponse);
      page.off("requestfailed", recordRequestFailure);
    }
    expect(download.suggestedFilename()).toBe("lead-plan.txt");

    const continuation = activity.locator("form.run-continuation");
    await expect(continuation).toBeVisible();
    const initialSessionRef = await readRunSessionRef(page, firstRunRef);
    const continuationAttachment = await exerciseAttachmentComposer(
      page,
      continuation.locator(".attachment-composer"),
      `/api/v1/projects/${projectRef}/artifacts`,
      "session-continuation",
    );
    await continuation
      .getByLabel("Дополнительное задание")
      .fill(
        [
          "В этом продолжении приложен новый файл, которого не было в исходном turn.",
          `Найди manifest.json нового AttachmentSet, затем прочитай файл ${continuationAttachment.fileName}.`,
          "Верни одной строкой фактический маркер из файла, имя файла, код лида из предыдущего turn и слово continuation-manifest-ok.",
          "Не угадывай маркер и не используй внешние источники.",
        ].join(" "),
      );
    const continuationResponsePromise = page.waitForResponse((response) => {
      const url = new URL(response.url());
      return (
        response.request().method() === "POST" &&
        url.pathname ===
          `/api/v1/sessions/${encodeURIComponent(initialSessionRef)}/turns`
      );
    });
    await continuation
      .getByRole("button", { name: "Отправить", exact: true })
      .click();
    const continuationResponse = await continuationResponsePromise;
    expect(
      continuationResponse.status(),
      await mutationFailureDiagnostic(continuationResponse, page),
    ).toBe(201);
    const continuationAttachmentSet = await readRequestAttachmentSet(
      page,
      continuationResponse,
    );
    expect(
      continuationAttachmentSet.items.map((item) => item.artifactRef),
    ).toEqual([continuationAttachment.ref]);
    const continuationWorkspace = (await continuationResponse.json()) as {
      run?: { ref?: string };
    };
    continuationRunRef = continuationWorkspace.run?.ref ?? "";
    expect(continuationRunRef).toMatch(/^run_[A-Za-z0-9_-]+$/);
    expect(continuationRunRef).not.toBe(firstRunRef);
    persistRefs();
    await expect(page).toHaveURL(new RegExp(`/runs/${continuationRunRef}$`));
    await waitForTerminalSuccess(page);
    await expect(page.locator("#main-content")).toContainText("ALPHA-482");
    await expect(page.locator("#main-content")).toContainText(
      continuationAttachment.marker,
    );
    await expect(page.locator("#main-content")).toContainText(
      continuationAttachment.fileName,
    );
    await expect(page.locator("#main-content")).toContainText(
      "continuation-manifest-ok",
    );
    const continuationSessionRef = await readRunSessionRef(
      page,
      continuationRunRef,
    );
    expect(continuationSessionRef).toBe(initialSessionRef);

    await browserDiagnostics.withExpectedNetworkInterruption(page, () =>
      gotoWithRetry(page, "/projects"),
    );
    await expect(page).toHaveURL(/\/projects$/);
  });

  test("история инструкций позволяет откатиться к опубликованной версии", async ({
    page,
  }) => {
    requireRefs("projectRef", "coordinatorRef");
    await gotoWithRetry(
      page,
      `/projects/${projectRef}/agents/${coordinatorRef}`,
    );
    await waitForAgentVersionReadback(page, coordinatorRef);
    await page.getByRole("tab", { name: "Инструкции" }).click();
    const instructionsPanel = page.locator("#agent-panel-instructions");
    const history = instructionsPanel.locator(".instruction-history");
    await expect.poll(() => history.locator("li").count()).toBeGreaterThan(0);
    const initialVersionCount = await history.locator("li").count();
    const originalInstructions = await page.evaluate(async (agentRef) => {
      const response = await fetch(
        `/api/v1/agents/${encodeURIComponent(agentRef)}`,
      );
      if (!response.ok)
        throw new Error(`agent read failed: ${String(response.status)}`);
      const body = (await response.json()) as {
        publishedInstructions?: { content?: string };
      };
      return body.publishedInstructions?.content ?? "";
    }, coordinatorRef);
    expect(originalInstructions).not.toBe("");
    const updatedInstructions = `${originalInstructions}\nВторая опубликованная версия для проверки контролируемого отката.`;
    const instructionsEditor = instructionsPanel.getByRole("textbox", {
      name: "Инструкции",
      exact: true,
    });
    await instructionsEditor.fill(updatedInstructions);
    const saveResponse = page.waitForResponse(
      (response) =>
        response.request().method() === "POST" &&
        new URL(response.url()).pathname ===
          `/api/v1/agents/${coordinatorRef}/instruction-drafts`,
    );
    await instructionsPanel
      .getByRole("button", { name: "Сохранить черновик", exact: true })
      .click();
    const savedDraft = await saveResponse;
    expect(
      savedDraft.status(),
      await mutationFailureDiagnostic(savedDraft, page, {
        kind: "agent",
        ref: coordinatorRef,
      }),
    ).toBe(201);
    const validateResponse = page.waitForResponse(
      (response) =>
        response.request().method() === "POST" &&
        new URL(response.url()).pathname ===
          `/api/v1/agents/${coordinatorRef}/instruction-commands`,
    );
    await instructionsPanel
      .getByRole("button", { name: "Проверить инструкции" })
      .click();
    expect((await validateResponse).status()).toBe(200);
    const publishResponse = page.waitForResponse(
      (response) =>
        response.request().method() === "POST" &&
        new URL(response.url()).pathname ===
          `/api/v1/agents/${coordinatorRef}/instruction-commands`,
    );
    await instructionsPanel
      .getByRole("button", { name: "Опубликовать инструкции" })
      .click();
    expect((await publishResponse).status()).toBe(200);

    await expect(history.locator("li")).toHaveCount(initialVersionCount + 1);
    await expect(history).toContainText("Текущая");
    const rollbackButton = history
      .getByRole("button", {
        name: "Вернуть опубликованную версию",
        exact: true,
      })
      .first();
    const previousRevision = rollbackButton.locator("xpath=ancestor::li");
    await rollbackButton.click();
    const confirmation = previousRevision.locator(
      ".instruction-history__confirm",
    );
    const rollbackResponsePromise = page.waitForResponse(
      (response) =>
        response.request().method() === "POST" &&
        new URL(response.url()).pathname ===
          `/api/v1/agents/${coordinatorRef}/instruction-commands`,
    );
    await confirmation
      .getByRole("button", {
        name: "Вернуть опубликованную версию",
        exact: true,
      })
      .click();
    const rollbackResponse = await rollbackResponsePromise;
    const authoritativeAgentVersion = await page.evaluate(async (agentRef) => {
      const response = await fetch(
        `/api/v1/agents/${encodeURIComponent(agentRef)}`,
      );
      const body = (await response.json()) as { version: number };
      return body.version;
    }, coordinatorRef);
    expect(
      rollbackResponse.status(),
      `${await rollbackResponse.text()} request=${rollbackResponse.request().headers()["if-match"] ?? "missing"} authoritative=${String(authoritativeAgentVersion)}`,
    ).toBe(200);

    await expect(history.locator("li")).toHaveCount(initialVersionCount + 2);
    await expect(history.locator("li").first()).toContainText("Текущая");
    await expect(instructionsEditor).toHaveText(originalInstructions);
    const publishedAfterRollback = await page.evaluate(async (agentRef) => {
      const response = await fetch(
        `/api/v1/agents/${encodeURIComponent(agentRef)}`,
      );
      if (!response.ok)
        throw new Error(`agent read failed: ${String(response.status)}`);
      const body = (await response.json()) as {
        publishedInstructions?: { ref?: string };
      };
      return body.publishedInstructions?.ref ?? "";
    }, coordinatorRef);
    expect(publishedAfterRollback).toMatch(/^ins_[A-Za-z0-9_-]+$/);
    publishedInstructionRef = publishedAfterRollback;
    instructionRunRef = await launchAgent(
      page,
      "Подтверди одним коротким предложением, что применяешь текущую опубликованную версию инструкций.",
    );
    persistRefs();
    await waitForTerminalSuccess(page);
  });

  test("помощник создаёт сотрудника типизированным действием и оставляет аудит", async ({
    page,
  }) => {
    requireRefs("projectRef");
    const createdByAssistant = !discoveryMode || !analystRef;
    if (createdByAssistant) {
      await gotoWithRetry(page, `/projects/${projectRef}`);
      await openKodex(page, true);
      const prompt = [
        `В текущем Проекте создай одного ИИ-сотрудника с точным именем «${analystName}».`,
        "Назначение: анализировать качество лидов.",
        "Роль: аналитик продаж.",
        "Инструкции: оценивай факты, отмечай допущения и отвечай по-русски.",
        "Не запускай его и не меняй другие объекты.",
      ].join(" ");
      await applyLatestKodexPlan(page, prompt, analystName);
      await closeKodex(page);

      await gotoWithRetry(page, `/projects/${projectRef}/agents`);
      const analystLink = page.getByRole("link", {
        name: new RegExp(analystName),
      });
      await expect(analystLink).toBeVisible();
      await analystLink.click();
      await expect(page).toHaveURL(/\/agents\/[^/]+$/);
      analystRef = routeRef(page, "agents");
      persistRefs();
      await publishAgent(page);
    } else {
      await gotoWithRetry(page, `/projects/${projectRef}/agents/${analystRef}`);
      await expectPageHeading(page, analystName);
    }

    if (createdByAssistant) {
      await gotoWithRetry(
        page,
        `/administration/audit?projectRef=${encodeURIComponent(projectRef)}`,
      );
      await expect(page.getByRole("table")).toContainText(
        "system_assistant.create_agent",
      );
      await expect(page.getByRole("table")).toContainText(analystName);
    }

    await gotoWithRetry(page, "/");
    await openKodex(page);
    await expect(page.getByRole("dialog", { name: "Kodex" })).toBeVisible();
    await expect(
      page.getByRole("button", { name: /Удалить|Архивировать|Отключить/ }),
    ).toHaveCount(0);
  });

  test("контекстный Kodex показывает полный редактируемый план без скрытых изменений", async ({
    page,
  }) => {
    requireRefs("projectRef");
    await gotoWithRetry(page, `/projects/${projectRef}`);
    await openKodex(page, true);
    const dialog = page.getByRole("dialog", { name: "Kodex" });
    await expect(dialog).toContainText(projectName);
    const purpose =
      "Квалификация входящих лидов с явной проверкой полноты коммерческого предложения.";
    const prompt = `Для текущего Проекта измени только назначение на «${purpose}». Не меняй и не создавай другие объекты.`;
    const planCard = await requestLatestKodexPlan(page, prompt, projectName);
    await expect(planCard).toContainText(purpose);
    await planCard.getByRole("button", { name: "Открыть план" }).click();

    const editor = dialog.locator(".assistant-plan-editor");
    await expect(editor).toContainText(
      "Скрытых изменений нет. План применяется одной транзакцией",
    );
    await expect(editor.getByLabel("Что изменит план")).not.toHaveValue("");
    const operations = editor.locator(".assistant-plan-operation");
    await expect(operations).toHaveCount(1);
    const operation = operations.first();
    await expect(operation).toContainText(/update/i);
    await expect(operation.getByLabel("Название операции")).not.toHaveValue("");
    await expect(
      operation.getByLabel("Описание и последствия"),
    ).not.toHaveValue("");
    const target = operation.getByRole("group", { name: "Объект" });
    await expect(
      target.getByRole("textbox", { name: "Название объекта" }),
    ).toHaveValue(projectName);
    await expect(operation.getByLabel("Текущее состояние")).not.toHaveValue("");
    const parameters = operation.getByLabel("Явные параметры операции, JSON");
    const after = operation.getByLabel("Состояние после операции, JSON");
    expect(JSON.parse(await parameters.inputValue())).toMatchObject({
      purpose,
    });
    expect(JSON.parse(await after.inputValue())).toMatchObject({ purpose });

    const rejection = page.waitForResponse(
      (response) =>
        response.request().method() === "POST" &&
        new URL(response.url()).pathname.endsWith("/rejection"),
    );
    await editor.getByRole("button", { name: "Отклонить" }).click();
    expect((await rejection).status()).toBe(200);
  });

  test("runtime сотрудника публикует policy, config.toml overlay и окружение", async ({
    page,
  }) => {
    requireRefs("projectRef", "coordinatorRef", "analystRef");
    if (!discoveryMode || !runtimeEnvironmentRef) {
      await gotoWithRetry(page, `/projects/${projectRef}/environments/new`);
      await expectPageHeading(page, "Новое окружение");
      await page.getByLabel("Название").fill(runtimeEnvironmentName);
      await page
        .getByLabel("Описание")
        .fill("Несекретное окружение для проверки следующей RuntimeRevision.");
      await page.getByRole("button", { name: "Образ и инструменты" }).click();
      await page
        .getByRole("button", {
          name: "Exact image revision и digest",
          exact: true,
        })
        .click();
      const imagePicker = page.getByRole("dialog", {
        name: "Exact image revision и digest",
      });
      const imageOption = imagePicker.getByRole("option").first();
      await expect(imageOption).toBeVisible();
      await imageOption.click();
      await page.getByRole("tab", { name: "Переменные", exact: true }).click();
      await page.getByRole("button", { name: "Добавить переменную" }).click();
      await page.getByLabel("Имя переменной").fill("E2E_MODE");
      await page.getByLabel("Несекретное значение").fill("redesign");
      const creation = page.waitForResponse(
        (response) =>
          response.request().method() === "POST" &&
          new URL(response.url()).pathname ===
            `/api/v1/projects/${projectRef}/runtime-environments`,
      );
      await page.getByRole("button", { name: "Создать", exact: true }).click();
      expect((await creation).status()).toBe(201);
      await expect(page).toHaveURL(/\/environments\/[^/]+$/);
      runtimeEnvironmentRef = routeRef(page, "environments");
      persistRefs();
      await page.getByRole("tab", { name: "Переменные", exact: true }).click();
      await expect(page.getByLabel("Имя переменной")).toHaveValue("E2E_MODE");
    }

    await gotoWithRetry(
      page,
      `/projects/${projectRef}/agents/${coordinatorRef}`,
    );
    const runtimePath = `/api/v1/agents/${coordinatorRef}/runtime-configuration`;
    const runtimeResponse = Promise.race([
      page.waitForResponse(
        (response) =>
          response.request().method() === "GET" &&
          new URL(response.url()).pathname === runtimePath,
      ),
      page
        .waitForEvent("requestfailed", {
          predicate: (request) =>
            request.method() === "GET" &&
            new URL(request.url()).pathname === runtimePath,
        })
        .then((request) => {
          throw new Error(
            `Runtime request failed: ${request.failure()?.errorText ?? "unknown"}`,
          );
        }),
    ]);
    await page.getByRole("tab", { name: "Runtime", exact: true }).click();
    expect((await runtimeResponse).status()).toBe(200);
    const runtimePanel = page.locator("#agent-panel-runtime");
    await expect(
      runtimePanel.getByRole("heading", {
        name: "Модель и среда выполнения",
      }),
    ).toBeVisible();

    const providerPicker = runtimePanel.getByRole("button", {
      name: "Выберите провайдера",
    });
    const modelPicker = runtimePanel.getByRole("button", {
      name: "Выберите модель",
    });
    const runtimeProfilePicker = runtimePanel.getByRole("button", {
      name: "Выберите runtime-профиль",
    });
    await expect(providerPicker).toBeVisible();
    await expect(modelPicker).toBeVisible();
    await expect(runtimeProfilePicker).toBeVisible();
    await expect(providerPicker.locator("strong")).not.toHaveText("");
    await expect(modelPicker.locator("strong")).not.toHaveText("");
    await expect(runtimeProfilePicker.locator("strong")).not.toHaveText("");
    const providerName = (
      await providerPicker.locator("strong").innerText()
    ).trim();
    const modelName = (await modelPicker.locator("strong").innerText()).trim();

    const policy = runtimePanel.getByLabel("Политика учётных записей");
    const policyBefore = await policy.inputValue();
    const policyAfter = "LEAST_USED";
    if (policyBefore !== policyAfter) {
      await policy.selectOption(policyAfter);
      const runtimePublication = page.waitForResponse(
        (response) =>
          response.request().method() === "PUT" &&
          new URL(response.url()).pathname ===
            `/api/v1/agents/${coordinatorRef}/runtime-configuration`,
      );
      await runtimePanel
        .getByRole("button", { name: "Сохранить runtime" })
        .click();
      const publicationResponse = await runtimePublication;
      const publicationProblem =
        publicationResponse.status() === 200
          ? undefined
          : ((await publicationResponse.json()) as {
              code?: string;
              detail?: string;
            });
      expect(
        publicationResponse.status(),
        JSON.stringify({
          ifMatch: publicationResponse.request().headers()["if-match"],
          code: publicationProblem?.code,
          detail: publicationProblem?.detail,
        }),
      ).toBe(200);
    }
    await expect(policy).toHaveValue(policyAfter);

    const overlayEditor = runtimePanel.getByRole("textbox", {
      name: "Overlay config.toml",
    });
    const readOverlayState = async (): Promise<{
      draftContent?: string;
      draftState?: string;
      publishedContent: string;
    }> =>
      page.evaluate(async (agentRef) => {
        const response = await fetch(
          `/api/v1/agents/${encodeURIComponent(agentRef)}/runtime-configuration`,
        );
        if (!response.ok) {
          throw new Error(
            `runtime overlay readback failed: ${String(response.status)}`,
          );
        }
        const body = (await response.json()) as {
          draftOverlay?: { content: string; state: string };
          publishedOverlay: { content: string };
        };
        return {
          draftContent: body.draftOverlay?.content,
          draftState: body.draftOverlay?.state,
          publishedContent: body.publishedOverlay.content,
        };
      }, coordinatorRef);
    let overlayState = await readOverlayState();
    if (overlayState.publishedContent !== runtimeOverlay) {
      if (overlayState.draftContent !== runtimeOverlay) {
        await overlayEditor.fill(runtimeOverlay);
        const draftCreation = page.waitForResponse(
          (response) =>
            response.request().method() === "POST" &&
            new URL(response.url()).pathname ===
              `/api/v1/agents/${coordinatorRef}/config-overlay-drafts`,
        );
        await runtimePanel
          .getByRole("button", { name: "Сохранить черновик" })
          .click();
        expect((await draftCreation).status()).toBe(201);
        overlayState = await readOverlayState();
      }
      if (overlayState.draftState !== "VALID") {
        const validation = page.waitForResponse(
          (response) =>
            response.request().method() === "POST" &&
            new URL(response.url()).pathname.endsWith(
              "/config-overlay-drafts/validation",
            ),
        );
        await runtimePanel
          .getByRole("button", { name: "Проверить TOML" })
          .click();
        expect((await validation).status()).toBe(200);
      }
      const publication = page.waitForResponse(
        (response) =>
          response.request().method() === "POST" &&
          new URL(response.url()).pathname.endsWith(
            "/config-overlay-drafts/publication",
          ),
      );
      await runtimePanel
        .getByRole("button", { name: "Опубликовать overlay" })
        .click();
      expect((await publication).status()).toBe(200);
    }
    const effectiveConfig = runtimePanel.getByRole("textbox", {
      name: "Итоговый effective config",
    });
    await expect(effectiveConfig).toContainText(
      'model_reasoning_effort = "high"',
    );

    const environmentResponse = page.waitForResponse(
      (response) =>
        response.request().method() === "GET" &&
        new URL(response.url()).pathname ===
          `/api/v1/agents/${coordinatorRef}/runtime-configuration`,
    );
    await page
      .getByRole("tab", { name: "Рабочее окружение", exact: true })
      .click();
    expect((await environmentResponse).status()).toBe(200);
    const environmentPanel = page.locator("#agent-panel-environment");
    await expect(
      environmentPanel.getByRole("heading", { name: "Текущее окружение" }),
    ).toBeVisible();
    await expect(
      environmentPanel.getByRole("heading", { name: "Каталог окружений" }),
    ).toBeVisible();
    const environmentPicker = environmentPanel.getByRole("button", {
      name: "Найти окружение по названию, назначению или ПО",
    });
    await environmentPicker.click();
    await expect(environmentPicker).toHaveAttribute("aria-expanded", "true");
    await environmentPanel
      .getByRole("heading", { name: "Текущее окружение" })
      .click();
    await expect(environmentPicker).toHaveAttribute("aria-expanded", "false");
    const boundEnvironmentRef = await page.evaluate(async (agentRef) => {
      const response = await fetch(
        `/api/v1/agents/${encodeURIComponent(agentRef)}/runtime-configuration`,
      );
      const body = (await response.json()) as { environment: { ref: string } };
      return body.environment.ref;
    }, coordinatorRef);
    if (boundEnvironmentRef !== runtimeEnvironmentRef) {
      await environmentPicker.click();
      const popover = page.getByRole("dialog", {
        name: "Найти окружение по названию, назначению или ПО",
      });
      await popover
        .getByRole("combobox", {
          name: "Найти окружение по названию, назначению или ПО",
        })
        .fill(runtimeEnvironmentName);
      await popover
        .getByRole("option", { name: new RegExp(runtimeEnvironmentName) })
        .click();
      const binding = page.waitForResponse(
        (response) =>
          response.request().method() === "PUT" &&
          new URL(response.url()).pathname ===
            `/api/v1/agents/${coordinatorRef}/runtime-environment-binding`,
      );
      await environmentPanel
        .getByRole("button", { name: "Назначить окружение" })
        .click();
      expect((await binding).status()).toBe(200);
    }

    const readback = await page.evaluate(async (agentRef) => {
      const response = await fetch(
        `/api/v1/agents/${encodeURIComponent(agentRef)}/runtime-configuration`,
      );
      if (!response.ok)
        throw new Error(`runtime readback failed: ${String(response.status)}`);
      return (await response.json()) as {
        configuration: {
          model: string;
          provider: string;
          providerPolicy: { mode: string };
          version: number;
        };
        environment: { ref: string; currentVersion: { revision: number } };
        publishedOverlay: { content: string; state: string };
        safeEffectiveConfig: string;
      };
    }, coordinatorRef);
    expect(readback.configuration.provider).toBe(providerName);
    expect(readback.configuration.model).toBe(modelName);
    expect(readback.configuration.version).toBeGreaterThan(0);
    expect(readback.configuration.providerPolicy.mode).toBe(policyAfter);
    expect(readback.environment.ref).toBe(runtimeEnvironmentRef);
    expect(readback.environment.currentVersion.revision).toBeGreaterThan(0);
    expect(readback.publishedOverlay.state).toBe("PUBLISHED");
    expect(readback.publishedOverlay.content).toMatch(
      /model_reasoning_effort\s*=\s*"high"/,
    );
    expect(readback.publishedOverlay.content).toMatch(
      /persistence\s*=\s*"none"/,
    );
    expect(readback.safeEffectiveConfig).toContain(
      'model_reasoning_effort = "high"',
    );

    await ensureAuthorizedProviderAffinity(page, coordinatorRef, 0);
    await ensureAuthorizedProviderAffinity(page, analystRef, 1);
  });

  test("файл загружается, просматривается, привязывается и скачивается", async ({
    page,
  }) => {
    requireRefs("projectRef", "coordinatorRef");
    await ensureAgentCapability(page, projectRef, coordinatorRef, /Файлы/);
    const content = [
      "Синтетический лид для локального E2E.",
      "Компания: Тестовое производство.",
      "Запрос: автоматизация квалификации обращений.",
    ].join("\n");
    const initialArtifactsResponse = page.waitForResponse(
      (response) =>
        response.request().method() === "GET" &&
        new URL(response.url()).pathname ===
          `/api/v1/projects/${projectRef}/artifacts`,
    );
    await gotoWithRetry(page, `/projects/${projectRef}/files`);
    expect((await initialArtifactsResponse).ok()).toBe(true);
    await expectPageHeading(page, "Файлы и знания");

    const existing = page.getByRole("listitem", {
      name: new RegExp(uploadedFileName),
    });
    if ((await existing.count()) === 0) {
      const uploadButton = page.getByRole("button", {
        name: "Загрузить",
        exact: true,
      });
      await expect(uploadButton).toBeVisible();
      const uploadResponse = page.waitForResponse(
        (response) =>
          response.request().method() === "POST" &&
          new URL(response.url()).pathname ===
            `/api/v1/projects/${projectRef}/artifacts`,
      );
      const fileChooser = page.waitForEvent("filechooser");
      await uploadButton.click();
      await (
        await fileChooser
      ).setFiles({
        name: uploadedFileName,
        mimeType: "text/plain",
        buffer: Buffer.from(content, "utf8"),
      });
      expect((await uploadResponse).status()).toBe(201);
    }
    const artifact = page
      .getByRole("button", { name: new RegExp(uploadedFileName) })
      .first();
    await expect(artifact).toBeVisible();
    await artifact.click();
    await expect(
      page.getByRole("heading", { name: uploadedFileName }),
    ).toBeVisible();
    const preview = page.getByRole("button", { name: "Открыть", exact: true });
    if ((await preview.count()) > 0) await preview.click();
    const previewDialog = page.getByRole("dialog", { name: uploadedFileName });
    await expect(previewDialog).toBeVisible();
    await expect(
      previewDialog.locator(".file-preview-dialog__content pre"),
    ).toContainText(content);

    const downloadPromise = page.waitForEvent("download");
    await previewDialog
      .getByRole("button", { name: "Скачать", exact: true })
      .click();
    const download = await downloadPromise;
    expect(download.suggestedFilename()).toBe(uploadedFileName);
    const path = await download.path();
    if (!path) throw new Error("download did not produce a local file");
    expect(await readFile(path, "utf8")).toBe(content);
    await previewDialog
      .locator("button.button")
      .filter({ hasText: "Закрыть" })
      .click();
    await expect(previewDialog).toHaveCount(0);

    const binding = page.getByRole("checkbox", {
      name: new RegExp(coordinatorName),
    });
    if (!(await binding.isChecked())) {
      const bindingResponse = page.waitForResponse((response) => {
        const path = new URL(response.url()).pathname;
        return (
          response.request().method() === "POST" &&
          path.startsWith("/api/v1/artifacts/") &&
          path.endsWith("/bindings")
        );
      });
      await binding.check();
      const response = await bindingResponse;
      expect(response.status(), await response.text()).toBe(200);
    }
    await expect(binding).toBeChecked();

    uploadedArtifactRef = await resolveArtifactRef(
      page,
      projectRef,
      uploadedFileName,
    );
    expect(uploadedArtifactRef).not.toBe("");
    persistRefs();
  });

  test("workboard сохраняет контекст Проекта, а запуск выбирает файлы и сессию", async ({
    page,
  }) => {
    requireRefs(
      "projectRef",
      "coordinatorRef",
      "firstRunRef",
      "continuationRunRef",
      "uploadedArtifactRef",
    );
    await gotoWithRetry(page, `/projects/${projectRef}`);
    await expectPageHeading(page, projectName);
    const workboard = page.locator(".project-workboard");
    await expect(workboard).toBeVisible();
    await expect(
      page.getByRole("heading", { name: "Требует внимания" }),
    ).toBeVisible();
    await expect(workboard).toContainText("Выполняется сейчас");
    await expect(workboard).toContainText("Недавние результаты");
    await expect(workboard).toContainText("Ресурсы Проекта");
    await expect(
      workboard.getByRole("link", { name: "Все запуски Проекта" }),
    ).toHaveAttribute("href", `/projects/${projectRef}/runs`);
    await expect(
      workboard.getByRole("link", { name: "Все файлы Проекта" }),
    ).toHaveAttribute("href", `/projects/${projectRef}/files`);

    await gotoWithRetry(
      page,
      `/projects/${projectRef}/runs/new?targetType=AGENT&targetRef=${encodeURIComponent(coordinatorRef)}`,
    );
    await expect(page.locator(".project-context")).toContainText(projectName);
    await expect(
      page.getByRole("button", { name: "Цель", exact: true }),
    ).toContainText(coordinatorName);

    const newRunAttachment = await exerciseAttachmentComposer(
      page,
      page.locator(".new-run-section .attachment-composer"),
      `/api/v1/projects/${projectRef}/artifacts`,
      "new-run",
    );

    const chooseFiles = page
      .getByRole("region", { name: /Входные файлы/ })
      .getByRole("button", { name: "Выбрать файлы", exact: true });
    await expect(chooseFiles).toBeEnabled();
    await chooseFiles.click();
    const filePicker = page.getByRole("dialog", {
      name: "Выберите входные файлы",
    });
    await expect(filePicker).toBeVisible();
    const viewToggle = filePicker.locator(".view-mode-toggle");
    const listView = viewToggle.getByRole("button").nth(0);
    const gridView = viewToggle.getByRole("button").nth(1);
    await expect(listView).toHaveAttribute("aria-pressed", "true");
    await gridView.click();
    await expect(
      filePicker.locator(".new-run-file-picker__picker"),
    ).toHaveClass(/new-run-file-picker__picker--grid/);
    await listView.click();
    await expect(
      filePicker.locator(".new-run-file-picker__picker"),
    ).toHaveClass(/new-run-file-picker__picker--list/);
    const filteredArtifacts = page.waitForResponse((response) => {
      const url = new URL(response.url());
      return (
        response.request().method() === "GET" &&
        url.pathname === `/api/v1/projects/${projectRef}/artifacts` &&
        url.searchParams.get("query") === uploadedFileName
      );
    });
    await filePicker.locator('input[type="search"]').fill(uploadedFileName);
    expect((await filteredArtifacts).status()).toBe(200);
    const artifactOption = filePicker
      .getByRole("option")
      .filter({ hasText: uploadedFileName });
    await expect(artifactOption).toHaveCount(1);
    await artifactOption.click();
    await expect(artifactOption).toHaveAttribute("aria-selected", "true");
    await filePicker.locator(".overlay-panel__footer .button--primary").click();
    await expect(filePicker).toHaveCount(0);
    await expect(
      page
        .getByRole("region", { name: /Входные файлы/ })
        .getByText(uploadedFileName, { exact: true }),
    ).toBeVisible();

    const sessionReadback = await page.evaluate(
      async ({ initialRef, continuedRef }) => {
        const [initialResponse, continuedResponse] = await Promise.all([
          fetch(`/api/v1/runs/${encodeURIComponent(initialRef)}`),
          fetch(`/api/v1/runs/${encodeURIComponent(continuedRef)}`),
        ]);
        if (!initialResponse.ok || !continuedResponse.ok)
          throw new Error("session lineage readback failed");
        const initial = (await initialResponse.json()) as {
          sessionRef: string;
          title: string;
        };
        const continued = (await continuedResponse.json()) as {
          sessionRef: string;
        };
        return { initial, continued };
      },
      { initialRef: firstRunRef, continuedRef: continuationRunRef },
    );
    expect(sessionReadback.continued.sessionRef).toBe(
      sessionReadback.initial.sessionRef,
    );
    await page.getByRole("radio", { name: /Продолжить существующую/ }).check();
    const sessionPicker = page.getByRole("dialog", {
      name: "Выберите предыдущую сессию",
    });
    await expect(sessionPicker).toBeVisible();
    await sessionPicker
      .locator('input[type="search"]')
      .fill(sessionReadback.initial.title);
    await sessionPicker
      .getByRole("option")
      .filter({ hasText: sessionReadback.initial.title })
      .first()
      .click();
    await expect(sessionPicker).toHaveCount(0);
    await expect(page.locator(".launch-summary")).toContainText(
      sessionReadback.initial.title,
    );
    await expect(
      page.getByRole("button", {
        name: /Архивировать сессию|Восстановить сессию/,
      }),
    ).toHaveCount(0);

    await page.getByRole("radio", { name: /Новая сессия/ }).check();
    await page
      .getByLabel("Название запуска")
      .fill(`${environment.resourcePrefix} — manifest нового запуска`);
    await page
      .getByLabel("Задание")
      .fill(
        [
          `Найди manifest.json текущего AttachmentSet и запись файла ${newRunAttachment.fileName}.`,
          "После materialization выполни в терминале `sleep 20`, затем прочитай этот файл и верни одной строкой фактический маркер, имя файла и слово new-run-manifest-ok.",
          "Не угадывай маркер и не используй внешние источники.",
        ].join(" "),
      );
    const launchResponse = page.waitForResponse(
      (response) =>
        response.request().method() === "POST" &&
        new URL(response.url()).pathname === "/api/v1/runs",
    );
    await page.getByRole("button", { name: "Запустить", exact: true }).click();
    const launched = await launchResponse;
    expect(
      launched.status(),
      await mutationFailureDiagnostic(launched, page),
    ).toBe(201);
    const launchAttachmentSet = await readRequestAttachmentSet(page, launched);
    expect(launchAttachmentSet.items.map((item) => item.artifactRef)).toEqual(
      expect.arrayContaining([uploadedArtifactRef, newRunAttachment.ref]),
    );
    await expectRunState(page, /В очереди|Выполняется/);
    const attachmentBeforeDelete = await readArtifact(
      page,
      newRunAttachment.ref,
    );
    const attachmentAfterDelete = await deleteArtifactAtVersion(
      page,
      newRunAttachment.ref,
      attachmentBeforeDelete.version,
    );
    expect(attachmentAfterDelete.lifecycleState).toBe("DELETED");
    await waitForTerminalSuccess(page);
    await expect(page.locator("#main-content")).toContainText(
      newRunAttachment.marker,
    );
    await expect(page.locator("#main-content")).toContainText(
      newRunAttachment.fileName,
    );
    await expect(page.locator("#main-content")).toContainText(
      "new-run-manifest-ok",
    );
  });

  test("привязанный knowledge-файл доступен ИИ-сотруднику", async ({
    page,
  }) => {
    requireRefs("projectRef", "coordinatorRef", "uploadedArtifactRef");
    await gotoWithRetry(
      page,
      `/projects/${projectRef}/agents/${coordinatorRef}`,
    );
    await launchAgent(
      page,
      "Прочитай привязанный текстовый файл знаний и назови только указанную в нём компанию. Не угадывай и не используй внешние источники.",
    );
    await waitForTerminalSuccess(page);
    await expect(page.locator("#main-content")).toContainText(
      "Тестовое производство",
    );
  });

  test("автоматизация создаётся, редактируется, запускается и архивируется", async ({
    page,
  }) => {
    requireRefs("projectRef", "analystRef");
    const schedulesResponse = page.waitForResponse(
      (response) =>
        response.request().method() === "GET" &&
        new URL(response.url()).pathname ===
          `/api/v1/projects/${projectRef}/schedules`,
    );
    await gotoWithRetry(page, `/projects/${projectRef}/automations`);
    expect((await schedulesResponse).status()).toBe(200);
    await expectPageHeading(page, "Автоматизации");

    let row = page.locator(".automation-row").filter({
      hasText: automationName,
    });
    if ((await row.count()) === 0) {
      await page
        .getByRole("button", { name: "Новая автоматизация" })
        .first()
        .click();
      const dialog = page.getByRole("dialog", { name: "Новая автоматизация" });
      await dialog.getByLabel("Название").fill(automationName);
      await dialog.getByLabel("Тип цели").selectOption("AGENT");
      await dialog.getByRole("button", { name: "Цель", exact: true }).click();
      const targetPicker = page.getByRole("dialog", {
        name: "Цель",
        exact: true,
      });
      await expect(targetPicker).toBeVisible();
      await page.keyboard.press("Escape");
      await expect(targetPicker).toHaveCount(0);
      await expect(dialog).toBeVisible();
      await dialog.getByRole("button", { name: "Цель", exact: true }).click();
      await targetPicker
        .getByRole("combobox", { name: "Выберите цель" })
        .fill(analystName);
      await targetPicker
        .getByRole("option", { name: new RegExp(analystName) })
        .click();
      const triggerAt = new Date(Date.now() + 75_000);
      const saratovHour = (triggerAt.getUTCHours() + 4) % 24;
      const timeOfDay = `${String(saratovHour).padStart(2, "0")}:${String(
        triggerAt.getUTCMinutes(),
      ).padStart(2, "0")}`;
      await dialog.getByLabel("Когда запускать").selectOption("DAILY");
      await dialog.getByLabel("Время запуска").fill(timeOfDay);
      await dialog.getByLabel("Часовой пояс").selectOption("Europe/Saratov");
      await dialog.getByLabel("Задача").fill(automationTask);
      const creation = page.waitForResponse((response) => {
        const url = new URL(response.url());
        return (
          response.request().method() === "POST" &&
          url.pathname === `/api/v1/projects/${projectRef}/schedules`
        );
      });
      await dialog
        .getByRole("button", { name: "Создать", exact: true })
        .click();
      const created = await creation;
      expect(
        created.status(),
        await mutationFailureDiagnostic(created, page),
      ).toBe(201);
      await expect(dialog).toHaveCount(0);
      row = page.locator(".automation-row").filter({
        hasText: automationName,
      });
    }
    await expect(row).toHaveCount(1);
    automationRef = await resolveScheduleRef(page, projectRef, automationName);
    expect(automationRef).not.toBe("");
    persistRefs();
    await row.click();
    const details = page.locator(".automation-details");
    await expect(details).toContainText(automationName);
    if ((await row.locator(".status-badge").textContent()) === "Архивирован") {
      await expect(details).toContainText(automationEditedTask);
      if (scheduledRunRef) {
        await gotoWithRetry(page, `/runs/${scheduledRunRef}`);
        await waitForTerminalSuccess(page);
      }
      return;
    }
    if (
      (await row.locator(".status-badge").textContent()) === "Приостановлено"
    ) {
      const enableResponse = scheduleCommandResponse(page);
      await details.getByRole("button", { name: "Включить" }).click();
      expect(
        (await enableResponse).status(),
        await mutationFailureDiagnostic(await enableResponse, page),
      ).toBe(200);
    }
    await expect(row.locator(".status-badge")).toHaveText("Активен");

    const pauseResponse = scheduleCommandResponse(page);
    await details.getByRole("button", { name: "Приостановить" }).click();
    const paused = await pauseResponse;
    expect(paused.status(), await mutationFailureDiagnostic(paused, page)).toBe(
      200,
    );
    await expect(row.locator(".status-badge")).toHaveText("Приостановлено");
    const resumeResponse = scheduleCommandResponse(page);
    await details.getByRole("button", { name: "Включить" }).click();
    const resumed = await resumeResponse;
    expect(
      resumed.status(),
      await mutationFailureDiagnostic(resumed, page),
    ).toBe(200);
    await expect(row.locator(".status-badge")).toHaveText("Активен");

    scheduledRunRef = await expect
      .poll(
        async () =>
          page.evaluate(
            async ({ expectedTitle, expectedProjectRef }) => {
              const response = await fetch(
                `/api/v1/runs?projectRef=${encodeURIComponent(expectedProjectRef)}&pageSize=100`,
              );
              if (!response.ok) return "";
              const body = (await response.json()) as {
                items?: Array<{
                  ref: string;
                  source: string;
                  title: string;
                }>;
              };
              return (
                body.items?.find(
                  (run) =>
                    run.source === "SCHEDULE" && run.title === expectedTitle,
                )?.ref ?? ""
              );
            },
            { expectedTitle: automationName, expectedProjectRef: projectRef },
          ),
        {
          message: "расписание должно создать run",
          timeout: 180_000,
          intervals: [1_000, 2_000, 5_000],
        },
      )
      .not.toBe("")
      .then(() =>
        page.evaluate(
          async ({ expectedTitle, expectedProjectRef }) => {
            const response = await fetch(
              `/api/v1/runs?projectRef=${encodeURIComponent(expectedProjectRef)}&pageSize=100`,
            );
            const body = (await response.json()) as {
              items: Array<{ ref: string; source: string; title: string }>;
            };
            return (
              body.items.find(
                (run) =>
                  run.source === "SCHEDULE" && run.title === expectedTitle,
              )?.ref ?? ""
            );
          },
          { expectedTitle: automationName, expectedProjectRef: projectRef },
        ),
      );
    expect(scheduledRunRef).not.toBe("");
    persistRefs();
    await gotoWithRetry(page, `/runs/${scheduledRunRef}`);
    await waitForTerminalSuccess(page);
    const scheduledRunReadback = await page.evaluate(
      async ({ runRef, expectedAgentRef }) => {
        const [graphResponse, eventsResponse] = await Promise.all([
          fetch(`/api/v1/runs/${encodeURIComponent(runRef)}/graph`),
          fetch(
            `/api/v1/runs/${encodeURIComponent(runRef)}/events?afterSequence=0&limit=500`,
          ),
        ]);
        if (!graphResponse.ok || !eventsResponse.ok) {
          throw new Error(
            `Scheduled run readback failed: graph=${String(graphResponse.status)}, events=${String(eventsResponse.status)}`,
          );
        }
        const workspace = (await graphResponse.json()) as {
          run: {
            ref: string;
            state: string;
            source: string;
            resultSummary?: string;
            target: { type: string; ref: string };
          };
          graph: {
            nodes: Array<{
              ref: string;
              type: string;
              state: string;
              agentRef?: string;
            }>;
          };
        };
        const events = (await eventsResponse.json()) as {
          complete: boolean;
          currentSequence: number;
          items: Array<{
            type: string;
            nodeRef?: string;
            messageKind?: string;
            summary: string;
            actor?: { ref?: string };
          }>;
        };
        const agentNode = workspace.graph.nodes.find(
          (node) =>
            node.type === "AGENT_EXECUTION" &&
            node.agentRef === expectedAgentRef,
        );
        return {
          run: workspace.run,
          agentNode,
          complete: events.complete,
          currentSequence: events.currentSequence,
          finalMessages: events.items
            .filter(
              (event) =>
                event.type === "TURN_COMPLETED" &&
                event.nodeRef === agentNode?.ref &&
                event.messageKind === "FINAL_MESSAGE" &&
                event.actor?.ref === expectedAgentRef,
            )
            .map((event) => event.summary.trim()),
        };
      },
      { runRef: scheduledRunRef, expectedAgentRef: analystRef },
    );
    expect(scheduledRunReadback.run).toMatchObject({
      ref: scheduledRunRef,
      state: "SUCCEEDED",
      source: "SCHEDULE",
      target: { type: "AGENT", ref: analystRef },
      resultSummary: "KODEX_AUTOMATION_E2E_OK",
    });
    expect(scheduledRunReadback.agentNode?.state).toBe("SUCCEEDED");
    expect(scheduledRunReadback.complete).toBe(true);
    expect(scheduledRunReadback.currentSequence).toBeGreaterThan(0);
    expect(scheduledRunReadback.finalMessages).toEqual([
      "KODEX_AUTOMATION_E2E_OK",
    ]);

    await gotoWithRetry(page, `/projects/${projectRef}/automations`);
    row = page.locator(".automation-row").filter({ hasText: automationName });
    await row.click();
    if ((await readScheduleRevisionState(page, automationRef)).revision === 1) {
      const edit = page.waitForResponse(
        (response) =>
          response.request().method() === "PATCH" &&
          /^\/api\/v1\/schedules\/[^/]+$/.test(
            new URL(response.url()).pathname,
          ),
      );
      await page
        .locator(".automation-details")
        .getByRole("button", { name: "Изменить автоматизацию" })
        .click();
      const editDialog = page.getByRole("dialog", {
        name: "Изменить автоматизацию",
      });
      await editDialog.getByLabel("Задача").fill(automationEditedTask);
      await editDialog.getByRole("button", { name: "Сохранить" }).click();
      const edited = await edit;
      expect(edited.status()).toBe(200);
      expect(
        (
          (await edited.json()) as {
            currentRevision?: { revision?: number };
          }
        ).currentRevision?.revision,
      ).toBe(2);
      await expect(editDialog).toHaveCount(0);
    }
    expect(await readScheduleRevisionState(page, automationRef)).toEqual({
      revision: 2,
      task: automationEditedTask,
    });
    await expect(page.locator(".automation-details")).toContainText(
      automationEditedTask,
    );

    await page
      .locator(".automation-details")
      .getByRole("button", { name: "Архивировать" })
      .click();
    const archiveDialog = page.getByRole("dialog", {
      name: "Архивировать автоматизацию?",
    });
    await expect(archiveDialog).toContainText(
      "Будущие запуски будут отменены. Неизменяемые ревизии и история запусков останутся доступны только для чтения.",
    );
    const [archived] = await Promise.all([
      scheduleCommandResponse(page),
      archiveDialog.getByRole("button", { name: "Архивировать" }).click(),
    ]);
    expect(archived.request().postDataJSON()).toEqual({ action: "ARCHIVE" });
    expect(archived.status()).toBe(200);
    const archivedSchedule = (await archived.json()) as { state?: string };
    expect(archivedSchedule.state).toBe("ARCHIVED");
    expect(await readScheduleRevisionState(page, automationRef)).toEqual({
      revision: 2,
      task: automationEditedTask,
    });
    await expect(row).toHaveCount(0);
    await page
      .getByRole("combobox", { name: "Состояние" })
      .selectOption("ARCHIVED");
    row = page.locator(".automation-row").filter({ hasText: automationName });
    await expect(row).toBeVisible();
    await row.click();
    await expect(
      page.locator(".automation-details__status .status-badge"),
    ).toHaveText("Архивирован");
    await expect(page.locator(".automation-details")).toContainText(
      automationEditedTask,
    );
  });

  test("сотрудник без capability Файлы не получает файл", async ({ page }) => {
    requireRefs("projectRef", "analystRef", "uploadedArtifactRef");
    await gotoWithRetry(page, `/projects/${projectRef}/agents/${analystRef}`);
    await launchAgent(
      page,
      "Ответь одним коротким предложением: текстовая задача выполнена. Файлы не используй.",
    );
    await waitForTerminalSuccess(page);
    await expect(page.locator("#main-content")).not.toContainText(
      "Не удалось прочитать файл из-за ограничения среды",
    );

    await gotoWithRetry(page, `/projects/${projectRef}/files`);
    await expectPageHeading(page, "Файлы и знания");
    const searchResponse = page.waitForResponse((response) => {
      const url = new URL(response.url());
      return (
        response.request().method() === "GET" &&
        url.pathname === `/api/v1/projects/${projectRef}/artifacts` &&
        url.searchParams.get("sourceKind") === "CONTROL_CENTER" &&
        url.searchParams.get("query") === uploadedFileName
      );
    });
    await page
      .getByRole("searchbox", { name: "Найти файл" })
      .fill(uploadedFileName);
    const searched = await searchResponse;
    expect(
      searched.status(),
      `Exact artifact search failed with HTTP ${String(searched.status())}`,
    ).toBe(200);
    const searchReadback = (await searched.json()) as {
      items?: Array<{ fileName: string; ref: string }>;
    };
    expect(searchReadback.items).toEqual(
      expect.arrayContaining([
        expect.objectContaining({
          fileName: uploadedFileName,
          ref: uploadedArtifactRef,
        }),
      ]),
    );
    const knowledgeArtifact = page.locator(
      `[data-artifact-ref="${uploadedArtifactRef}"]`,
    );
    await expect(knowledgeArtifact).toBeVisible();
    await knowledgeArtifact.locator("button.file-list-row").click();
    const binding = page.getByRole("checkbox", {
      name: new RegExp(analystName),
    });
    await expect(binding).toBeDisabled();

    await gotoWithRetry(
      page,
      `/projects/${projectRef}/runs/new?targetType=AGENT&targetRef=${encodeURIComponent(analystRef)}`,
    );
    await expect(page.getByLabel("Задание")).toBeVisible();
    const filesSection = page.getByRole("region", {
      name: /Входные файлы/,
    });
    await expect(
      filesSection.getByRole("button", {
        name: "Выбрать файлы",
        exact: true,
      }),
    ).toBeDisabled();
    await expect(page.locator("#main-content")).toContainText(
      "Сначала выдайте всем выбранным ИИ-сотрудникам возможность «Файлы»",
    );

    const forgedResult = await page.evaluate(
      async ({ expectedFileName, expectedProjectRef, targetRef, title }) => {
        const artifactResponse = await fetch(
          `/api/v1/projects/${encodeURIComponent(expectedProjectRef)}/artifacts?pageSize=100`,
        );
        const artifactPage = (await artifactResponse.json()) as {
          items: Array<{ fileName: string; ref: string }>;
        };
        const artifactRef = artifactPage.items.find(
          (item) => item.fileName === expectedFileName,
        )?.ref;
        if (!artifactRef) throw new Error("E2E artifact is absent");
        const csrfPrefix = "__Host-kodex-csrf=";
        const csrf = document.cookie
          .split(";")
          .map((part) => part.trim())
          .find((part) => part.startsWith(csrfPrefix))
          ?.slice(csrfPrefix.length);
        if (!csrf) throw new Error("E2E CSRF cookie is absent");
        const mutationHeaders = (): Record<string, string> => ({
          "Content-Type": "application/json",
          "Idempotency-Key": crypto.randomUUID(),
          "X-CSRF-Token": decodeURIComponent(csrf),
        });
        const draftResponse = await fetch(
          `/api/v1/projects/${encodeURIComponent(expectedProjectRef)}/attachment-sets`,
          {
            method: "POST",
            headers: mutationHeaders(),
            body: JSON.stringify({
              purpose: "RUN_INPUT",
              artifactRefs: [artifactRef],
            }),
          },
        );
        if (!draftResponse.ok)
          throw new Error(
            `E2E AttachmentSet draft failed: ${String(draftResponse.status)}`,
          );
        const draft = (await draftResponse.json()) as {
          ref: string;
          version: number;
        };
        const finalizeResponse = await fetch(
          `/api/v1/attachment-sets/${encodeURIComponent(draft.ref)}/finalization`,
          {
            method: "POST",
            headers: {
              ...mutationHeaders(),
              "If-Match": `"${String(draft.version)}"`,
            },
          },
        );
        if (!finalizeResponse.ok)
          throw new Error(
            `E2E AttachmentSet finalization failed: ${String(finalizeResponse.status)}`,
          );
        const attachmentSet = (await finalizeResponse.json()) as {
          ref: string;
        };
        const beforeResponse = await fetch(
          `/api/v1/runs?projectRef=${encodeURIComponent(expectedProjectRef)}&pageSize=100`,
        );
        const before = (await beforeResponse.json()) as {
          items: Array<{ title: string }>;
        };
        const response = await fetch("/api/v1/runs", {
          method: "POST",
          headers: {
            "Content-Type": "application/json",
            "Idempotency-Key": crypto.randomUUID(),
            "X-CSRF-Token": decodeURIComponent(csrf),
          },
          body: JSON.stringify({
            projectRef: expectedProjectRef,
            targetRef,
            targetType: "AGENT",
            title,
            task: "Эта подделанная команда не должна создать Run.",
            attachmentSetRef: attachmentSet.ref,
          }),
        });
        const problem = (await response.json()) as { code?: string };
        const afterResponse = await fetch(
          `/api/v1/runs?projectRef=${encodeURIComponent(expectedProjectRef)}&pageSize=100`,
        );
        const after = (await afterResponse.json()) as {
          items: Array<{ title: string }>;
        };
        return {
          afterCount: after.items.length,
          beforeCount: before.items.length,
          code: problem.code ?? "",
          created: after.items.some((run) => run.title === title),
          status: response.status,
        };
      },
      {
        expectedFileName: uploadedFileName,
        expectedProjectRef: projectRef,
        targetRef: analystRef,
        title: `${environment.resourcePrefix} — запрещённый запуск с файлом`,
      },
    );
    expect(forgedResult.status).toBe(409);
    expect(forgedResult.code).not.toBe("");
    expect(forgedResult.created).toBe(false);
    expect(forgedResult.afterCount).toBe(forgedResult.beforeCount);
  });

  test("вложенный Процесс показывает live graph и Human Gate переживает reconnect", async ({
    page,
    browserDiagnostics,
  }) => {
    requireRefs("projectRef", "coordinatorRef", "analystRef");
    if (!discoveryMode || !writerRef) {
      writerRef = await createAgent(page, projectRef, {
        name: writerName,
        purpose: "Готовить персонализированные коммерческие предложения.",
        role: "Автор B2B-предложений.",
        instructions:
          "Используй вывод аналитика, не выдумывай факты и отвечай по-русски.",
      });
      persistRefs();
      await publishAgent(page);
    }

    await ensureAgentCapability(
      page,
      projectRef,
      coordinatorRef,
      /Делегирование/,
    );

    if (discoveryMode && workflowRef) {
      await gotoWithRetry(
        page,
        `/projects/${projectRef}/workflows/${workflowRef}`,
      );
    } else {
      await gotoWithRetry(page, `/projects/${projectRef}`);
      await openKodex(page, true);
      const prompt = [
        `В текущем Проекте создай ровно один Процесс с точным названием «${workflowName}».`,
        `Назначение: параллельно оценить лид и подготовить предложение с решением владельца. Координатор — существующий сотрудник «${coordinatorName}».`,
        `Добавь ровно два независимых параллельных этапа в одной группе: «Оценка лида» выполняет существующий сотрудник «${analystName}» с заданием оценить исходные данные лида и вернуть структурированный вывод; «Коммерческое предложение» выполняет существующий сотрудник «${writerName}» с заданием подготовить черновик предложения только по исходным данным лида. Ни один параллельный этап не должен ожидать результат другого.`,
        "Добавь обязательное LONG_TEXT поле входных данных с точным названием «Исходные данные лида».",
        "Для второго этапа требуется решение человека с вариантами APPROVE, REJECT и REQUEST_CHANGES. Максимальная параллельность — 2, timeout Процесса — 7200 секунд.",
        "Не создавай и не меняй сотрудников, не запускай Процесс и не создавай другие объекты.",
      ].join(" ");
      await applyLatestKodexPlan(page, prompt, workflowName);
      await closeKodex(page);

      await gotoWithRetry(page, `/projects/${projectRef}/workflows`);
      const workflowLink = page.getByRole("link", {
        name: new RegExp(workflowName),
      });
      await expect(workflowLink).toBeVisible();
      await workflowLink.click();
      await expect(page).toHaveURL(/\/workflows\/[^/]+$/);
      workflowRef = routeRef(page, "workflows");
      persistRefs();
    }
    await expect(page.locator(".workflow-step")).toHaveCount(2);
    const validateWorkflow = page.getByRole("button", {
      name: "Проверить Процесс",
    });
    if ((await validateWorkflow.count()) > 0) {
      await validateWorkflow.click();
      await expect(validateWorkflow).toHaveCount(0);
    }
    const publishWorkflow = page.getByRole("button", {
      name: "Опубликовать Процесс",
    });
    if ((await publishWorkflow.count()) > 0) {
      await publishWorkflow.click();
    }
    await expect(publishWorkflow).toHaveCount(0);

    let resumeExistingRun = false;
    if (discoveryMode && workflowRunRef && workflowRunRef !== "new") {
      const state = await page.evaluate(async (runRef) => {
        const response = await fetch(
          `/api/v1/runs/${encodeURIComponent(runRef)}`,
        );
        if (!response.ok) return "MISSING";
        return (
          ((await response.json()) as { state?: string }).state ?? "MISSING"
        );
      }, workflowRunRef);
      resumeExistingRun = ![
        "MISSING",
        "SUCCEEDED",
        "FAILED",
        "CANCELLED",
      ].includes(state);
      if (!resumeExistingRun) {
        workflowRunRef = "";
        persistRefs();
      }
    }
    if (resumeExistingRun) {
      await gotoWithRetry(page, `/runs/${workflowRunRef}`);
    } else {
      await page.getByRole("link", { name: "Запустить", exact: true }).click();
      await expect(page).toHaveURL(/\/runs\/new\?/);
      await page
        .getByLabel("Название запуска")
        .fill(`${environment.resourcePrefix} — запуск квалификации лида`);
      await page
        .getByLabel("Задание")
        .fill(
          "Квалифицируй лид производственной компании и подготовь предложение. После завершения этапов и предусмотренного Процессом решения владельца собери итоговый ответ.",
        );
      await page
        .getByLabel("Исходные данные лида")
        .fill(
          "Компания: производственное предприятие; потребность: автоматизация продаж.",
        );
      const launchResponse = page.waitForResponse(
        (response) =>
          response.request().method() === "POST" &&
          new URL(response.url()).pathname === "/api/v1/runs",
      );
      await page
        .getByRole("button", { name: "Запустить", exact: true })
        .click();
      const launched = await launchResponse;
      expect(
        launched.status(),
        await mutationFailureDiagnostic(launched, page),
      ).toBe(201);
      await expect
        .poll(() => routeRef(page, "runs"), { timeout: 30_000 })
        .not.toBe("new");
      workflowRunRef = routeRef(page, "runs");
      persistRefs();
      await expectRunState(page, /В очереди|Выполняется/);
    }
    await expectRunState(page, "Ждёт решения");
    const decisionAttention = page
      .getByRole("toolbar", { name: "Инструменты запуска" })
      .getByRole("button", { name: "Решения" });
    await expect(decisionAttention).toBeVisible();
    await page.emulateMedia({ reducedMotion: "no-preference" });
    await expect(decisionAttention).toHaveCSS(
      "animation-name",
      /^attention-outline(?:-[a-z0-9]+)?$/,
    );
    await page.emulateMedia({ reducedMotion: "reduce" });
    await expect(decisionAttention).toHaveCSS("animation-name", "none");
    await expect(decisionAttention).not.toHaveCSS("box-shadow", "none");
    await page.emulateMedia({ reducedMotion: "no-preference" });
    const graphNodes = page
      .getByRole("region", { name: "Граф выполнения" })
      .locator('[role="button"][data-node-ref]');
    await expect(graphNodes).toHaveCount(5, {
      timeout: 300_000,
    });
    await expect
      .poll(() =>
        graphNodes.evaluateAll((nodes) =>
          nodes.map((node) => node.getAttribute("aria-label") ?? ""),
        ),
      )
      .toEqual(
        expect.arrayContaining([
          expect.stringContaining(analystName),
          expect.stringContaining(writerName),
        ]),
      );
    const authoritativeGraph = await page.evaluate(async (runRef) => {
      const response = await fetch(
        `/api/v1/runs/${encodeURIComponent(runRef)}/graph`,
      );
      if (!response.ok)
        throw new Error(`graph read failed: ${String(response.status)}`);
      const body = (await response.json()) as {
        graph: {
          edges: Array<{
            sourceNodeRef: string;
            targetNodeRef: string;
            type: string;
          }>;
          nodes: Array<{ ref: string; type: string }>;
        };
      };
      return body.graph;
    }, workflowRunRef);
    const authoritativeNodeRefs = new Set(
      authoritativeGraph.nodes.map((node) => node.ref),
    );
    const missingEdgeEndpoints = authoritativeGraph.edges.flatMap((edge) => [
      ...(authoritativeNodeRefs.has(edge.sourceNodeRef)
        ? []
        : [`${edge.type}:source:${edge.sourceNodeRef}`]),
      ...(authoritativeNodeRefs.has(edge.targetNodeRef)
        ? []
        : [`${edge.type}:target:${edge.targetNodeRef}`]),
    ]);
    expect(
      missingEdgeEndpoints,
      `authoritative nodes: ${[...authoritativeNodeRefs].sort().join(",")}`,
    ).toEqual([]);
    expect(authoritativeGraph.nodes).toHaveLength(6);
    expect(
      authoritativeGraph.nodes.filter((node) => node.type === "HUMAN_GATE"),
    ).toHaveLength(1);
    const authoritativeEdgeTypes = authoritativeGraph.edges
      .map((edge) => edge.type)
      .sort();
    expect(authoritativeEdgeTypes).toEqual([
      "CALLBACK_TO",
      "CALLBACK_TO",
      "CONTINUES",
      "DELEGATED_TO",
      "DELEGATED_TO",
      "DELEGATED_TO",
      "WAITING_FOR",
    ]);
    const visibleSessionEdgeTypes = authoritativeEdgeTypes.filter(
      (type) => type !== "WAITING_FOR",
    );
    await expect
      .poll(
        () =>
          page
            .locator("[data-edge-type]")
            .evaluateAll((edges) =>
              edges
                .map((edge) => edge.getAttribute("data-edge-type") ?? "")
                .sort(),
            ),
        {
          message: `visible session edges: ${visibleSessionEdgeTypes.join(",")}`,
        },
      )
      .toEqual(visibleSessionEdgeTypes);
    await page
      .getByRole("toolbar", { name: "Инструменты запуска" })
      .getByRole("button", { name: "Ход работы" })
      .click();
    const activityDrawer = page.getByRole("dialog", { name: "Ход работы" });
    await expect(activityDrawer).toBeVisible();
    await activityDrawer
      .getByRole("combobox", { name: "Контекст узла" })
      .selectOption("");
    await expect(
      activityDrawer.getByText("Результат дочернего ИИ-сотрудника доставлен", {
        exact: true,
      }),
    ).toHaveCount(2);

    await browserDiagnostics.withExpectedNetworkInterruption(page, async () => {
      await page.context().setOffline(true);
      await expect(
        page.getByText(
          "Нет сети. Показываем последнее полученное состояние; действия временно недоступны.",
          { exact: true },
        ),
      ).toBeVisible();
      await page.context().setOffline(false);
      await expect(
        page.getByText(
          "Нет сети. Показываем последнее полученное состояние; действия временно недоступны.",
          { exact: true },
        ),
      ).toHaveCount(0);
    });

    const gate = await openGateForRun(page, workflowRunRef);
    const gateReadback = await readOwnerGate(page, gate.ref);
    await gotoWithRetry(page, "/decisions");
    await expectPageHeading(page, "Решения");
    const decisionRow = page
      .locator(".decision-row")
      .filter({ hasText: gateReadback.title })
      .first();
    await expect(decisionRow).toBeVisible();
    await decisionRow.click();
    const decisionDetail = page.locator(".decision-detail");
    await expect(decisionDetail).toContainText(gateReadback.contextSummary);
    await expect(decisionDetail).toContainText(
      gateReadback.consequencesSummary,
    );
    await expect(decisionDetail).toContainText(
      gateReadback.requestedBy.displayName,
    );
    const evidence = await exerciseAttachmentComposer(
      page,
      decisionDetail.locator(".attachment-composer"),
      `/api/v1/projects/${projectRef}/artifacts`,
      "human-gate",
    );
    await decisionDetail
      .getByRole("textbox", { name: "Комментарий к одобрению" })
      .fill(
        "Evidence приложен; локальный E2E подтверждает решение через inbox.",
      );
    const resolutionResponse = page.waitForResponse(
      (response) =>
        response.request().method() === "POST" &&
        new URL(response.url()).pathname ===
          `/api/v1/owner-gates/${gate.ref}/resolution`,
    );
    await decisionDetail
      .getByRole("button", { name: "Одобрить", exact: true })
      .click();
    const resolved = await resolutionResponse;
    expect(resolved.status(), await resolved.text()).toBe(200);
    const resolutionAttachmentSet = await readRequestAttachmentSet(
      page,
      resolved,
    );
    expect(
      resolutionAttachmentSet.items.map((item) => item.artifactRef),
    ).toEqual([evidence.ref]);
    const resolvedGate = (await resolved.json()) as {
      gate?: { resolutionAttachmentSetRef?: string; state?: string };
    };
    expect(resolvedGate.gate?.state).toBe("APPROVED");
    expect(resolvedGate.gate?.resolutionAttachmentSetRef).toBe(
      resolutionAttachmentSet.ref,
    );

    const staleResolution = await resolveGateAtVersion(
      page,
      gate.ref,
      gate.version,
    );
    expect(staleResolution.status).toBe(409);
    expect(staleResolution.code).not.toBe("");
    expect(
      (await readOwnerGate(page, gate.ref)).resolutionAttachmentSetRef,
    ).toBe(resolutionAttachmentSet.ref);

    await gotoWithRetry(page, `/runs/${workflowRunRef}`);
    await expectRunState(page, /Выполняется|Завершён/);
    await waitForTerminalSuccess(page);
    await assertNoDuplicateGraphNodes(page);
    await page
      .getByRole("toolbar", { name: "Инструменты запуска" })
      .getByRole("button", { name: "Ход работы" })
      .click();
    await expect(
      page.getByRole("dialog", { name: "Ход работы" }),
    ).toContainText(/решение/i);
  });

  test("отмена закрывает граф, а retry создаёт новую попытку с lineage", async ({
    page,
  }) => {
    requireRefs("projectRef", "coordinatorRef");
    await gotoWithRetry(
      page,
      `/projects/${projectRef}/agents/${coordinatorRef}`,
    );
    const cancelledRef = await launchAgent(
      page,
      "Это проверка отмены. Сначала выполни в терминале команду `sleep 120`, дождись её завершения и только затем сообщи результат.",
    );
    await expectRunState(page, /В очереди|Выполняется/);
    await page.getByRole("button", { name: "Отменить запуск" }).click();
    await expectRunState(page, "Отменён");
    await expect(
      page
        .getByRole("region", { name: "Граф выполнения" })
        .getByRole("button", { name: /Сессия.*Отменён/ })
        .first(),
    ).toBeVisible();

    await page.getByRole("button", { name: "Повторить попытку" }).click();
    await expect(page).toHaveURL(
      (url) =>
        /\/runs\/[^/]+$/.test(url.pathname) &&
        !url.pathname.endsWith(`/${cancelledRef}`),
    );
    const retriedRef = routeRef(page, "runs");
    expect(retriedRef).not.toBe(cancelledRef);
    await expect(page.getByText("Попытка 2", { exact: true })).toBeVisible();
    await expect(
      page.getByRole("link", { name: "Открыть предыдущую попытку" }),
    ).toHaveAttribute("href", `/projects/${projectRef}/runs/${cancelledRef}`);
    await expect
      .poll(
        () =>
          page.evaluate(async (runRef) => {
            const response = await fetch(
              `/api/v1/runs/${encodeURIComponent(runRef)}/graph`,
            );
            if (!response.ok) return -1;
            const body = (await response.json()) as {
              graph: { edges: Array<{ type: string }> };
            };
            return body.graph.edges.filter((edge) => edge.type === "RETRY_OF")
              .length;
          }, retriedRef),
        { timeout: 30_000 },
      )
      .toBe(1);
    await expect(page.locator('[data-edge-type="RETRY_OF"]')).toHaveCount(1);
    const cancelRetry = page.getByRole("button", { name: "Отменить запуск" });
    if (await cancelRetry.isVisible()) {
      await cancelRetry.click();
      await expectRunState(page, "Отменён");
    }
  });

  test("enterprise RBAC объясняет точечный allow и отказывает другому сотруднику", async ({
    page,
  }) => {
    requireRefs("projectRef", "coordinatorRef", "analystRef");
    await gotoWithRetry(page, "/administration/access/roles");
    await expectPageHeading(page, "Участники и доступ");
    let roleCard = page
      .locator(".role-card")
      .filter({ hasText: accessRoleName })
      .first();
    if ((await roleCard.count()) === 0) {
      await page.getByRole("button", { name: "Создать роль" }).click();
      const dialog = page.getByRole("dialog", {
        name: "Новая пользовательская роль",
      });
      await dialog.getByLabel("Название").fill(accessRoleName);
      await dialog
        .getByLabel("Понятное назначение")
        .fill("Запуск и просмотр одного явно выбранного ИИ-сотрудника.");
      const permissions = dialog.getByRole("group", { name: "Полномочия" });
      await permissions
        .getByRole("checkbox", {
          name: /Просматривать ИИ-сотрудников/,
        })
        .check();
      await permissions
        .getByRole("checkbox", {
          name: /Запускать ИИ-сотрудников/,
        })
        .check();
      await dialog
        .getByRole("group", { name: "Допустимые области" })
        .getByRole("checkbox", { name: /Конкретный ресурс/ })
        .check();
      await dialog
        .getByLabel("Причина изменения")
        .fill("Проверка enterprise RBAC в локальном E2E.");
      const creation = page.waitForResponse(
        (response) =>
          response.request().method() === "POST" &&
          new URL(response.url()).pathname ===
            "/api/v1/administration/access/roles",
      );
      await dialog.getByRole("button", { name: "Создать роль v1" }).click();
      expect((await creation).status()).toBe(201);
      roleCard = page
        .locator(".role-card")
        .filter({ hasText: accessRoleName })
        .first();
    }
    await expect(roleCard).toContainText("Конкретный ресурс");
    await expect(roleCard).toContainText("Полномочий: 2");
    const roleTagHeights = await roleCard
      .locator(".scope-tag")
      .evaluateAll((tags) =>
        tags.map((tag) => Math.round(tag.getBoundingClientRect().height)),
      );
    expect(roleTagHeights.length).toBeGreaterThanOrEqual(2);
    expect(
      roleTagHeights.every((height) => height > 0 && height <= 32),
      `Role badges must remain compact, received heights: ${roleTagHeights.join(", ")}`,
    ).toBe(true);

    const setup = await page.evaluate(
      async ({
        exactAgentRef,
        expectedGroupName,
        expectedRoleName,
        otherAgentRef,
        projectRef,
      }) => {
        const [rolesResponse, groupsResponse] = await Promise.all([
          fetch(
            "/api/v1/administration/access/roles?pageSize=100&includeArchived=false",
          ),
          fetch("/api/v1/administration/access/oidc-groups?pageSize=100"),
        ]);
        if (!rolesResponse.ok || !groupsResponse.ok)
          throw new Error("RBAC catalog readback failed");
        const roles = (await rolesResponse.json()) as {
          items: Array<{
            ref: string;
            currentVersion: { ref: string; name: string };
          }>;
        };
        const role = roles.items.find(
          (item) => item.currentVersion.name === expectedRoleName,
        );
        if (!role) throw new Error("E2E access role is absent");
        const groups = (await groupsResponse.json()) as {
          items: Array<{
            ref: string;
            displayName: string;
            memberCount: number;
            state: string;
          }>;
        };
        const matchingGroups = groups.items.filter(
          (group) => group.displayName === expectedGroupName,
        );
        if (
          matchingGroups.length !== 1 ||
          matchingGroups[0]?.state !== "ACTIVE" ||
          matchingGroups[0].memberCount < 1
        ) {
          throw new Error("Ожидаемая активная OIDC-группа не синхронизирована");
        }
        const candidate = {
          ...matchingGroups[0],
          kind: "OIDC_GROUP" as const,
        };
        const bindingsResponse = await fetch(
          `/api/v1/administration/access/bindings?pageSize=100&projectRef=${encodeURIComponent(projectRef)}&roleRef=${encodeURIComponent(role.ref)}&includeRevoked=false`,
        );
        if (!bindingsResponse.ok)
          throw new Error("RBAC binding catalog readback failed");
        const bindings = (await bindingsResponse.json()) as {
          items: Array<{
            subject: { ref: string };
            scope: { resourceKind?: string; resourceRef?: string };
          }>;
        };
        const existing = bindings.items.find(
          (binding) =>
            binding.scope.resourceKind === "AGENT" &&
            binding.scope.resourceRef === exactAgentRef,
        );
        if (existing?.subject.ref === candidate.ref) {
          return {
            candidate,
            exactAgentRef,
            otherAgentRef,
            projectRef,
            roleRef: role.ref,
            roleVersionRef: role.currentVersion.ref,
          };
        }
        const csrfPrefix = `${encodeURIComponent("__Host-kodex-csrf")}=`;
        const csrf = document.cookie
          .split("; ")
          .find((part) => part.startsWith(csrfPrefix))
          ?.slice(csrfPrefix.length);
        if (!csrf) throw new Error("E2E CSRF cookie is absent");
        const response = await fetch(
          "/api/v1/administration/access/effective-access/query",
          {
            method: "POST",
            headers: {
              "Content-Type": "application/json",
              "X-CSRF-Token": decodeURIComponent(csrf),
            },
            body: JSON.stringify({
              subjectRef: candidate.ref,
              permissionKeys: ["agent.launch"],
              target: {
                kind: "RESOURCE_INSTANCE",
                projectRef,
                resourceKind: "AGENT",
                resourceRef: exactAgentRef,
              },
            }),
          },
        );
        if (!response.ok) {
          throw new Error(
            `Исходное решение OIDC-group RBAC недоступно: ${String(response.status)} ${await response.text()}`,
          );
        }
        const body = (await response.json()) as {
          items: Array<{ decision: string }>;
        };
        if (body.items[0]?.decision !== "DENIED") {
          throw new Error("OIDC-группа имеет неожиданный исходный доступ");
        }
        return {
          candidate,
          exactAgentRef,
          otherAgentRef,
          projectRef,
          roleRef: role.ref,
          roleVersionRef: role.currentVersion.ref,
        };
      },
      {
        exactAgentRef: coordinatorRef,
        expectedGroupName: environment.rbacGroup,
        expectedRoleName: accessRoleName,
        otherAgentRef: analystRef,
        projectRef,
      },
    );

    const existingBinding = await page.evaluate(
      async ({ exactAgentRef, projectRef, roleRef, subjectRef }) => {
        const response = await fetch(
          `/api/v1/administration/access/bindings?pageSize=100&projectRef=${encodeURIComponent(projectRef)}&roleRef=${encodeURIComponent(roleRef)}&subjectRef=${encodeURIComponent(subjectRef)}&includeRevoked=false`,
        );
        if (!response.ok) throw new Error("RBAC binding readback failed");
        const body = (await response.json()) as {
          items: Array<{ scope: { resourceRef?: string } }>;
        };
        return body.items.some(
          (item) => item.scope.resourceRef === exactAgentRef,
        );
      },
      {
        exactAgentRef: coordinatorRef,
        projectRef,
        roleRef: setup.roleRef,
        subjectRef: setup.candidate.ref,
      },
    );
    if (!existingBinding) {
      await gotoWithRetry(page, "/administration/access/bindings");
      await page.getByRole("button", { name: "Создать привязку" }).click();
      const dialog = page.getByRole("dialog", {
        name: "Новая привязка роли",
      });
      await dialog
        .getByLabel("Тип субъекта")
        .selectOption(setup.candidate.kind);
      await dialog
        .locator(".form-grid select")
        .nth(1)
        .selectOption({ index: 1 });
      await dialog.getByLabel("Версия роли").selectOption(setup.roleVersionRef);
      const scopeFields = dialog.locator(".scope-editor select");
      await scopeFields.nth(0).selectOption("RESOURCE_INSTANCE");
      await scopeFields.nth(1).selectOption(projectRef);
      await scopeFields.nth(2).selectOption("AGENT");
      await scopeFields.nth(3).selectOption(coordinatorRef);
      const creation = page.waitForResponse(
        (response) =>
          response.request().method() === "POST" &&
          new URL(response.url()).pathname ===
            "/api/v1/administration/access/bindings",
      );
      await dialog.getByRole("button", { name: "Создать привязку" }).click();
      expect((await creation).status()).toBe(201);
      const card = page
        .locator(".binding-card")
        .filter({ hasText: setup.candidate.displayName })
        .filter({ hasText: accessRoleName });
      await expect(card).toContainText(accessRoleName);
      await expect(card).toContainText(coordinatorName);
    }

    await gotoWithRetry(page, "/administration/access/effective");
    const form = page.locator(".effective-form");
    await form.getByLabel("Субъект").selectOption(setup.candidate.ref);
    await form.getByLabel("Действие").selectOption("agent.launch");
    const effectiveScopeFields = form.locator(".scope-editor select");
    await effectiveScopeFields.nth(0).selectOption("RESOURCE_INSTANCE");
    await effectiveScopeFields.nth(1).selectOption(projectRef);
    await effectiveScopeFields.nth(2).selectOption("AGENT");
    await effectiveScopeFields.nth(3).selectOption(coordinatorRef);
    const allowed = page.waitForResponse(
      (response) =>
        response.request().method() === "POST" &&
        new URL(response.url()).pathname.endsWith("/effective-access/explain"),
    );
    await form.getByRole("button", { name: "Объяснить решение" }).click();
    expect((await allowed).status()).toBe(200);
    const result = page.locator(".result-panel");
    await expect(result).toContainText("Разрешено");
    await expect(result).toContainText("Область привязки совпадает");

    await effectiveScopeFields.nth(3).selectOption(analystRef);
    const denied = page.waitForResponse(
      (response) =>
        response.request().method() === "POST" &&
        new URL(response.url()).pathname.endsWith("/effective-access/explain"),
    );
    await form.getByRole("button", { name: "Объяснить решение" }).click();
    expect((await denied).status()).toBe(200);
    await expect(result).toContainText("Запрещено");
    await expect(result).toContainText(
      "Подходящая разрешающая привязка отсутствует",
    );
  });

  test("core остаётся готовым без подключённых интеграций", async ({
    page,
  }) => {
    requireRefs(
      "firstRunRef",
      "workflowRunRef",
      "analystRef",
      "writerRef",
      "workflowRef",
    );
    await gotoWithRetry(page, "/integrations");
    await expectPageHeading(page, "Интеграции");
    await expect(
      page.getByText("Платформа работает без интеграций"),
    ).toBeVisible();
    await expect(page.getByText(/Подключения необязательны/)).toBeVisible();
    await expect(page.getByText(/Mattermost обязателен/i)).toHaveCount(0);

    await gotoWithRetry(page, `/runs/${firstRunRef}`);
    await expectRunState(page, "Завершён");
    await gotoWithRetry(page, `/runs/${workflowRunRef}`);
    await expectRunState(page, "Завершён");
    expect(analystRef).toBeTruthy();
    expect(writerRef).toBeTruthy();
    expect(workflowRef).toBeTruthy();
  });

  test("корзина восстанавливает файл и необратимо удаляет точную S3-версию", async ({
    page,
  }) => {
    requireRefs("projectRef");
    await gotoWithRetry(page, `/projects/${projectRef}/files`);
    await expectPageHeading(page, "Файлы и знания");

    const restoreArtifact = await uploadFilesWorkspaceArtifact(
      page,
      `${environment.resourcePrefix}-trash-restore.txt`,
      "restore fixture",
    );
    const deleted = await operateArtifactLifecycle(
      page,
      restoreArtifact.fileName,
      restoreArtifact.ref,
      "DELETE",
    );
    expect(deleted.lifecycleState).toBe("DELETED");
    expect(deleted.deletedAt).toBeTruthy();
    expect(deleted.purgeAfter).toBeTruthy();
    const retentionMilliseconds =
      Date.parse(deleted.purgeAfter ?? "") -
      Date.parse(deleted.deletedAt ?? "");
    expect(retentionMilliseconds).toBeGreaterThan(29.9 * 24 * 60 * 60 * 1000);
    expect(retentionMilliseconds).toBeLessThan(30.1 * 24 * 60 * 60 * 1000);

    await page.getByRole("link", { name: "Корзина", exact: true }).click();
    await expect(page).toHaveURL(
      new RegExp(`/projects/${projectRef}/files/trash$`),
    );
    const restored = await operateArtifactLifecycle(
      page,
      restoreArtifact.fileName,
      restoreArtifact.ref,
      "RESTORE",
    );
    expect(restored.lifecycleState).toBe("ACTIVE");
    expect(restored.deletedAt).toBeUndefined();
    expect(restored.purgeAfter).toBeUndefined();

    await page.getByRole("link", { name: "Файлы", exact: true }).click();
    await expect(page).toHaveURL(new RegExp(`/projects/${projectRef}/files$`));
    const deletedAgain = await operateArtifactLifecycle(
      page,
      restoreArtifact.fileName,
      restoreArtifact.ref,
      "DELETE",
    );
    expect(deletedAgain.lifecycleState).toBe("DELETED");
    const purgeReceipt = artifactStorageReceipt("purge", restoreArtifact.ref);
    await runArtifactStorageFixture(
      "capture",
      restoreArtifact.ref,
      purgeReceipt,
    );
    await page.getByRole("link", { name: "Корзина", exact: true }).click();
    await expect(page).toHaveURL(
      new RegExp(`/projects/${projectRef}/files/trash$`),
    );
    const purgeResponse = page.waitForResponse(
      (response) =>
        response.request().method() === "DELETE" &&
        new URL(response.url()).pathname ===
          `/api/v1/artifacts/${restoreArtifact.ref}/purge`,
    );
    await page
      .getByRole("button", { name: new RegExp(restoreArtifact.fileName) })
      .first()
      .click();
    await page
      .locator(".file-details")
      .getByRole("button", { name: "Удалить навсегда", exact: true })
      .click();
    const purgeDialog = page.getByRole("dialog", {
      name: "Удалить файл навсегда?",
    });
    await purgeDialog
      .getByRole("button", { name: "Удалить навсегда", exact: true })
      .click();
    expect((await purgeResponse).status()).toBe(200);
    await runArtifactStorageFixture(
      "assert-absent",
      restoreArtifact.ref,
      purgeReceipt,
    );

    await page.getByRole("link", { name: "Файлы", exact: true }).click();
    await expect(page).toHaveURL(new RegExp(`/projects/${projectRef}/files$`));
    const emptyTrashArtifact = await uploadFilesWorkspaceArtifact(
      page,
      `${environment.resourcePrefix}-empty-trash.txt`,
      "empty trash fixture",
    );
    await operateArtifactLifecycle(
      page,
      emptyTrashArtifact.fileName,
      emptyTrashArtifact.ref,
      "DELETE",
    );
    const emptyReceipt = artifactStorageReceipt(
      "empty",
      emptyTrashArtifact.ref,
    );
    await runArtifactStorageFixture(
      "capture",
      emptyTrashArtifact.ref,
      emptyReceipt,
    );
    await page.getByRole("link", { name: "Корзина", exact: true }).click();
    await expect(page).toHaveURL(
      new RegExp(`/projects/${projectRef}/files/trash$`),
    );
    await page.getByRole("button", { name: "Очистить корзину" }).click();
    const emptyDialog = page.getByRole("dialog", {
      name: "Очистить всю корзину?",
    });
    await emptyDialog.locator('input[type="text"]').fill("УДАЛИТЬ НАВСЕГДА");
    await emptyDialog
      .getByRole("button", { name: "Очистить корзину", exact: true })
      .click();
    await expect
      .poll(
        async () =>
          (await readArtifact(page, emptyTrashArtifact.ref)).lifecycleState,
        { timeout: 60_000 },
      )
      .toBe("PURGED");
    await runArtifactStorageFixture(
      "assert-absent",
      emptyTrashArtifact.ref,
      emptyReceipt,
    );

    await page.getByRole("link", { name: "Файлы", exact: true }).click();
    await expect(page).toHaveURL(new RegExp(`/projects/${projectRef}/files$`));
    const retentionArtifact = await uploadFilesWorkspaceArtifact(
      page,
      `${environment.resourcePrefix}-retention-clock.txt`,
      "retention clock fixture",
    );
    await operateArtifactLifecycle(
      page,
      retentionArtifact.fileName,
      retentionArtifact.ref,
      "DELETE",
    );
    const retentionReceipt = artifactStorageReceipt(
      "retention",
      retentionArtifact.ref,
    );
    await runArtifactStorageFixture(
      "capture",
      retentionArtifact.ref,
      retentionReceipt,
    );
    await runArtifactStorageFixture(
      "accelerate-retention",
      retentionArtifact.ref,
      retentionReceipt,
    );
  });

  test("финальная визуальная приёмка: run canvas", async ({ page }) => {
    requireRefs("projectRef", "workflowRunRef");
    await page.setViewportSize({ width: 1920, height: 1080 });
    await gotoWithRetry(page, `/projects/${projectRef}/runs/${workflowRunRef}`);

    const workspace = page.locator(".run-workspace");
    const summary = workspace.locator(".run-canvas-summary");
    const workspaceToolbar = workspace.locator(".run-workspace-toolbar");
    const graphToolbar = workspace.locator(".graph-toolbar");
    const legend = workspace.locator(".graph-legend");
    await expect(workspace).toBeVisible();
    await expect(summary).toBeVisible();
    await expect(workspaceToolbar).toBeVisible();
    await expect(graphToolbar).toBeVisible();
    await expect(legend).toBeVisible();
    await expect(graphToolbar.locator(".graph-view-switch")).toBeVisible();
    await expect(
      graphToolbar.getByRole("button", { name: "Уменьшить масштаб" }),
    ).toBeVisible();
    await expect(
      graphToolbar.getByRole("button", { name: "Увеличить масштаб" }),
    ).toBeVisible();

    const viewport = page.viewportSize();
    if (!viewport) throw new Error("browser viewport is unavailable");
    const verticalOverflow = await page.evaluate(() => ({
      body: document.body.scrollHeight - document.body.clientHeight,
      document:
        document.documentElement.scrollHeight -
        document.documentElement.clientHeight,
    }));
    expect(verticalOverflow.body).toBeLessThanOrEqual(1);
    expect(verticalOverflow.document).toBeLessThanOrEqual(1);

    const sidebarBox = await visualBox(page.locator(".sidebar"), "sidebar");
    const workspaceBox = await visualBox(workspace, "run workspace");
    const graphCanvasBox = await visualBox(
      workspace.locator(".graph-canvas-shell"),
      "graph canvas",
    );
    expectNear(workspaceBox.left, sidebarBox.right, "workspace left edge");
    expectNear(workspaceBox.right, viewport.width, "workspace right edge");
    expectNear(workspaceBox.bottom, viewport.height, "workspace bottom edge");
    expectNear(graphCanvasBox.left, workspaceBox.left, "canvas left edge");
    expectNear(graphCanvasBox.top, workspaceBox.top, "canvas top edge");
    expectNear(graphCanvasBox.right, workspaceBox.right, "canvas right edge");
    expectNear(
      graphCanvasBox.bottom,
      workspaceBox.bottom,
      "canvas bottom edge",
    );

    const panels = [
      { label: "summary", box: await visualBox(summary, "summary") },
      {
        label: "workspace toolbar",
        box: await visualBox(workspaceToolbar, "workspace toolbar"),
      },
      {
        label: "graph toolbar",
        box: await visualBox(graphToolbar, "graph toolbar"),
      },
      { label: "legend", box: await visualBox(legend, "legend") },
    ];
    const [
      summaryPanel,
      workspaceToolbarPanel,
      graphToolbarPanel,
      legendPanel,
    ] = panels;
    if (
      !summaryPanel ||
      !workspaceToolbarPanel ||
      !graphToolbarPanel ||
      !legendPanel
    ) {
      throw new Error("run canvas panels are unavailable");
    }
    expectNear(
      summaryPanel.box.left - workspaceBox.left,
      14,
      "summary left offset",
    );
    expectNear(
      summaryPanel.box.top - workspaceBox.top,
      14,
      "summary top offset",
    );
    expectNear(
      (workspaceToolbarPanel.box.left + workspaceToolbarPanel.box.right) / 2,
      (workspaceBox.left + workspaceBox.right) / 2,
      "workspace toolbar center",
    );
    expectNear(
      graphToolbarPanel.box.right,
      workspaceBox.right - 14,
      "graph toolbar right offset",
    );
    expectNear(
      graphToolbarPanel.box.top - workspaceBox.top,
      14,
      "graph toolbar top offset",
    );
    expectNear(
      legendPanel.box.left - workspaceBox.left,
      14,
      "legend left offset",
    );
    expectNear(
      legendPanel.box.bottom,
      workspaceBox.bottom - 14,
      "legend bottom offset",
    );
    for (let left = 0; left < panels.length; left += 1) {
      for (let right = left + 1; right < panels.length; right += 1) {
        const leftPanel = panels[left];
        const rightPanel = panels[right];
        if (!leftPanel || !rightPanel)
          throw new Error("run canvas panel pair is unavailable");
        expectNoIntersection(leftPanel, rightPanel);
      }
    }
  });

  test("финальная визуальная приёмка: badges", async ({ page }) => {
    requireRefs("projectRef", "workflowRunRef");
    await page.setViewportSize({ width: 1920, height: 1080 });
    await gotoWithRetry(page, `/projects/${projectRef}/runs/${workflowRunRef}`);
    await expect
      .poll(() => page.locator(".status-badge:visible").count())
      .toBeGreaterThanOrEqual(4);
    const samples = await page.locator(".status-badge").evaluateAll((badges) =>
      badges.flatMap((badge) => {
        const box = badge.getBoundingClientRect();
        if (box.width === 0 || box.height === 0) return [];
        const style = getComputedStyle(badge);
        const parentBox = badge.parentElement?.getBoundingClientRect();
        return [
          {
            alignSelf: style.alignSelf,
            className: badge.className,
            display: style.display,
            flexGrow: style.flexGrow,
            grandparentClassName:
              badge.parentElement?.parentElement?.className ?? "",
            height: box.height,
            parentHeight: parentBox?.height ?? 0,
            parentClassName: badge.parentElement?.className ?? "",
            text: badge.textContent.trim(),
            width: box.width,
          },
        ];
      }),
    );
    expect(samples.length).toBeGreaterThanOrEqual(4);
    for (const sample of samples) {
      const diagnostic = JSON.stringify(sample);
      expect(sample.display, diagnostic).toBe("inline-flex");
      expect(sample.alignSelf, diagnostic).not.toBe("stretch");
      expect(Number(sample.flexGrow), diagnostic).toBe(0);
      expect(sample.height, diagnostic).toBeGreaterThan(0);
      expect(sample.height, diagnostic).toBeLessThanOrEqual(32);
      if (sample.parentHeight > 48) {
        expect(sample.height, diagnostic).toBeLessThan(sample.parentHeight);
      }
      expect(sample.width, diagnostic).toBeLessThan(640);
    }
  });

  test("финальная визуальная приёмка: semantic modals", async ({ page }) => {
    requireRefs("projectRef", "uploadedArtifactRef");
    await page.setViewportSize({ width: 1920, height: 1080 });
    await gotoWithRetry(
      page,
      `/projects/${projectRef}/files?artifactRef=${encodeURIComponent(uploadedArtifactRef)}`,
    );
    const details = page.locator(".file-details");
    await expect(details).toBeVisible();
    await details.getByRole("button", { name: "Открыть", exact: true }).click();
    const dialog = page.getByRole("dialog", { name: uploadedFileName });
    await expect(dialog).toBeVisible();
    const dialogBox = await expectInsideViewport(page, dialog, "file preview");
    expect(dialogBox.width).toBeGreaterThanOrEqual(1536);
    const overflow = await dialog.evaluate((panel) => {
      const body = panel.querySelector<HTMLElement>(".modal__body");
      return {
        body: body ? body.scrollWidth - body.clientWidth : Number.NaN,
        panel: panel.scrollWidth - panel.clientWidth,
      };
    });
    expect(Number.isFinite(overflow.body)).toBe(true);
    expect(overflow.body).toBeLessThanOrEqual(1);
    expect(overflow.panel).toBeLessThanOrEqual(1);
  });

  test("финальная визуальная приёмка: files workspace", async ({ page }) => {
    requireRefs("projectRef", "uploadedArtifactRef");
    await page.setViewportSize({ width: 1920, height: 1080 });
    await gotoWithRetry(
      page,
      `/projects/${projectRef}/files?artifactRef=${encodeURIComponent(uploadedArtifactRef)}`,
    );
    const filesPage = page.locator(".files-page");
    const workspace = page.locator(".files-workspace");
    const layout = workspace.locator(".files-workspace__layout--details");
    const collection = layout.locator(".files-workspace__scroll");
    const details = layout.locator(".file-details");
    await expect(layout).toBeVisible();
    await expect(details).toBeVisible();

    const viewport = page.viewportSize();
    if (!viewport) throw new Error("browser viewport is unavailable");
    const sidebarBox = await visualBox(page.locator(".sidebar"), "sidebar");
    const pageBox = await visualBox(filesPage, "files page");
    const workspaceBox = await visualBox(workspace, "files workspace");
    const layoutBox = await visualBox(layout, "files layout");
    const collectionBox = await visualBox(collection, "file collection");
    const detailsBox = await visualBox(details, "file details");
    const padding = await filesPage.evaluate((element) => {
      const style = getComputedStyle(element);
      return {
        left: Number.parseFloat(style.paddingLeft),
        right: Number.parseFloat(style.paddingRight),
      };
    });
    expectNear(pageBox.left, sidebarBox.right, "files page left edge");
    expectNear(pageBox.right, viewport.width, "files page right edge");
    expectNear(
      workspaceBox.left,
      pageBox.left + padding.left,
      "files workspace left edge",
    );
    expectNear(
      workspaceBox.right,
      pageBox.right - padding.right,
      "files workspace right edge",
    );
    expect(detailsBox.width).toBeGreaterThanOrEqual(240);
    expect(detailsBox.width).toBeLessThanOrEqual(280);
    expectNear(detailsBox.right, layoutBox.right, "details right edge");
    expect(collectionBox.right).toBeLessThanOrEqual(detailsBox.left + 1);
    expect(collectionBox.width).toBeGreaterThan(detailsBox.width * 3);
  });

  test("финальная визуальная приёмка: assistant entity drawer", async ({
    page,
  }) => {
    requireRefs("projectRef", "coordinatorRef");
    await page.setViewportSize({ width: 1920, height: 1080 });
    const agentPath = `/projects/${projectRef}/agents/${coordinatorRef}`;
    await gotoWithRetry(page, agentPath);
    await expectPageHeading(page, coordinatorName);
    await openKodex(page);
    const drawer = page.getByRole("dialog", { name: "Kodex" });
    await expect(drawer).toHaveAttribute("aria-busy", "false");
    const drawerBox = await expectInsideViewport(page, drawer, "Kodex drawer");
    expect(drawerBox.width).toBeGreaterThanOrEqual(520);
    expect(drawerBox.width).toBeLessThanOrEqual(640);
    expectNear(drawerBox.right, 1920, "Kodex drawer right edge");
    expectNear(drawerBox.top, 0, "Kodex drawer top edge");
    expectNear(drawerBox.bottom, 1080, "Kodex drawer bottom edge");

    const context = drawer.locator(".assistant-context-strip");
    await expect(context).toContainText(coordinatorName);
    await expect(context).toContainText(agentPath);
    await expect(drawer.locator(".assistant-drawer__header")).toBeVisible();
    await expect(drawer.locator(".assistant-composer")).toBeVisible();
    const overflow = await drawer.evaluate((element) =>
      [
        element,
        element.querySelector<HTMLElement>(".assistant-drawer__header"),
        element.querySelector<HTMLElement>(".assistant-context-strip"),
        element.querySelector<HTMLElement>(".assistant-composer"),
      ].map((item) =>
        item
          ? {
              clientWidth: item.clientWidth,
              overflow: item.scrollWidth - item.clientWidth,
            }
          : { clientWidth: 0, overflow: Number.NaN },
      ),
    );
    for (const item of overflow) {
      expect(item.clientWidth).toBeGreaterThan(0);
      expect(Number.isFinite(item.overflow)).toBe(true);
      expect(item.overflow).toBeLessThanOrEqual(1);
    }
  });

  test("административные экраны и security boundary дают ожидаемый readback", async ({
    browser,
    page,
  }) => {
    requireRefs("projectRef", "automationRef", "uploadedArtifactRef");
    await gotoWithRetry(page, "/administration");
    await expectPageHeading(page, "Администрирование");
    await expect(page.locator("#main-content")).toContainText("Core-платформа");

    await gotoWithRetry(page, "/administration/access");
    await expectPageHeading(page, "Участники и доступ");
    await expect(page.locator(".access-table__row")).not.toHaveCount(0);
    await gotoWithRetry(page, `/projects/${projectRef}/members`);
    await expectPageHeading(page, "Участники и доступ");
    await expect(page.locator(".access-table__row")).not.toHaveCount(0);

    await gotoWithRetry(
      page,
      `/administration/audit?projectRef=${encodeURIComponent(projectRef)}`,
    );
    await expectPageHeading(page, "Аудит и диагностика");
    const auditSearch = page.getByRole("searchbox", {
      name: "Поиск по аудиту",
    });
    await auditSearch.fill(automationName);
    await expect(page.getByRole("table")).toContainText(automationName);
    await auditSearch.fill(uploadedFileName);
    await expect(page.getByRole("table")).toContainText(uploadedFileName);

    const unauthenticated = await browser.newContext({
      baseURL: environment.baseURL,
      locale: "ru-RU",
    });
    try {
      for (const path of [
        "/api/v1/bootstrap",
        "/api/v1/projects",
        "/api/v1/runs",
      ]) {
        const response = await unauthenticated.request.get(path);
        expect(response.status(), path).toBe(401);
      }
    } finally {
      await unauthenticated.close();
    }

    const projectsBeforeRejectedMutations = await page.evaluate(async () => {
      const response = await fetch("/api/v1/projects?pageSize=100");
      const body = (await response.json()) as { items: unknown[] };
      return body.items.length;
    });
    const input = {
      name: `${environment.resourcePrefix} — запрещённый проект`,
      purpose: "Этот объект не должен быть создан.",
      language: "ru",
    };
    const missingCSRF = await page.request.post("/api/v1/projects", {
      data: input,
      headers: { "Idempotency-Key": crypto.randomUUID() },
    });
    expect(missingCSRF.status()).toBe(403);
    expect((await missingCSRF.json()) as { code?: string }).toMatchObject({
      code: "CSRF_REJECTED",
    });

    const invalidCSRF = await page.request.post("/api/v1/projects", {
      data: input,
      headers: {
        "Idempotency-Key": crypto.randomUUID(),
        "X-CSRF-Token": "invalid-csrf-token-value",
      },
    });
    expect(invalidCSRF.status()).toBe(403);
    expect((await invalidCSRF.json()) as { code?: string }).toMatchObject({
      code: "CSRF_REJECTED",
    });

    const csrf = await page.evaluate(() => {
      const prefix = "__Host-kodex-csrf=";
      const value = document.cookie
        .split(";")
        .map((item) => item.trim())
        .find((item) => item.startsWith(prefix));
      return value ? decodeURIComponent(value.slice(prefix.length)) : "";
    });
    expect(csrf.length).toBeGreaterThanOrEqual(43);
    const foreignOrigin = await page.request.post("/api/v1/projects", {
      data: input,
      headers: {
        Origin: "https://foreign.invalid",
        "Idempotency-Key": crypto.randomUUID(),
        "X-CSRF-Token": csrf,
      },
    });
    expect(foreignOrigin.status()).toBe(403);
    const projectsAfterRejectedMutations = await page.evaluate(async () => {
      const response = await fetch("/api/v1/projects?pageSize=100");
      const body = (await response.json()) as { items: unknown[] };
      return body.items.length;
    });
    expect(projectsAfterRejectedMutations).toBe(
      projectsBeforeRejectedMutations,
    );

    const currentUserMenuButton = page.locator("button[aria-haspopup='menu']");
    await expect(currentUserMenuButton).toBeVisible();
    await currentUserMenuButton.click();
    await expect(currentUserMenuButton).toHaveAttribute(
      "aria-expanded",
      "true",
    );
    const currentUserMenu = page.getByRole("menu", {
      name: /owner Owner, роль: Владелец/,
    });
    await expect(currentUserMenu).toBeVisible();
    const logoutButton = currentUserMenu.getByRole("button", {
      name: "Выйти",
      exact: true,
    });
    await expect(logoutButton).toBeVisible();
    await logoutButton.click();
    await expect(
      page.getByRole("button", { name: "Войти", exact: true }),
    ).toBeVisible();
    expect(
      await page.evaluate(async () =>
        fetch("/api/v1/projects").then((response) => response.status),
      ),
    ).toBe(401);
  });
});

function persistRefs(): void {
  saveDiscoveryRefs(environment.resourcePrefix, currentRefs());
}

function currentRefs(): DiscoveryRefs {
  return {
    projectRef,
    coordinatorRef,
    analystRef,
    automationRef,
    writerRef,
    workflowRef,
    firstRunRef,
    continuationRunRef,
    instructionRunRef,
    publishedInstructionRef,
    runtimeEnvironmentRef,
    scheduledRunRef,
    workflowRunRef,
    uploadedArtifactRef,
  };
}

function requireRefs(...required: ReadonlyArray<keyof DiscoveryRefs>): void {
  if (!discoveryMode) return;
  const persisted = loadDiscoveryRefs(environment.resourcePrefix);
  projectRef = persisted.projectRef ?? projectRef;
  coordinatorRef = persisted.coordinatorRef ?? coordinatorRef;
  analystRef = persisted.analystRef ?? analystRef;
  automationRef = persisted.automationRef ?? automationRef;
  writerRef = persisted.writerRef ?? writerRef;
  workflowRef = persisted.workflowRef ?? workflowRef;
  firstRunRef = persisted.firstRunRef ?? firstRunRef;
  continuationRunRef = persisted.continuationRunRef ?? continuationRunRef;
  instructionRunRef = persisted.instructionRunRef ?? instructionRunRef;
  publishedInstructionRef =
    persisted.publishedInstructionRef ?? publishedInstructionRef;
  runtimeEnvironmentRef =
    persisted.runtimeEnvironmentRef ?? runtimeEnvironmentRef;
  scheduledRunRef = persisted.scheduledRunRef ?? scheduledRunRef;
  workflowRunRef = persisted.workflowRunRef ?? workflowRunRef;
  uploadedArtifactRef = persisted.uploadedArtifactRef ?? uploadedArtifactRef;
  const refs = currentRefs();
  const missing = required.filter((key) => !refs[key]);
  if (missing.length > 0) {
    throw new Error(
      `BLOCKED: отсутствуют prerequisite refs: ${missing.join(", ")}`,
    );
  }
}

async function ensureAuthorizedProviderAffinity(
  page: Page,
  agentRef: string,
  eligibleIndex = 0,
): Promise<void> {
  const preflight = await page.evaluate(async (requestedEligibleIndex) => {
    const response = await fetch(
      "/api/v1/provider-accounts?definitionKey=openai-codex&pageSize=100",
    );
    if (!response.ok) {
      return {
        accountRef: "",
        eligibleCount: 0,
        status: response.status,
        summary: "provider account catalog readback failed",
      };
    }
    const body = (await response.json()) as {
      items: Array<{
        enabled: boolean;
        ready: boolean;
        ref: string;
        state: string;
      }>;
    };
    const eligible = body.items
      .filter(
        (item) => item.state === "AUTHORIZED" && item.enabled && item.ready,
      )
      .toSorted((left, right) => left.ref.localeCompare(right.ref));
    const states = body.items.reduce<Record<string, number>>((result, item) => {
      const key = `${item.state}:${item.enabled ? "enabled" : "disabled"}:${item.ready ? "ready" : "not-ready"}`;
      result[key] = (result[key] ?? 0) + 1;
      return result;
    }, {});
    return {
      accountRef: eligible[requestedEligibleIndex]?.ref ?? "",
      eligibleCount: eligible.length,
      status: response.status,
      summary: Object.entries(states)
        .sort(([left], [right]) => left.localeCompare(right))
        .map(([state, count]) => `${state}=${String(count)}`)
        .join(", "),
    };
  }, eligibleIndex);
  expect(preflight.status, preflight.summary).toBe(200);
  if (!preflight.accountRef) {
    const blocker = `BLOCKED: AUTHORIZED+enabled+ready provider account index ${String(eligibleIndex)} is unavailable; eligible=${String(preflight.eligibleCount)} (${preflight.summary || "empty catalog"})`;
    test.info().annotations.push({ type: "blocked", description: blocker });
    throw new Error(blocker);
  }
  await pinAgentProviderAccount(page, agentRef, preflight.accountRef);
}

async function pinAgentProviderAccount(
  page: Page,
  agentRef: string,
  accountRef: string,
): Promise<void> {
  const result = await page.evaluate(
    async ({ expectedAccountRef, expectedAgentRef }) => {
      const readbackResponse = await fetch(
        `/api/v1/agents/${encodeURIComponent(expectedAgentRef)}/runtime-configuration`,
      );
      if (!readbackResponse.ok) {
        return { status: readbackResponse.status, detail: "runtime readback" };
      }
      const readback = (await readbackResponse.json()) as {
        agentVersion: number;
        configuration: {
          runtimeProfileRef: string;
          model: string;
          providerPolicy: {
            accountCandidates: Array<{ accountRef: string; weight: number }>;
            mode: string;
          };
        };
      };
      const currentCandidates =
        readback.configuration.providerPolicy.accountCandidates;
      if (
        readback.configuration.providerPolicy.mode === "FIXED" &&
        currentCandidates.length === 1 &&
        currentCandidates[0]?.accountRef === expectedAccountRef
      ) {
        return { status: 200, detail: "" };
      }
      const csrfPrefix = `${encodeURIComponent("__Host-kodex-csrf")}=`;
      const csrf = document.cookie
        .split(";")
        .map((part) => part.trim())
        .find((part) => part.startsWith(csrfPrefix))
        ?.slice(csrfPrefix.length);
      if (!csrf) return { status: 0, detail: "CSRF token is unavailable" };
      const publication = await fetch(
        `/api/v1/agents/${encodeURIComponent(expectedAgentRef)}/runtime-configuration`,
        {
          method: "PUT",
          headers: {
            "Content-Type": "application/json",
            "Idempotency-Key": crypto.randomUUID(),
            "If-Match": `"${String(readback.agentVersion)}"`,
            "X-CSRF-Token": decodeURIComponent(csrf),
          },
          body: JSON.stringify({
            runtimeProfileRef: readback.configuration.runtimeProfileRef,
            model: readback.configuration.model,
            providerPolicyMode: "FIXED",
            providerAccounts: [{ accountRef: expectedAccountRef, weight: 1 }],
          }),
        },
      );
      return {
        status: publication.status,
        detail: publication.ok ? "" : (await publication.text()).slice(0, 512),
      };
    },
    { expectedAccountRef: accountRef, expectedAgentRef: agentRef },
  );
  expect(result.status, result.detail).toBe(200);
}

async function resolveArtifactRef(
  page: Page,
  expectedProjectRef: string,
  expectedFileName: string,
): Promise<string> {
  return page.evaluate(
    async ({ projectRef: currentProjectRef, fileName }) => {
      const response = await fetch(
        `/api/v1/projects/${encodeURIComponent(currentProjectRef)}/artifacts?pageSize=100&query=${encodeURIComponent(fileName)}`,
      );
      if (!response.ok) throw new Error("artifact readback failed");
      const body = (await response.json()) as {
        items?: Array<{ ref: string; fileName: string }>;
      };
      return body.items?.find((item) => item.fileName === fileName)?.ref ?? "";
    },
    { projectRef: expectedProjectRef, fileName: expectedFileName },
  );
}

interface ArtifactReadback {
  readonly deletedAt?: string;
  readonly fileName: string;
  readonly lifecycleState: "ACTIVE" | "DELETED" | "PURGE_PENDING" | "PURGED";
  readonly purgeAfter?: string;
  readonly ref: string;
  readonly version: number;
}

interface AttachmentSetReadback {
  readonly items: Array<{
    readonly artifactRef: string;
    readonly artifactRevision: number;
    readonly artifactVersion: number;
  }>;
  readonly ref: string;
  readonly state: "DRAFT" | "FINALIZED";
}

async function readRequestAttachmentSet(
  page: Page,
  response: Response,
): Promise<AttachmentSetReadback> {
  const request = response.request().postDataJSON() as {
    attachmentSetRef?: string;
  };
  expect(request.attachmentSetRef).toMatch(/^aset_[A-Za-z0-9_-]+$/);
  const attachmentSet = await page.evaluate(async (ref) => {
    const read = await fetch(
      `/api/v1/attachment-sets/${encodeURIComponent(ref ?? "")}?pageSize=100`,
    );
    if (!read.ok) {
      throw new Error(`attachment set readback failed: ${String(read.status)}`);
    }
    const body = (await read.json()) as {
      attachmentSet: AttachmentSetReadback;
    };
    return body.attachmentSet;
  }, request.attachmentSetRef);
  expect(attachmentSet.ref).toBe(request.attachmentSetRef);
  expect(attachmentSet.state).toBe("FINALIZED");
  expect(
    attachmentSet.items.every(
      (item) => item.artifactRevision > 0 && item.artifactVersion > 0,
    ),
  ).toBe(true);
  return attachmentSet;
}

async function uploadFilesWorkspaceArtifact(
  page: Page,
  fileName: string,
  content: string,
): Promise<ArtifactReadback> {
  const uploadButton = page
    .locator(".files-workspace")
    .getByRole("button", { name: "Загрузить", exact: true });
  await expect(uploadButton).toBeVisible();
  await expect(uploadButton).toBeEnabled();
  const response = page.waitForResponse(
    (candidate) =>
      candidate.request().method() === "POST" &&
      new URL(candidate.url()).pathname ===
        `/api/v1/projects/${projectRef}/artifacts`,
  );
  const fileChooser = page.waitForEvent("filechooser");
  await uploadButton.click();
  await (
    await fileChooser
  ).setFiles({
    name: fileName,
    mimeType: "text/plain",
    buffer: Buffer.from(content, "utf8"),
  });
  const upload = await response;
  expect(upload.request().headers()["x-file-name"]).toBe(fileName);
  expect(upload.status(), await upload.text()).toBe(201);
  return (await upload.json()) as ArtifactReadback;
}

async function operateArtifactLifecycle(
  page: Page,
  fileName: string,
  artifactRef: string,
  action: "DELETE" | "RESTORE",
): Promise<ArtifactReadback> {
  const path =
    action === "DELETE"
      ? `/api/v1/artifacts/${artifactRef}`
      : `/api/v1/artifacts/${artifactRef}/restore`;
  const method = action === "DELETE" ? "DELETE" : "POST";
  const actionLabel = action === "DELETE" ? "В корзину" : "Восстановить";
  const confirmationLabel =
    action === "DELETE" ? "Переместить в корзину" : "Восстановить";
  const dialogTitle =
    action === "DELETE" ? "Переместить файл в корзину?" : "Восстановить файл?";
  const artifactItem = page
    .locator(`[data-artifact-ref="${artifactRef}"]`)
    .first();
  await expect(artifactItem).toBeVisible();
  const impact =
    action === "DELETE"
      ? page.waitForResponse(
          (candidate) =>
            candidate.request().method() === "GET" &&
            new URL(candidate.url()).pathname ===
              `/api/v1/artifacts/${artifactRef}/impact`,
        )
      : undefined;
  await artifactItem
    .getByRole("button", { name: `${actionLabel}: ${fileName}`, exact: true })
    .click();
  if (impact) {
    const impactResponse = await impact;
    expect(impactResponse.status(), await impactResponse.text()).toBe(200);
  }
  const dialog = page.getByRole("dialog", { name: dialogTitle });
  await expect(dialog).toBeVisible();
  const response = page.waitForResponse(
    (candidate) =>
      candidate.request().method() === method &&
      new URL(candidate.url()).pathname === path,
  );
  await dialog
    .getByRole("button", { name: confirmationLabel, exact: true })
    .click();
  const mutation = await response;
  expect(mutation.status(), await mutation.text()).toBe(200);
  return (await mutation.json()) as ArtifactReadback;
}

async function readArtifact(
  page: Page,
  artifactRef: string,
): Promise<ArtifactReadback> {
  return page.evaluate(async (ref) => {
    const response = await fetch(
      `/api/v1/artifacts/${encodeURIComponent(ref)}`,
    );
    if (!response.ok) {
      throw new Error(`artifact readback failed: ${String(response.status)}`);
    }
    return (await response.json()) as ArtifactReadback;
  }, artifactRef);
}

async function deleteArtifactAtVersion(
  page: Page,
  artifactRef: string,
  version: number,
): Promise<ArtifactReadback> {
  return page.evaluate(
    async ({ ref, expectedVersion }) => {
      const prefix = `${encodeURIComponent("__Host-kodex-csrf")}=`;
      const csrf = document.cookie
        .split(";")
        .map((part) => part.trim())
        .find((part) => part.startsWith(prefix))
        ?.slice(prefix.length);
      if (!csrf) throw new Error("CSRF token is unavailable");
      const impactResponse = await fetch(
        `/api/v1/artifacts/${encodeURIComponent(ref)}/impact?action=DELETE`,
      );
      if (!impactResponse.ok) {
        throw new Error(
          `artifact impact failed: ${String(impactResponse.status)} ${await impactResponse.text()}`,
        );
      }
      const impact = (await impactResponse.json()) as {
        artifactRef: string;
        artifactVersion: number;
        impactDigest: string;
        permitted: boolean;
      };
      if (
        !impact.permitted ||
        impact.artifactRef !== ref ||
        impact.artifactVersion !== expectedVersion
      ) {
        throw new Error(
          `artifact impact does not permit delete: ${JSON.stringify(impact)}`,
        );
      }
      const response = await fetch(
        `/api/v1/artifacts/${encodeURIComponent(ref)}`,
        {
          method: "DELETE",
          headers: {
            "Idempotency-Key": crypto.randomUUID(),
            "If-Match": `"${String(expectedVersion)}"`,
            "X-Impact-Digest": impact.impactDigest,
            "X-CSRF-Token": decodeURIComponent(csrf),
          },
        },
      );
      if (!response.ok) {
        throw new Error(
          `artifact delete failed: ${String(response.status)} ${await response.text()}`,
        );
      }
      return (await response.json()) as ArtifactReadback;
    },
    { ref: artifactRef, expectedVersion: version },
  );
}

function artifactStorageReceipt(label: string, artifactRef: string): string {
  const stateDirectory = process.env.KODEX_E2E_STATE_DIRECTORY;
  if (!stateDirectory || !stateDirectory.startsWith("/")) {
    throw new Error("KODEX_E2E_STATE_DIRECTORY is required for storage E2E");
  }
  return resolve(
    stateDirectory,
    "e2e",
    `${environment.resourcePrefix}-${label}-${artifactRef}.json`,
  );
}

async function runArtifactStorageFixture(
  mode: "capture" | "assert-absent" | "accelerate-retention",
  artifactRef: string,
  receipt: string,
): Promise<void> {
  const repositoryRoot = process.env.KODEX_E2E_REPOSITORY_ROOT;
  const kubeconfig = process.env.KODEX_E2E_KUBECONFIG;
  const context = process.env.KODEX_E2E_KUBE_CONTEXT;
  const stateDirectory = process.env.KODEX_E2E_STATE_DIRECTORY;
  if (
    !repositoryRoot?.startsWith("/") ||
    !kubeconfig?.startsWith("/") ||
    !context ||
    !stateDirectory?.startsWith("/")
  ) {
    throw new Error(
      "local storage E2E requires exact repository, kubeconfig, context and state paths",
    );
  }
  const script = resolve(
    repositoryRoot,
    "scripts/tests/local-artifact-storage-e2e.sh",
  );
  await execFileAsync(
    script,
    [
      mode,
      "--context",
      context,
      "--kubeconfig",
      kubeconfig,
      "--state-directory",
      stateDirectory,
      "--artifact-ref",
      artifactRef,
      "--receipt",
      receipt,
    ],
    {
      cwd: repositoryRoot,
      env: {
        ...process.env,
        KODEX_E2E_CONFIRM_DISPOSABLE:
          "I_UNDERSTAND_THIS_MUTATES_A_DISPOSABLE_INSTALLATION",
      },
      maxBuffer: 2 * 1024 * 1024,
      timeout: 8 * 60 * 1000,
    },
  );
}

async function resolveScheduleRef(
  page: Page,
  expectedProjectRef: string,
  expectedName: string,
): Promise<string> {
  return page.evaluate(
    async ({ projectRef: currentProjectRef, name }) => {
      const response = await fetch(
        `/api/v1/projects/${encodeURIComponent(currentProjectRef)}/schedules?pageSize=100`,
      );
      if (!response.ok) throw new Error("schedule readback failed");
      const body = (await response.json()) as {
        items?: Array<{ ref: string; name: string }>;
      };
      return body.items?.find((item) => item.name === name)?.ref ?? "";
    },
    { projectRef: expectedProjectRef, name: expectedName },
  );
}

async function openGateForRun(
  page: Page,
  runRef: string,
): Promise<{ ref: string; version: number }> {
  let gate: { ref: string; version: number } | undefined;
  await expect
    .poll(
      async () => {
        gate = await page.evaluate(async (expectedRunRef) => {
          try {
            const runResponse = await fetch(
              `/api/v1/runs/${encodeURIComponent(expectedRunRef)}`,
            );
            if (!runResponse.ok) return undefined;
            const run = (await runResponse.json()) as {
              gateRefs?: string[];
            };
            for (const gateRef of run.gateRefs ?? []) {
              const gateResponse = await fetch(
                `/api/v1/owner-gates/${encodeURIComponent(gateRef)}`,
              );
              if (!gateResponse.ok) continue;
              const candidate = (await gateResponse.json()) as {
                ref: string;
                state: string;
                version: number;
              };
              if (candidate.state === "OPEN") {
                return { ref: candidate.ref, version: candidate.version };
              }
            }
            return undefined;
          } catch {
            return undefined;
          }
        }, runRef);
        return Boolean(gate);
      },
      { timeout: 30_000 },
    )
    .toBe(true);
  if (!gate) throw new Error("open owner gate was not found");
  return gate;
}

async function readOwnerGate(
  page: Page,
  gateRef: string,
): Promise<{
  consequencesSummary: string;
  contextSummary: string;
  requestedBy: { displayName: string; ref: string };
  resolutionAttachmentSetRef?: string;
  state: string;
  title: string;
  version: number;
}> {
  return page.evaluate(async (ref) => {
    const response = await fetch(
      `/api/v1/owner-gates/${encodeURIComponent(ref)}`,
    );
    if (!response.ok) {
      throw new Error(`owner gate readback failed: ${String(response.status)}`);
    }
    return (await response.json()) as {
      consequencesSummary: string;
      contextSummary: string;
      requestedBy: { displayName: string; ref: string };
      resolutionAttachmentSetRef?: string;
      state: string;
      title: string;
      version: number;
    };
  }, gateRef);
}

async function resolveGateAtVersion(
  page: Page,
  gateRef: string,
  version: number,
): Promise<{ code: string; status: number }> {
  return page.evaluate(
    async ({ expectedGateRef, expectedVersion }) => {
      const prefix = `${encodeURIComponent("__Host-kodex-csrf")}=`;
      const csrf = document.cookie
        .split(";")
        .map((part) => part.trim())
        .find((part) => part.startsWith(prefix))
        ?.slice(prefix.length);
      if (!csrf) throw new Error("CSRF token is unavailable");
      const response = await fetch(
        `/api/v1/owner-gates/${encodeURIComponent(expectedGateRef)}/resolution`,
        {
          method: "POST",
          headers: {
            "Content-Type": "application/json",
            "Idempotency-Key": crypto.randomUUID(),
            "If-Match": `"${String(expectedVersion)}"`,
            "X-CSRF-Token": decodeURIComponent(csrf),
          },
          body: JSON.stringify({ decision: "APPROVE" }),
        },
      );
      let code = "";
      if (!response.ok) {
        const problem = (await response.json()) as { code?: string };
        code = problem.code ?? "";
      }
      return { code, status: response.status };
    },
    { expectedGateRef: gateRef, expectedVersion: version },
  );
}

async function readRunSessionRef(page: Page, runRef: string): Promise<string> {
  let sessionRef = "";
  await expect
    .poll(
      async () => {
        sessionRef = await page.evaluate(async (currentRunRef) => {
          try {
            const response = await fetch(
              `/api/v1/runs/${encodeURIComponent(currentRunRef)}`,
            );
            if (!response.ok) return "";
            const body = (await response.json()) as { sessionRef?: string };
            return body.sessionRef ?? "";
          } catch {
            return "";
          }
        }, runRef);
        return sessionRef;
      },
      { timeout: 30_000 },
    )
    .not.toBe("");
  return sessionRef;
}

interface MeasuredTokenUsage {
  readonly cachedInputTokens: number;
  readonly cacheWriteInputTokens: number;
  readonly inputTokens: number;
  readonly modelContextWindow: number;
  readonly outputTokens: number;
  readonly reasoningOutputTokens: number;
  readonly totalTokens: number;
}

async function readRunUsage(
  page: Page,
  runRef: string,
): Promise<MeasuredTokenUsage> {
  let usage: MeasuredTokenUsage | undefined;
  await expect
    .poll(
      async () => {
        usage = await page.evaluate(async (currentRunRef) => {
          try {
            const response = await fetch(
              `/api/v1/runs/${encodeURIComponent(currentRunRef)}`,
            );
            if (!response.ok) return undefined;
            const body = (await response.json()) as {
              usage?: MeasuredTokenUsage;
            };
            return body.usage;
          } catch {
            return undefined;
          }
        }, runRef);
        return Boolean(usage);
      },
      { timeout: 30_000, intervals: [200, 600, 1_000] },
    )
    .toBe(true);
  if (!usage) throw new Error("run usage is unavailable after bounded retry");
  return usage;
}

function assertValidMeasuredUsage(usage: MeasuredTokenUsage): void {
  expect(usage.totalTokens).toBeGreaterThan(0);
  expect(usage.inputTokens).toBeGreaterThan(0);
  expect(usage.outputTokens).toBeGreaterThan(0);
  expect(usage.modelContextWindow).toBeGreaterThan(0);
  expect(usage.totalTokens).toBe(usage.inputTokens + usage.outputTokens);
  expect(usage.cachedInputTokens).toBeLessThanOrEqual(usage.inputTokens);
  expect(usage.cacheWriteInputTokens).toBeLessThanOrEqual(usage.inputTokens);
  expect(usage.reasoningOutputTokens).toBeLessThanOrEqual(usage.outputTokens);
}

async function waitForAgentVersionReadback(
  page: Page,
  agentRef: string,
): Promise<void> {
  await expect
    .poll(
      async () => {
        const visibleVersion = Number.parseInt(
          (await page
            .locator(".agent-detail-page")
            .getAttribute("data-agent-version")) ?? "0",
        );
        const authoritativeVersion = await page.evaluate(async (ref) => {
          try {
            const response = await fetch(
              `/api/v1/agents/${encodeURIComponent(ref)}`,
            );
            if (!response.ok) return 0;
            return (
              ((await response.json()) as { version?: number }).version ?? 0
            );
          } catch {
            return 0;
          }
        }, agentRef);
        if (
          authoritativeVersion > 0 &&
          visibleVersion !== authoritativeVersion
        ) {
          await page.reload();
        }
        return (
          authoritativeVersion > 0 && visibleVersion === authoritativeVersion
        );
      },
      { timeout: 30_000, intervals: [250, 500, 1_000] },
    )
    .toBe(true);
}

function scheduleCommandResponse(page: Page): Promise<Response> {
  return page.waitForResponse((response) => {
    const pathname = new URL(response.url()).pathname;
    return (
      response.request().method() === "POST" &&
      /^\/api\/v1\/schedules\/[^/]+\/commands$/.test(pathname)
    );
  });
}

async function readScheduleRevisionState(
  page: Page,
  scheduleRef: string,
): Promise<{ revision: number; task: string }> {
  return page.evaluate(async (ref) => {
    const response = await fetch(
      `/api/v1/schedules/${encodeURIComponent(ref)}`,
    );
    if (!response.ok)
      throw new Error(
        `Schedule readback failed with HTTP ${String(response.status)}`,
      );
    const schedule = (await response.json()) as {
      currentRevision?: { input?: { task?: unknown }; revision?: unknown };
    };
    return {
      revision:
        typeof schedule.currentRevision?.revision === "number"
          ? schedule.currentRevision.revision
          : 0,
      task:
        typeof schedule.currentRevision?.input?.task === "string"
          ? schedule.currentRevision.input.task
          : "",
    };
  }, scheduleRef);
}

async function mutationFailureDiagnostic(
  response: Response,
  page: Page,
  resource?: { kind: "agent"; ref: string },
): Promise<string> {
  let problemCode = "";
  try {
    problemCode = ((await response.json()) as { code?: string }).code ?? "";
  } catch {
    problemCode = "";
  }
  let authoritativeVersion = 0;
  if (resource?.kind === "agent") {
    authoritativeVersion = await page.evaluate(async (ref) => {
      const current = await fetch(`/api/v1/agents/${encodeURIComponent(ref)}`);
      if (!current.ok) return 0;
      return ((await current.json()) as { version?: number }).version ?? 0;
    }, resource.ref);
  }
  const request = response.request();
  return [
    `mutation ${request.method()} ${new URL(response.url()).pathname}`,
    `status=${String(response.status())}`,
    `code=${problemCode || "none"}`,
    `if-match=${request.headers()["if-match"] ?? "missing"}`,
    `authoritative-version=${String(authoritativeVersion)}`,
  ].join("; ");
}
