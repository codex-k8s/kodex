import { expect, type Locator, type Page } from "@playwright/test";
import { FixtureUnavailable, type FixturePin } from "./ui-fixture-manifest";
import { matchesAssistantSearch } from "./ui-readonly-search-proof";
import {
  assistantDialog,
  cleanupAssistant,
  geometry,
  visit,
  focused,
  observeActionResponse,
  checkCondition,
  observeCondition,
} from "./ui-acceptance-browser";

export class ReadonlyFixtureMissing extends Error {
  constructor(
    readonly stage:
      | "PROJECTS_EMPTY"
      | "PROJECT_DIALOG_EMPTY"
      | "ASSISTANT_HISTORY_EMPTY"
      | "ASSISTANT_COMPOSER_UNAVAILABLE",
  ) {
    super("Readonly fixture unavailable");
  }
}
export async function searchAssistantHistory(
  page: Page,
  search: Locator,
  entries: Locator,
  query: string,
) {
  const response = await observeActionResponse(
    page,
    (value) =>
      matchesAssistantSearch(value.request().method(), value.url(), query),
    () => search.fill(query),
  );
  checkCondition("HTTP_STATUS", response.status(), 200);
  await expect(entries).toHaveCount(0);
}
export async function projectForm(
  page: Page,
  locale: "ru" | "en",
  prefix: string,
) {
  await visit(page, "/projects");
  await page
    .getByRole("button", {
      name: locale === "ru" ? "Новый Проект" : "New Project",
      exact: true,
    })
    .click();
  const form = page.locator("#project-form");
  try {
    const name = form.locator("input[required]");
    const purpose = form.locator("textarea");
    await focused("SELECTOR_FOCUS", name);
    await name.fill(`${prefix}-unsaved-form`);
    await name.press("Tab");
    await expect(purpose).toBeFocused();
    await purpose.fill("Synthetic unsaved purpose");
    await expect(name).toHaveValue(`${prefix}-unsaved-form`);
    const valid = await form.evaluate((element) =>
      (element as HTMLFormElement).checkValidity(),
    );
    checkCondition("UI_ACTION", valid, true);
    await name.fill("");
    const invalid = await name.evaluate(
      (element) => !(element as HTMLInputElement).checkValidity(),
    );
    checkCondition("UI_ACTION", invalid, true);
    const result = await geometry(page);
    return { ...result, requiredValidation: true, focusPreserved: true };
  } finally {
    await page
      .getByRole("button", {
        name: locale === "ru" ? "Отмена" : "Cancel",
        exact: true,
      })
      .click();
    await expect(form).toHaveCount(0);
  }
}
// Нулевой count не является пустым terminal state: загрузка/перерисовка
// наблюдаются без повторного GET, клика, create или извлечения текста данных.
export async function waitForProjectCollection(
  root: Locator,
  locale: "ru" | "en",
  stage: "PROJECTS_EMPTY" | "PROJECT_DIALOG_EMPTY",
  timeoutMs = 15_000,
): Promise<number> {
  if (!Number.isFinite(timeoutMs) || timeoutMs < 100 || timeoutMs > 15_000)
    throw new Error("Invalid project collection observation budget");
  let snapshot = { state: "PENDING", rows: 0 };
  await observeCondition("UI_ACTION", () =>
    expect
      .poll(
        async () => {
          snapshot = await root.evaluateAll(
            (roots, emptyLabel) => {
              if (roots.length !== 1) return { state: "PENDING", rows: 0 };
              const element = roots[0];
              if (!element) return { state: "PENDING", rows: 0 };
              const visible = (node: Element) => {
                const rect = node.getBoundingClientRect();
                return (
                  rect.width > 0 &&
                  rect.height > 0 &&
                  getComputedStyle(node).visibility !== "hidden"
                );
              };
              if (!visible(element)) return { state: "PENDING", rows: 0 };
              const has = (selector: string) =>
                Array.from(element.querySelectorAll(selector)).some(visible);
              if (has('[role="alert"]')) return { state: "ERROR", rows: 0 };
              if (has('[role="status"]')) return { state: "PENDING", rows: 0 };
              const rows = Array.from(
                element.querySelectorAll(".project-list__item"),
              ).filter(visible).length;
              if (rows) return { state: "POPULATED", rows };
              const empty = Array.from(element.querySelectorAll("p")).some(
                (node) =>
                  visible(node) && node.textContent.trim() === emptyLabel,
              );
              return { state: empty ? "EMPTY" : "PENDING", rows: 0 };
            },
            locale === "ru"
              ? "Создайте первый Проект"
              : "Create your first Project",
          );
          return snapshot.state !== "PENDING";
        },
        { timeout: timeoutMs, intervals: [50, 100, 250] },
      )
      .toBe(true),
  );
  checkCondition("VISIBLE_ALERT", snapshot.state === "ERROR", false);
  if (snapshot.state === "EMPTY") throw new ReadonlyFixtureMissing(stage);
  return snapshot.rows;
}

