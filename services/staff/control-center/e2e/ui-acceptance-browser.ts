import {
  expect,
  type Page,
  type Response,
  type Locator,
} from "@playwright/test";

import { UIConditionError, type Condition } from "./ui-acceptance-proof";

export function checkCondition(
  condition: Condition,
  actual: number | boolean,
  expected: number | boolean,
  pass = actual === expected,
): void {
  if (!pass) throw new UIConditionError(condition, actual, expected);
}
// Сохраняем только явно измеренное число/boolean. Сырой locator/error не переносится.
export async function observeCondition(
  condition: Condition,
  action: () => Promise<unknown>,
  measure?: () => Promise<number | boolean>,
  expected?: number | boolean,
) {
  try {
    await action();
  } catch {
    const actual = measure ? await measure().catch(() => undefined) : undefined;
    throw new UIConditionError(condition, actual, expected);
  }
}
export async function focused(condition: Condition, locator: Locator) {
  await observeCondition(
    condition,
    () => expect(locator).toBeFocused(),
    () => locator.evaluate((element) => element === document.activeElement),
    true,
  );
}
export function checkGeometry(metrics: Awaited<ReturnType<typeof geometry>>) {
  checkCondition(
    "DOCUMENT_OVERFLOW",
    metrics.overflow,
    1,
    metrics.overflow <= 1,
  );
  checkCondition(
    "HEADER_HEIGHT",
    metrics.headerHeight,
    0,
    metrics.headerHeight > 0,
  );
  checkCondition("UNTRANSLATED", metrics.untranslated, false);
  checkCondition("VISIBLE_ALERT", metrics.alerts, 0);
}
export async function geometry(page: Page) {
  return page.evaluate(() => {
    const header = document.querySelector(".topbar")?.getBoundingClientRect();
    return {
      overflow: Math.max(
        0,
        document.documentElement.scrollWidth -
          document.documentElement.clientWidth,
      ),
      headerHeight: header?.height ?? 0,
      headings: document.querySelectorAll(".page-header h1").length,
      untranslated: document.body.innerText.includes("i18n:"),
      alerts: Array.from(document.querySelectorAll('[role="alert"]')).filter(
        (item) => (item as HTMLElement).offsetHeight > 0,
      ).length,
    };
  });
}
export async function visit(page: Page, path: string) {
  const response = await page.goto(path, { waitUntil: "domcontentloaded" });
  if (!response) throw new UIConditionError("HTTP_STATUS", undefined, 200);
  checkCondition("HTTP_STATUS", response.status(), 200);
  checkCondition("ROUTE_MISMATCH", new URL(page.url()).pathname === path, true);
  const shell = page.locator(".app-shell");
  const heading = page.locator(".page-header h1").first();
  await observeCondition(
    "APP_SHELL",
    () => expect(shell).toBeVisible(),
    () => shell.isVisible(),
    true,
  );
  await observeCondition(
    "PAGE_HEADING",
    () => expect(heading).toBeVisible(),
    () => heading.isVisible(),
    true,
  );
  // Ограниченная стабилизация текущего экрана; не ждём networkidle при WS/polling.
  await page.waitForTimeout(750);
  const metrics = await geometry(page);
  checkGeometry(metrics);
  return metrics;
}

// Оба обещания получают обработчик сразу: ошибка click не оставляет поздний
// необработанный timeout наблюдателя ответа. Повторного действия здесь нет.
export async function observeActionResponse(
  page: Page,
  predicate: (response: Response) => boolean,
  action: () => Promise<unknown>,
  timeout = 15_000,
): Promise<Response> {
  const [response] = await Promise.all([
    page.waitForResponse(predicate, { timeout }),
    action(),
  ]);
  return response;
}

export function assistantDialog(page: Page, locale: "ru" | "en") {
  return page.getByRole("dialog", {
    name: locale === "ru" ? "Kodex" : "Kodex Assistant",
    exact: true,
  });
}
// Cleanup не зависит от проверяемого имени: неверный title тоже должен
// сохранять FAIL, но не оставлять окно для следующего независимого варианта.
export async function cleanupAssistant(page: Page, locale: "ru" | "en") {
  const component = page.locator("#assistant-workspace");
  if (!(await component.isVisible())) return;
  await component
    .getByRole("button", {
      name: locale === "ru" ? "Закрыть" : "Close",
      exact: true,
    })
    .click();
  await expect(component).toHaveCount(0);
}
export async function safeFocusMetrics(page: Page) {
  return page.evaluate(() => {
    const active = document.activeElement;
    return {
      focusOnPickerInput: !!active?.matches(
        '.async-picker__popover input[role="combobox"]',
      ),
      focusOnPickerTrigger: !!active?.matches(".async-picker__trigger"),
      focusOnPopoverPanel: !!active?.matches(".dismissible-popover"),
      focusOnAssistant: active?.id === "assistant-workspace",
      focusOnBody: active === document.body,
    };
  });
}

export async function safeAlertMetrics(page: Page) {
  return page.evaluate(() => {
    const alerts = Array.from(
      document.querySelectorAll('[role="alert"]'),
    ).filter((item) => (item as HTMLElement).offsetHeight > 0);
    // Сравнение только с canonical errors.default ru/en, сам текст не возвращается.
    const known = [
      "Не удалось выполнить действие. Повторите попытку.",
      "The action could not be completed. Try again.",
    ];
    return {
      alertProblemNotice: alerts.some((item) =>
        item.classList.contains("problem-notice"),
      ),
      alertDefaultError: alerts.some((item) =>
        known.includes(item.querySelector("p")?.textContent.trim() ?? ""),
      ),
      alertUnclassified: alerts.some(
        (item) =>
          !known.includes(item.querySelector("p")?.textContent.trim() ?? ""),
      ),
    };
  });
}
