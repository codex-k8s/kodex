import {
  type Browser,
  type BrowserContext,
  type Download,
  type Page,
  type Request,
  type Response,
} from "@playwright/test";
import { readFile } from "node:fs/promises";

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
const projectName = `${environment.resourcePrefix} — отдел продаж`;
const coordinatorName = `${environment.resourcePrefix} — координатор продаж`;
const analystName = `${environment.resourcePrefix} — аналитик лидов`;
const writerName = `${environment.resourcePrefix} — автор предложений`;
const workflowName = `${environment.resourcePrefix} — квалификация лида`;
const uploadedFileName = `${environment.resourcePrefix}-lead-context.txt`;
const automationName = `${environment.resourcePrefix} — ежечасная проверка лидов`;
const automationTask =
  "Проверь новые синтетические лиды и подготовь краткий статус.";

const initialRefs = loadDiscoveryRefs(environment.resourcePrefix);
let projectRef = initialRefs.projectRef ?? "";
let coordinatorRef = initialRefs.coordinatorRef ?? "";
let analystRef = initialRefs.analystRef ?? "";
let writerRef = initialRefs.writerRef ?? "";
let workflowRef = initialRefs.workflowRef ?? "";
let firstRunRef = initialRefs.firstRunRef ?? "";
let continuationRunRef = initialRefs.continuationRunRef ?? "";
let instructionRunRef = initialRefs.instructionRunRef ?? "";
let publishedInstructionRef = initialRefs.publishedInstructionRef ?? "";
let scheduledRunRef = initialRefs.scheduledRunRef ?? "";
let workflowRunRef = initialRefs.workflowRunRef ?? "";