export async function projectCollection(page: Page, locale: "ru" | "en") {
  const response = await observeActionResponse(
    page,
    (value) => {
      const url = new URL(value.url());
      return (
        value.request().method() === "GET" &&
        url.pathname === "/api/v1/projects" &&
        url.searchParams.get("pageSize") === "30" &&
        !url.searchParams.get("query") &&
        !url.searchParams.get("pageToken")
      );
    },
    () => visit(page, "/projects"),
  );
  checkCondition("HTTP_STATUS", response.status(), 200);
  const count = await waitForProjectCollection(
    page.locator(".page-frame"),
    locale,
    "PROJECTS_EMPTY",
  );
  checkCondition("UI_ACTION", count, 6, count <= 6);
  await page.locator(".projects-toolbar .icon-button").click();
  const dialog = page.getByRole("dialog", {
    name: locale === "ru" ? "Проекты" : "Projects",
    exact: true,
  });
  try {
    await expect(dialog).toBeVisible();
    const populated = await waitForProjectCollection(
      dialog,
      locale,
      "PROJECT_DIALOG_EMPTY",
    );
    return {
      collapsedRows: count,
      expandedRows: populated,
      ...(await geometry(page)),
    };
  } finally {
    await dialog
      .getByRole("button", {
        name: locale === "ru" ? "Закрыть" : "Close",
        exact: true,
      })
      .click();
    await expect(dialog).toHaveCount(0);
  }
}
export async function configurationCreate(
  page: Page,
  kind: string,
  locale: "ru" | "en",
) {
  await visit(page, `/configurations/${kind}`);
  await page
    .locator(".configuration-catalog")
    .getByRole("link", {
      name: locale === "ru" ? "Создать" : "Create",
      exact: true,
    })
    .click();
  const editor = page.locator(".configuration-editor");
  await expect(editor).toBeVisible();
  const fields = editor.locator("input,textarea,.cm-content");
  const count = await fields.count();
  checkCondition("UI_ACTION", count, 0, count > 0);
  return { visibleEditor: true, fieldCount: count, ...(await geometry(page)) };
}
export async function assistantDraft(
  page: Page,
  locale: "ru" | "en",
  prefix: string,
) {
  await visit(page, "/");
  await page
    .getByRole("button", {
      name: locale === "ru" ? "Открыть Kodex" : "Open Kodex",
      exact: true,
    })
    .click();
  const dialog = assistantDialog(page, locale);
  const composer = dialog.locator(".assistant-composer textarea");
  try {
    await expect(dialog).toBeVisible();
    await focused("ASSISTANT_FOCUS", dialog);
    await expect(dialog).toHaveAttribute("aria-busy", "false");
    await expect(dialog.getByRole("alert")).toHaveCount(0);
    const mobile = (page.viewportSize()?.width ?? 1440) < 1001;
    const historyToggle = dialog.getByRole("button", {
      name: locale === "ru" ? "История диалогов" : "Conversation history",
      exact: true,
    });
    if (mobile) await historyToggle.click();
    const history = page.locator(
      mobile ? ".assistant-history__menu" : ".assistant-conversation-sidebar",
    );
    const entries = history.locator(
      mobile ? ":scope > button" : ".assistant-conversation-entry",
    );
    const historyItems = await entries.count();
    if (!historyItems)
      throw new ReadonlyFixtureMissing("ASSISTANT_HISTORY_EMPTY");
    await entries.first().click();
    await expect(dialog).toHaveAttribute("aria-busy", "false");
    if (!(await composer.count()) || !(await composer.isEnabled()))
      throw new ReadonlyFixtureMissing("ASSISTANT_COMPOSER_UNAVAILABLE");
    await composer.fill(`${prefix}-unsaved`);
    let cancelled = false;
    const dismiss = async (prompt: import("@playwright/test").Dialog) => {
      cancelled = true;
      await prompt.dismiss();
    };
    page.once("dialog", dismiss);
    try {
      await composer.press("Escape");
    } finally {
      page.off("dialog", dismiss);
    }
    await expect(dialog).toBeVisible();
    await expect(composer).toHaveValue(`${prefix}-unsaved`);
    checkCondition("UI_ACTION", cancelled, true);
    await composer.fill("");
    if (mobile) await historyToggle.click();
    const search = history.getByRole("searchbox");
    await expect(search).toBeVisible();
    await searchAssistantHistory(page, search, entries, `${prefix}-absent`);
    return {
      unsavedCloseCancelled: true,
      populatedHistory: true,
      historyItems,
      searchReadback: true,
    };
  } finally {
    if (await composer.count()) await composer.fill("").catch(() => undefined);
    await cleanupAssistant(page, locale);
  }
}

