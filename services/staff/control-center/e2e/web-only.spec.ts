import {
  type Browser,
  type BrowserContext,
  type Locator,
  type Page,
} from "@playwright/test";

import { loadE2EEnvironment } from "./environment";
import { expect, test } from "./fixtures";
import {
  assertNoDuplicateGraphNodes,
  createAgent,
  expectPageHeading,
  expectRunState,
  launchAgent,
  publishAndPrepareAgent,
  routeRef,
  waitForTerminalSuccess,
} from "./helpers";

const environment = loadE2EEnvironment();
const projectName = `${environment.resourcePrefix} — отдел продаж`;
const coordinatorName = `${environment.resourcePrefix} — координатор продаж`;
const analystName = `${environment.resourcePrefix} — аналитик лидов`;
const writerName = `${environment.resourcePrefix} — автор предложений`;
const workflowName = `${environment.resourcePrefix} — квалификация лида`;

let projectRef = "";
let coordinatorRef = "";
let analystRef = "";
let writerRef = "";
let workflowRef = "";
let firstRunRef = "";
let workflowRunRef = "";

test.describe.serial("web-only fresh installation", () => {
  test("первый вход, горячий помощник и первый Проект", async ({ page }) => {
    await page.goto("/onboarding");
    await expectPageHeading(page, "Настроим Kodex");
    await expect(
      page.getByRole("heading", { name: "Системный помощник готов" }),
    ).toBeVisible();
    await expect(
      page.getByText("Внешние интеграции не нужны для начала работы"),
    ).toBeVisible();
    await expect(page.getByText("Готов", { exact: true })).toBeVisible();

    await page.getByRole("link", { name: "Начать с помощником" }).click();
    await expectPageHeading(page, "Помощник Kodex");
    await expect(page.getByText(/Системный.*неудаляемый/)).toBeVisible();

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
    await plan
      .getByRole("button", { name: "Применить разрешённые изменения" })
      .click();

    await page.goto("/projects");
    const projectLink = page.getByRole("link", {
      name: new RegExp(projectName),
    });
    await expect(projectLink).toBeVisible();
    await projectLink.click();
    projectRef = routeRef(page, "projects");

    await page.goto("/onboarding");
    await page.getByRole("button", { name: "Завершить настройку" }).click();
    await expect(page).toHaveURL(/\/projects$/);
  });

  test("ИИ-сотрудник выполняет первый запуск и отдаёт файл", async ({
    page,
  }) => {
    coordinatorRef = await createAgent(page, projectRef, {
      name: coordinatorName,
      purpose: "Координировать квалификацию лидов и собирать итоговый ответ.",
      role: "Руководитель процесса продаж, который распределяет работу между специалистами.",
      instructions:
        "Работай только с данными задания. Отвечай по-русски. При запросе результата создавай безопасный текстовый файл.",
    });
    await publishAndPrepareAgent(page);

    firstRunRef = await launchAgent(
      page,
      "Подготовь краткий план квалификации нового B2B-лида и создай файл lead-plan.txt с итогом.",
    );
    await expectRunState(page, /В очереди|Выполняется/);
    await waitForTerminalSuccess(page);
    await expect(
      page.getByRole("heading", { name: "Результаты и файлы" }),
    ).toBeVisible();

    const artifact = page.locator(".artifact-row").first();
    await expect(artifact).toBeVisible();
    const downloadPromise = page.waitForEvent("download");
    await artifact.click();
    const download = await downloadPromise;
    expect(download.suggestedFilename()).toBeTruthy();

    const continuation = page.getByRole("heading", {
      name: "Продолжить сессию",
    });
    await expect(continuation).toBeVisible();
    await page
      .getByLabel("Дополнительное задание")
      .fill(
        "Добавь два вопроса для следующего контакта и сохрани контекст сессии.",
      );
    await page.getByRole("button", { name: "Отправить", exact: true }).click();
    await expect(page).toHaveURL(/\/runs\/[^/]+$/);
    await waitForTerminalSuccess(page);
  });

  test("помощник создаёт сотрудника типизированным действием и оставляет аудит", async ({
    page,
  }) => {
    await page.goto(`/assistant?projectRef=${encodeURIComponent(projectRef)}`);
    await page.getByRole("button", { name: "Новый диалог" }).click();
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
    await plan
      .getByRole("button", { name: "Применить разрешённые изменения" })
      .click();

    await page.goto(`/projects/${projectRef}/agents`);
    const analystLink = page.getByRole("link", {
      name: new RegExp(analystName),
    });
    await expect(analystLink).toBeVisible();
    await analystLink.click();
    analystRef = routeRef(page, "agents");
    await publishAndPrepareAgent(page);

    await page.goto(
      `/administration/audit?projectRef=${encodeURIComponent(projectRef)}`,
    );
    await expect(page.getByRole("table")).toContainText(
      "system_assistant.create_agent",
    );
    await expect(page.getByRole("table")).toContainText(analystName);

    await page.goto("/assistant");
    await expect(page.getByText(/Системный.*неудаляемый/)).toBeVisible();
    await expect(
      page.getByRole("button", { name: /Удалить|Архивировать|Отключить/ }),
    ).toHaveCount(0);
  });

  test("вложенный Процесс показывает live graph и Human Gate переживает reconnect", async ({
    page,
    browser,
    browserDiagnostics,
  }) => {
    writerRef = await createAgent(page, projectRef, {
      name: writerName,
      purpose: "Готовить персонализированные коммерческие предложения.",
      role: "Автор B2B-предложений.",
      instructions:
        "Используй вывод аналитика, не выдумывай факты и отвечай по-русски.",
    });
    await publishAndPrepareAgent(page);

    await page.goto(`/projects/${projectRef}/agents/${coordinatorRef}`);
    const delegationCapability = page.getByRole("checkbox", {
      name: /Делегирование/,
    });
    await delegationCapability.check();
    await expect(delegationCapability).toBeChecked();

    await page.goto(`/projects/${projectRef}/workflows`);
    await page.getByRole("button", { name: "Новый Процесс" }).first().click();
    const dialog = page.getByRole("dialog", { name: "Новый Процесс" });
    await dialog.getByLabel("Название").fill(workflowName);
    await dialog
      .getByLabel("Назначение")
      .fill(
        "Параллельно оценить лид и подготовить предложение с решением владельца.",
      );
    await dialog
      .getByLabel("Координатор")
      .selectOption({ label: coordinatorName });
    await dialog.getByRole("button", { name: "Создать", exact: true }).click();
    workflowRef = routeRef(page, "workflows");

    await page.getByRole("button", { name: "Создать", exact: true }).click();
    await page.getByRole("button", { name: "Создать", exact: true }).click();
    const steps = page.locator(".workflow-step");
    await expect(steps).toHaveCount(2);
    await fillWorkflowStep(
      steps.nth(0),
      "Оценка лида",
      analystName,
      "Оцени лид и верни структурированный вывод.",
      false,
    );
    await fillWorkflowStep(
      steps.nth(1),
      "Коммерческое предложение",
      writerName,
      "Подготовь предложение на основе оценки.",
      true,
    );
    await page.getByRole("button", { name: "Сохранить", exact: true }).click();
    await page.getByRole("button", { name: "Проверить Процесс" }).click();
    await page.getByRole("button", { name: "Опубликовать Процесс" }).click();

    await page
      .getByLabel("Задание")
      .fill(
        "Квалифицируй лид производственной компании и подготовь предложение. Перед итогом запроси решение владельца.",
      );
    await page.getByRole("button", { name: "Запустить", exact: true }).click();
    workflowRunRef = routeRef(page, "runs");
    await expectRunState(page, /В очереди|Выполняется/);
    await expectRunState(page, "Ждёт решения");
    await expect(page.locator(".canvas-node")).toHaveCount(6, {
      timeout: 300_000,
    });
    await expect(
      page.locator(".canvas-node").filter({ hasText: analystName }),
    ).toHaveCount(1);
    await expect(
      page.locator(".canvas-node").filter({ hasText: writerName }),
    ).toHaveCount(1);
    await expect(page.locator('[data-edge-type="CALLBACK_TO"]')).toHaveCount(2);
    await expect(page.locator('[data-edge-type="CONTINUES"]')).toHaveCount(1);
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
          await expect(page.getByText("Вы не в сети.")).toBeVisible();

          await Promise.allSettled([
            winner.page
              .getByRole("button", { name: "Одобрить", exact: true })
              .click(),
            contender.page
              .getByRole("button", { name: "Одобрить", exact: true })
              .click(),
          ]);
          await expect
            .poll(async () => {
              const message =
                "Данные изменились. Показано актуальное состояние.";
              return (
                Number(await winner.page.getByText(message).isVisible()) +
                Number(await contender.page.getByText(message).isVisible())
              );
            })
            .toBe(1);

          await page.context().setOffline(false);
          await expect(page.getByText("Вы не в сети.")).toHaveCount(0);
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
    await page.goto(`/projects/${projectRef}/agents/${coordinatorRef}`);
    const cancelledRef = await launchAgent(
      page,
      "Начни подробный анализ длинного списка лидов и не завершай работу до проверки всех записей.",
    );
    await page.getByRole("button", { name: "Отменить запуск" }).click();
    await expectRunState(page, "Отменён");
    await expect(
      page.locator(".canvas-node").filter({ hasText: "Отменён" }).first(),
    ).toBeVisible();

    await page.getByRole("button", { name: "Повторить попытку" }).click();
    const retriedRef = routeRef(page, "runs");
    expect(retriedRef).not.toBe(cancelledRef);
    await expect(page.getByText("Попытка 2", { exact: true })).toBeVisible();
    await expect(
      page.getByRole("link", { name: "Открыть предыдущую попытку" }),
    ).toHaveAttribute("href", `/runs/${cancelledRef}`);
  });

  test("core остаётся готовым без подключённых интеграций", async ({
    page,
  }) => {
    await page.goto("/integrations");
    await expectPageHeading(page, "Интеграции");
    await expect(
      page.getByText("Платформа работает без интеграций"),
    ).toBeVisible();
    await expect(page.getByText(/Подключения необязательны/)).toBeVisible();
    await expect(page.getByText(/Mattermost обязателен/i)).toHaveCount(0);

    await page.goto(`/runs/${firstRunRef}`);
    await expectRunState(page, "Завершён");
    await page.goto(`/runs/${workflowRunRef}`);
    await expectRunState(page, "Завершён");
    expect(analystRef).toBeTruthy();
    expect(writerRef).toBeTruthy();
    expect(workflowRef).toBeTruthy();
  });
});

async function fillWorkflowStep(
  step: Locator,
  name: string,
  agentName: string,
  purpose: string,
  humanGate: boolean,
): Promise<void> {
  await step.getByLabel("Название этапа").fill(name);
  await step.getByLabel("Исполнитель").selectOption({ label: agentName });
  await step.getByLabel("Назначение").fill(purpose);
  await step.getByLabel("Можно выполнять параллельно").check();
  if (humanGate) await step.getByLabel("Требуется решение человека").check();
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
  await page.goto(`/runs/${runRef}`);
  await expect(
    page.getByRole("button", { name: "Одобрить", exact: true }),
  ).toBeVisible();
  return { context, page };
}