test.describe("web-only fresh installation", () => {
  test.describe.configure({ mode: discoveryMode ? "default" : "serial" });

  test("первый вход, горячий помощник и первый Проект", async ({ page }) => {
    await gotoWithRetry(page, "/onboarding");
    await expectPageHeading(page, "Настроим Kodex");
    await expect(
      page.getByRole("heading", { name: "Системный помощник готов" }),
    ).toBeVisible();
    await expect(
      page.getByText("Внешние интеграции не нужны для начала работы"),
    ).toBeVisible();
    await expect(
      page.locator("#main-content").getByText("Готов", { exact: true }),
    ).toBeVisible();

    await page.getByRole("link", { name: "Начать с помощником" }).click();
    await expectPageHeading(page, "Помощник Kodex");
    await expect(
      page.locator("#main-content").getByText(/Системный.*неудаляемый/),
    ).toBeVisible();

    if (discoveryMode && projectRef) {
      await gotoWithRetry(page, `/projects/${projectRef}`);
      await expectPageHeading(page, projectName);
      const currentUser = page.locator("details.current-user-menu > summary");
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
    await page
      .getByLabel("Опишите, что нужно настроить или запустить")
      .fill(prompt);
    await page.getByRole("button", { name: "Отправить помощнику" }).click();
    const plan = page.locator(".assistant-plan").last();
    await expect(plan).toContainText(projectName, { timeout: 120_000 });
    const applyPlan = plan.getByRole("button", {
      name: "Применить разрешённые изменения",
    });
    await applyPlan.click();
    await expect(applyPlan).toHaveCount(0);

    await gotoWithRetry(page, "/projects");
    const projectLink = page.getByRole("link", {
      name: new RegExp(projectName),
    });
    await expect(projectLink).toBeVisible();
    await projectLink.click();
    await expect(page).toHaveURL(/\/projects\/[^/]+$/);
    projectRef = routeRef(page, "projects");
    persistRefs();
    const currentUser = page.locator("details.current-user-menu > summary");
    await expect(currentUser).toBeVisible();
    await expect(currentUser).toHaveAttribute(
      "aria-label",
      /owner.*роль: Владелец/i,
    );
    await waitForConnected(page);
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

    firstRunRef = await launchAgent(
      page,
      "Для нового B2B-лида используй код ALPHA-482. Подготовь краткий план квалификации и создай файл lead-plan.txt с итогом.",
    );
    persistRefs();
    await expectRunState(page, /В очереди|Выполняется/);
    await waitForTerminalSuccess(page);
    const usageBeforeReload = await readRunUsage(page, firstRunRef);
    assertValidMeasuredUsage(usageBeforeReload);
    await expect(page.locator(".token-usage")).toContainText(
      new Intl.NumberFormat("ru-RU").format(usageBeforeReload.totalTokens),
    );
    await page.reload();
    await waitForConnected(page);
    await expectRunState(page, "Завершён");
    expect(await readRunUsage(page, firstRunRef)).toEqual(usageBeforeReload);
    await expect(
      page.getByRole("heading", { name: "Результаты и файлы" }),
    ).toBeVisible();

    const artifact = page
      .locator(".artifact-row")
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
        artifact.click(),
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

    const continuation = page.getByRole("heading", {
      name: "Продолжить сессию",
    });
    await expect(continuation).toBeVisible();
    const initialSessionRef = await readRunSessionRef(page, firstRunRef);
    await page
      .getByLabel("Дополнительное задание")
      .fill(
        "Добавь два вопроса для следующего контакта и повтори код лида из предыдущего сообщения.",
      );
    const continuationResponsePromise = page.waitForResponse((response) => {
      const url = new URL(response.url());
      return (
        response.request().method() === "POST" &&
        url.pathname ===
          `/api/v1/sessions/${encodeURIComponent(initialSessionRef)}/turns`
      );
    });
    await page.getByRole("button", { name: "Отправить", exact: true }).click();
    const continuationResponse = await continuationResponsePromise;
    expect(
      continuationResponse.status(),
      await mutationFailureDiagnostic(continuationResponse, page),
    ).toBe(201);
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
    const instructionsPanel = page.locator("article.panel").filter({
      has: page.getByRole("heading", { name: "Инструкции", exact: true }),
    });
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
    await instructionsPanel.locator("textarea").fill(updatedInstructions);
    const saveResponse = page.waitForResponse(
      (response) =>
        response.request().method() === "POST" &&
        new URL(response.url()).pathname ===
          `/api/v1/agents/${coordinatorRef}/instruction-drafts`,
    );
    await instructionsPanel.getByRole("button", { name: "Сохранить" }).click();
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
    await expect(instructionsPanel.locator("textarea")).toHaveValue(
      originalInstructions,
    );
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
      await gotoWithRetry(
        page,
        `/assistant?projectRef=${encodeURIComponent(projectRef)}`,
      );
      await page
        .getByRole("button", { name: "Новый диалог", exact: true })
        .click();
      const prompt = [
        `В текущем Проекте создай одного ИИ-сотрудника с точным именем «${analystName}».`,
        "Назначение: анализировать качество лидов.",
        "Роль: аналитик продаж.",
        "Инструкции: оценивай факты, отмечай допущения и отвечай по-русски.",
        "Не запускай его и не меняй другие объекты.",
      ].join(" ");
      await page
        .getByLabel("Опишите, что нужно настроить или запустить")
        .fill(prompt);
      await page.getByRole("button", { name: "Отправить помощнику" }).click();
      const plan = page.locator(".assistant-plan").last();
      await expect(plan).toContainText(analystName, { timeout: 120_000 });
      const applyPlan = plan.getByRole("button", {
        name: "Применить разрешённые изменения",
      });
      await applyPlan.click();
      await expect(applyPlan).toHaveCount(0);

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

    await gotoWithRetry(page, "/assistant");
    await expect(
      page.locator("#main-content").getByText(/Системный.*неудаляемый/),
    ).toBeVisible();
    await expect(
      page.getByRole("button", { name: /Удалить|Архивировать|Отключить/ }),
    ).toHaveCount(0);
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

    const existing = page.locator(".file-row").filter({
      hasText: uploadedFileName,
    });
    if ((await existing.count()) === 0) {
      const uploadButton = page
        .locator('button[type="button"]')
        .filter({ hasText: "Загрузить файл" })
        .first();
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
      .locator(".file-row")
      .filter({
        hasText: uploadedFileName,
      })
      .first();
    await expect(artifact).toBeVisible();
    await artifact.click();
    await expect(
      page.getByRole("heading", { name: uploadedFileName }),
    ).toBeVisible();
    const preview = page.getByRole("button", { name: "Открыть", exact: true });
    if ((await preview.count()) > 0) await preview.click();
    await expect(page.locator(".files-preview pre")).toContainText(content);

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

    const downloadPromise = page.waitForEvent("download");
    await page.getByRole("button", { name: "Скачать", exact: true }).click();
    const download = await downloadPromise;
    expect(download.suggestedFilename()).toBe(uploadedFileName);
    const path = await download.path();
    if (!path) throw new Error("download did not produce a local file");
    expect(await readFile(path, "utf8")).toBe(content);
  });

  test("привязанный knowledge-файл доступен ИИ-сотруднику", async ({
    page,
  }) => {
    requireRefs("projectRef", "coordinatorRef");
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

  test("автоматизация создаётся, проходит pause/resume и запускается", async ({
    page,
  }) => {
    requireRefs("projectRef", "coordinatorRef");
    const schedulesResponse = page.waitForResponse(
      (response) =>
        response.request().method() === "GET" &&
        new URL(response.url()).pathname ===
          `/api/v1/projects/${projectRef}/schedules`,
    );
    await gotoWithRetry(page, `/projects/${projectRef}/automations`);
    expect((await schedulesResponse).status()).toBe(200);
    await expectPageHeading(page, "Автоматизации");

    let row = page.locator(".entity-row").filter({ hasText: automationName });
    if ((await row.count()) === 0) {
      await page
        .getByRole("button", { name: "Новая автоматизация" })
        .first()
        .click();
      const dialog = page.getByRole("dialog", { name: "Новая автоматизация" });
      await dialog.getByLabel("Название").fill(automationName);
      await dialog.getByLabel("Что запустить").selectOption("AGENT");
      await dialog.getByLabel("Цель").selectOption({ label: coordinatorName });
      const triggerAt = new Date(Date.now() + 75_000);
      const saratovHour = (triggerAt.getUTCHours() + 4) % 24;
      const timeOfDay = `${String(saratovHour).padStart(2, "0")}:${String(
        triggerAt.getUTCMinutes(),
      ).padStart(2, "0")}`;
      await dialog.getByLabel("Когда запускать").selectOption("DAILY");
      await dialog.getByLabel("Время запуска").fill(timeOfDay);
      await dialog.getByLabel("Часовой пояс").fill("Europe/Saratov");
      await dialog.getByLabel("Задание").fill(automationTask);
      await dialog
        .getByRole("button", { name: "Создать", exact: true })
        .click();
      await expect(dialog).toHaveCount(0);
      row = page.locator(".entity-row").filter({ hasText: automationName });
    }
    await expect(row).toHaveCount(1);
    if (
      (await row.locator(".status-badge").textContent()) === "Приостановлено"
    ) {
      const enableResponse = scheduleCommandResponse(page);
      await row.getByRole("button", { name: "Включить" }).click();
      expect(
        (await enableResponse).status(),
        await mutationFailureDiagnostic(await enableResponse, page),
      ).toBe(200);
    }
    await expect(row.locator(".status-badge")).toHaveText("Активен");

    const pauseResponse = scheduleCommandResponse(page);
    await row.getByRole("button", { name: "Приостановить" }).click();
    const paused = await pauseResponse;
    expect(paused.status(), await mutationFailureDiagnostic(paused, page)).toBe(
      200,
    );
    await expect(row.locator(".status-badge")).toHaveText("Приостановлено");
    const resumeResponse = scheduleCommandResponse(page);
    await row.getByRole("button", { name: "Включить" }).click();
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
  });

  test("сотрудник без capability Файлы не получает файл", async ({ page }) => {
    requireRefs("projectRef", "analystRef");
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
    const knowledgeArtifact = page.getByRole("button", {
      name: new RegExp(uploadedFileName),
    });
    await expect(knowledgeArtifact).toBeVisible();
    await knowledgeArtifact.click();
    const binding = page.getByRole("checkbox", {
      name: new RegExp(analystName),
    });
    await expect(binding).toBeDisabled();

    await gotoWithRetry(
      page,
      `/projects/${projectRef}/runs/new?targetType=AGENT&targetRef=${encodeURIComponent(analystRef)}`,
    );
    await expect(page.getByLabel("Задание")).toBeVisible();
    const fileChoices = page.getByRole("checkbox", {
      name: new RegExp(uploadedFileName),
    });
    await expect.poll(() => fileChoices.count()).toBeGreaterThan(0);
    expect(
      await fileChoices.evaluateAll((choices) =>
        choices.every((choice) => (choice as HTMLInputElement).disabled),
      ),
    ).toBe(true);
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
            artifactRefs: [artifactRef],
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
    browser,
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

    await gotoWithRetry(
      page,
      `/projects/${projectRef}/agents/${coordinatorRef}`,
    );
    const delegationCapability = page.getByRole("checkbox", {
      name: /Делегирование/,
    });
    if (!(await delegationCapability.isChecked())) {
      const capabilityResponse = page.waitForResponse(
        (response) =>
          response.request().method() === "POST" &&
          response.url().includes(`/api/v1/agents/${coordinatorRef}/commands`),
      );
      await delegationCapability.check();
      expect((await capabilityResponse).ok()).toBe(true);
      await expect(page.getByText("Сохраняем возможность…")).toHaveCount(0);
    }
    await expect(delegationCapability).toBeChecked();

    if (discoveryMode && workflowRef) {
      await gotoWithRetry(
        page,
        `/projects/${projectRef}/workflows/${workflowRef}`,
      );
    } else {
      await gotoWithRetry(
        page,
        `/assistant?projectRef=${encodeURIComponent(projectRef)}`,
      );
      await page
        .getByRole("button", { name: "Новый диалог", exact: true })
        .click();
      const prompt = [
        `В текущем Проекте создай ровно один Процесс с точным названием «${workflowName}».`,
        `Назначение: параллельно оценить лид и подготовить предложение с решением владельца. Координатор — существующий сотрудник «${coordinatorName}».`,
        `Добавь ровно два параллельных этапа в одной группе: «Оценка лида» выполняет существующий сотрудник «${analystName}» с заданием оценить лид и вернуть структурированный вывод; «Коммерческое предложение» выполняет существующий сотрудник «${writerName}» с заданием подготовить предложение на основе оценки.`,
        "Для второго этапа требуется решение человека с вариантами APPROVE, REJECT и REQUEST_CHANGES. Максимальная параллельность — 2, timeout Процесса — 7200 секунд.",
        "Не создавай и не меняй сотрудников, не запускай Процесс и не создавай другие объекты.",
      ].join(" ");
      await page
        .getByLabel("Опишите, что нужно настроить или запустить")
        .fill(prompt);
      await page.getByRole("button", { name: "Отправить помощнику" }).click();
      const workflowPlan = page.locator(".assistant-plan").last();
      await expect(workflowPlan).toContainText(workflowName, {
        timeout: 120_000,
      });
      const applyWorkflowPlan = workflowPlan.getByRole("button", {
        name: "Применить разрешённые изменения",
      });
      const applyWorkflowPlanResponse = page.waitForResponse(
        (response) =>
          response.request().method() === "POST" &&
          response.url().includes("/api/v1/assistant-plans/") &&
          response.url().endsWith("/application"),
      );
      await applyWorkflowPlan.click();
      expect((await applyWorkflowPlanResponse).status()).toBe(200);
      await expect(applyWorkflowPlan).toHaveCount(0);

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
          "Квалифицируй лид производственной компании и подготовь предложение. Перед итогом запроси решение владельца.",
        );
      await page
        .getByRole("button", { name: "Запустить", exact: true })
        .click();
      await expect
        .poll(() => routeRef(page, "runs"), { timeout: 30_000 })
        .not.toBe("new");
      workflowRunRef = routeRef(page, "runs");
      persistRefs();
      await expectRunState(page, /В очереди|Выполняется/);
    }
    await expectRunState(page, "Ждёт решения");
    await expect(page.locator(".canvas-node")).toHaveCount(6, {
      timeout: 300_000,
    });
    await expect
      .poll(() =>
        page
          .locator(".canvas-node strong[title]")
          .evaluateAll((labels) =>
            labels.map((label) => label.getAttribute("title") ?? ""),
          ),
      )
      .toEqual(expect.arrayContaining([analystName, writerName]));
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
          nodes: Array<{ ref: string }>;
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
        { message: `authoritative edges: ${authoritativeEdgeTypes.join(",")}` },
      )
      .toEqual(authoritativeEdgeTypes);
    await expect(
      page
        .locator(".timeline")
        .getByText("Результат дочернего ИИ-сотрудника доставлен", {
          exact: true,
        }),
    ).toHaveCount(2);

    const contexts: BrowserContext[] = [];
    try {
      const freshStorageState = await page.context().storageState();
      const gate = await openGateForRun(page, workflowRunRef);
      const winner = await authenticatedRunPage(
        browser,
        workflowRunRef,
        browserDiagnostics.monitorPage,
        freshStorageState,
      );
      const contender = await authenticatedRunPage(
        browser,
        workflowRunRef,
        browserDiagnostics.monitorPage,
        freshStorageState,
      );
      contexts.push(winner.context, contender.context);
      await browserDiagnostics.withExpectedNetworkInterruption(
        page,
        async () => {
          await page.context().setOffline(true);
          await expect(
            page.getByText(
              "Нет сети. Показываем последнее полученное состояние; действия временно недоступны.",
              { exact: true },
            ),
          ).toBeVisible();

          const resolutions = await Promise.all([
            resolveGateAtVersion(winner.page, gate.ref, gate.version),
            resolveGateAtVersion(contender.page, gate.ref, gate.version),
          ]);
          expect(
            resolutions.map((resolution) => resolution.status).sort(),
          ).toEqual([200, 409]);
          expect(
            resolutions.find((resolution) => resolution.status === 409)?.code,
          ).not.toBe("");

          await page.context().setOffline(false);
          await expect(
            page.getByText(
              "Нет сети. Показываем последнее полученное состояние; действия временно недоступны.",
              { exact: true },
            ),
          ).toHaveCount(0);
        },
      );
      await expectRunState(page, /Выполняется|Завершён/);
      await waitForTerminalSuccess(page);
      await assertNoDuplicateGraphNodes(page);
      await expect(page.locator(".timeline")).toContainText(/решение/i);
    } finally {
      await page.context().setOffline(false);
      await Promise.all(contexts.map((context) => context.close()));
    }
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
      page.locator(".canvas-node").filter({ hasText: "Отменён" }).first(),
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
    ).toHaveAttribute("href", `/runs/${cancelledRef}`);
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

  test("административные экраны и security boundary дают ожидаемый readback", async ({
    browser,
    page,
  }) => {
    requireRefs("projectRef");
    await gotoWithRetry(page, "/administration");
    await expectPageHeading(page, "Администрирование");
    await expect(page.locator("#main-content")).toContainText("Core-платформа");

    await gotoWithRetry(page, "/administration/access");
    await expectPageHeading(page, "Участники организации");
    await expect(page.locator(".entity-row")).not.toHaveCount(0);
    await gotoWithRetry(page, `/projects/${projectRef}/members`);
    await expectPageHeading(page, "Участники и доступ");
    await expect(page.locator(".entity-row")).not.toHaveCount(0);

    await gotoWithRetry(
      page,
      `/administration/audit?projectRef=${encodeURIComponent(projectRef)}`,
    );
    await expectPageHeading(page, "Аудит и диагностика");
    await expect(page.getByRole("table")).toContainText(automationName);
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

    const currentUserMenu = page.locator("details.current-user-menu");
    const currentUserMenuButton = currentUserMenu.locator(":scope > summary");
    await expect(currentUserMenuButton).toBeVisible();
    await currentUserMenuButton.click();
    await expect(currentUserMenu).toHaveAttribute("open", "");
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
    writerRef,
    workflowRef,
    firstRunRef,
    continuationRunRef,
    instructionRunRef,
    publishedInstructionRef,
    scheduledRunRef,
    workflowRunRef,
  };
}

function requireRefs(...required: ReadonlyArray<keyof DiscoveryRefs>): void {
  if (!discoveryMode) return;
  const persisted = loadDiscoveryRefs(environment.resourcePrefix);
  projectRef = persisted.projectRef ?? projectRef;
  coordinatorRef = persisted.coordinatorRef ?? coordinatorRef;
  analystRef = persisted.analystRef ?? analystRef;
  writerRef = persisted.writerRef ?? writerRef;
  workflowRef = persisted.workflowRef ?? workflowRef;
  firstRunRef = persisted.firstRunRef ?? firstRunRef;
  continuationRunRef = persisted.continuationRunRef ?? continuationRunRef;
  instructionRunRef = persisted.instructionRunRef ?? instructionRunRef;
  publishedInstructionRef =
    persisted.publishedInstructionRef ?? publishedInstructionRef;
  scheduledRunRef = persisted.scheduledRunRef ?? scheduledRunRef;
  workflowRunRef = persisted.workflowRunRef ?? workflowRunRef;
  const refs = currentRefs();
  const missing = required.filter((key) => !refs[key]);
  test.skip(
    missing.length > 0,
    `BLOCKED: отсутствуют prerequisite refs: ${missing.join(", ")}`,
  );
}

async function authenticatedRunPage(
  browser: Browser,
  runRef: string,
  monitorPage: (page: Page) => void,
  storageState: Awaited<ReturnType<BrowserContext["storageState"]>>,
): Promise<{ context: BrowserContext; page: Page }> {
  const context = await browser.newContext({
    baseURL: environment.baseURL,
    storageState,
    locale: "ru-RU",
  });
  const page = await context.newPage();
  monitorPage(page);
  await gotoWithRetry(page, `/runs/${runRef}`);
  await expect(
    page.getByRole("button", { name: "Одобрить", exact: true }),
  ).toBeVisible();
  return { context, page };
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
          (
            await page
              .locator("article.panel")
              .filter({ hasText: "Профиль сотрудника" })
              .getByText(/Версия \d+/)
              .textContent()
          )?.match(/\d+/)?.[0] ?? "0",
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