// История читается независимо от доступности composer и runtime provider.
export async function assistantHistory(
  page: Page,
  locale: "ru" | "en",
  prefix: string,
  pin: FixturePin,
  pagination = false,
) {
  if (pin.kind !== "ASSISTANT" || !pin.query)
    throw new FixtureUnavailable("MISSING");
  await visit(page, pin.projectRef ? `/projects/${pin.projectRef}` : "/");
  await page
    .getByRole("button", {
      name: locale === "ru" ? "Открыть Kodex" : "Open Kodex",
      exact: true,
    })
    .click();
  const dialog = assistantDialog(page, locale),
    mobile = (page.viewportSize()?.width ?? 1440) < 1001;
  const toggle = dialog.getByRole("button", {
    name: locale === "ru" ? "История диалогов" : "Conversation history",
    exact: true,
  });
  try {
    await expect(dialog).toBeVisible();
    await expect(dialog).toHaveAttribute("aria-busy", "false");
    if (mobile) await toggle.click();
    const history = page.locator(
      mobile ? ".assistant-history__menu" : ".assistant-conversation-sidebar",
    );
    const search = history.getByRole("searchbox");
    await expect(search).toBeVisible();
    const response = await observeActionResponse(
      page,
      (value) =>
        matchesAssistantSearch(
          value.request().method(),
          value.url(),
          pin.query ?? "",
        ),
      () => search.fill(pin.query ?? ""),
    );
    checkCondition("HTTP_STATUS", response.status(), 200);
    const data: unknown = await response.json();
    if (
      !data ||
      typeof data !== "object" ||
      !Array.isArray((data as { items?: unknown }).items)
    )
      throw new Error("Invalid history response");
    const items = (
      data as { items: Array<{ ref: string; version: number; title: string }> }
    ).items;
    const selected = items.filter(
      (item) => item.ref === pin.ref && item.version === pin.version,
    );
    if (selected.length !== 1 || typeof selected[0]?.title !== "string")
      throw new FixtureUnavailable("VERSION_DRIFT");
    const title = selected[0].title;
    await history.getByText(title, { exact: true }).click();
    await expect(dialog).toHaveAttribute("aria-busy", "false");
    if (mobile) await toggle.click();
    const entries = history.locator(
      mobile ? ":scope > button" : ".assistant-conversation-entry",
    );
    await searchAssistantHistory(page, search, entries, `${prefix}-absent`);
    const historyPages: import("@playwright/test").Response[] = [];
    const pageListener = (value: import("@playwright/test").Response) => {
      const url = new URL(value.url());
      if (
        value.request().method() === "GET" &&
        url.pathname === "/api/v1/assistant-conversations" &&
        url.searchParams.get("pageToken")
      )
        historyPages.push(value);
    };
    if (pagination) page.on("response", pageListener);
    try {
      const clear = await observeActionResponse(
        page,
        (value) =>
          matchesAssistantSearch(value.request().method(), value.url(), ""),
        () => search.fill(""),
      );
      checkCondition("HTTP_STATUS", clear.status(), 200);
      await expect(history.getByText(title, { exact: true })).toBeVisible();
      if (pagination) {
        const raw: unknown = await clear.json();
        const cursor =
          raw && typeof raw === "object"
            ? (raw as { nextPageToken?: unknown }).nextPageToken
            : undefined;
        if (typeof cursor !== "string" || !cursor)
          throw new FixtureUnavailable("EMPTY_PAGE");
        const matchesPage = (value: import("@playwright/test").Response) => {
          const url = new URL(value.url());
          return (
            value.request().method() === "GET" &&
            url.pathname === "/api/v1/assistant-conversations" &&
            url.searchParams.get("pageToken") === cursor &&
            (url.searchParams.get("query") ?? "") === "" &&
            (url.searchParams.get("projectRef") ?? "") ===
              (pin.projectRef ?? "")
          );
        };
        const next =
          historyPages.find(matchesPage) ??
          (await observeActionResponse(page, matchesPage, () =>
            history.evaluate((node) => {
              node.scrollTop = node.scrollHeight;
            }),
          ));
        checkCondition("HTTP_STATUS", next.status(), 200);
        const nextBody = (await next.json()) as {
          items: Array<{ ref: string; title: string }>;
        };
        const firstBody = raw as { items: Array<{ ref: string }> };
        if (
          !Array.isArray(nextBody.items) ||
          !nextBody.items.length ||
          nextBody.items.some((item) =>
            firstBody.items.some((previous) => previous.ref === item.ref),
          )
        )
          throw new Error("Invalid history page continuity");
        for (const item of nextBody.items)
          await expect(
            history.getByText(item.title, { exact: true }),
          ).toHaveCount(1);
      }
      return {
        historyExactPin: true,
        searchReadback: true,
        clearReadback: true,
        openedWithoutComposer: true,
        pagination,
      };
    } finally {
      page.off("response", pageListener);
    }
  } finally {
    if (!page.isClosed()) await cleanupAssistant(page, locale);
  }
}
