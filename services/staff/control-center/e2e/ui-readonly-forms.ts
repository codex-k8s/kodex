import { expect, type Page } from "@playwright/test";
import {
  assistantDialog,
  cleanupAssistant,
  geometry,
  visit,
  focused,
  observeActionResponse,
  checkCondition,
} from "./ui-acceptance-browser";

export class ReadonlyFixtureMissing extends Error {}
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
export async function projectCollection(page: Page, locale: "ru" | "en") {
  await visit(page, "/projects");
  const rows = page.locator(".project-list__item");
  const count = await rows.count();
  if (!count) throw new ReadonlyFixtureMissing();
  checkCondition("UI_ACTION", count, 6, count <= 6);
  await page.locator(".projects-toolbar .icon-button").click();
  const dialog = page.getByRole("dialog", {
    name: locale === "ru" ? "Проекты" : "Projects",
    exact: true,
  });
  try {
    await expect(dialog).toBeVisible();
    const populated = await dialog.locator(".project-list__item").count();
    if (!populated) throw new ReadonlyFixtureMissing();
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
    if (!historyItems) throw new ReadonlyFixtureMissing();
    await entries.first().click();
    if (!(await composer.count()) || !(await composer.isEnabled()))
      throw new ReadonlyFixtureMissing();
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
    const response = await observeActionResponse(
      page,
      (value) =>
        new URL(value.url()).pathname === "/api/v1/assistant-conversations" &&
        value.request().method() === "GET",
      () => search.fill(`${prefix}-absent`),
    );
    checkCondition("HTTP_STATUS", response.status(), 200);
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
