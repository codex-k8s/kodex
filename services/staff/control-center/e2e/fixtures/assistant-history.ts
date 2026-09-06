import { expect, type Page } from "@playwright/test";
import type { AssistantConversation } from "../../src/shared/api/generated/openapi/types.gen";
export async function checkAssistantHistory(
  page: Page,
  projectRef: string,
): Promise<void> {
  const conversation: AssistantConversation = {
    ref: "cnv_recent",
    state: "ACTIVE",
    projectRef,
    version: 1,
    title: "Текущий диалог",
    titleSource: "USER_EDITED",
    titleRevision: 1,
    context: {
      route: `/projects/${projectRef}/files`,
      entityKind: "PROJECT",
      entityRef: projectRef,
      entityVersion: 1,
      entityName: "Проект",
      allowedOperations: [],
    },
    turns: [],
    updatedAt: "2026-09-05T01:00:00Z",
  };
  const cursors: (string | null)[] = [];
  let older: AssistantConversation = {
    ...conversation,
    ref: "cnv_older",
    title: "Предыдущий диалог",
    updatedAt: "2026-09-04T01:00:00Z",
  };
  const searches: { query: string; state: string; cursor: string | null }[] =
    [];
  let archives = 0;
  await page.route(
    "**/api/v1/assistant-conversations/cnv_older/archive",
    async (route) => {
      archives += 1;
      expect(route.request().method()).toBe("POST");
      expect(route.request().headers()["if-match"]).toBe('"1"');
      expect(route.request().headers()["idempotency-key"]).toBeTruthy();
      expect(route.request().headers()["x-csrf-token"]).toBeTruthy();
      older = { ...older, state: "ARCHIVED", version: 2 };
      await route.fulfill({ json: older, headers: { ETag: '"2"' } });
    },
  );
  await page.route("**/api/v1/assistant-conversations*", async (route) => {
    const query = new URL(route.request().url()).searchParams;
    expect(query.get("projectRef")).toBe(projectRef);
    expect(query.get("pageSize")).toBe("40");
    const cursor = query.get("pageToken");
    const text = query.get("query") ?? "";
    const state = query.get("state") ?? "ACTIVE";
    searches.push({ query: text, state, cursor });
    cursors.push(cursor);
    if (text || state !== "ACTIVE") {
      expect(cursor).toBeNull();
      await route.fulfill({
        json: {
          items:
            older.state === state && older.title.includes(text) ? [older] : [],
        },
      });
      return;
    }
    await route.fulfill({
      json: cursor
        ? {
            items: older.state === "ACTIVE" ? [older] : [],
          }
        : { items: [conversation], nextPageToken: "history_next" },
    });
  });
  await page
    .getByRole("button", { name: "Открыть Kodex", exact: true })
    .click();
  const dialog = page.locator("#assistant-workspace");
  const mobile = (page.viewportSize()?.width ?? 1440) < 1001;
  if (mobile)
    await dialog
      .getByRole("button", { name: "История диалогов", exact: true })
      .click();
  const history = page.locator(
    mobile ? ".assistant-history__menu" : ".assistant-conversation-sidebar",
  );
  await history.getByRole("button", { name: /ещё/ }).click();
  await expect(
    history.getByRole("button", { name: /Предыдущий диалог/ }),
  ).toBeVisible();
  expect(cursors).toEqual([null, "history_next"]);
  await history.getByRole("button", { name: /Предыдущий диалог/ }).click();
  await expect(dialog).toHaveAttribute("data-conversation-ref", "cnv_older");
  const composer = dialog.locator(".assistant-composer textarea");
  await composer.fill("Несохранённый текст");
  let closeWarning = "";
  page.once("dialog", async (prompt) => {
    closeWarning = prompt.message();
    await prompt.dismiss();
  });
  await composer.press("Escape");
  await expect(dialog).toBeVisible();
  await expect(composer).toHaveValue("Несохранённый текст");
  expect(closeWarning).toContain("неотправленным сообщением");
  await composer.fill("");
  if (mobile)
    await dialog
      .getByRole("button", { name: "История диалогов", exact: true })
      .click();
  await history
    .getByRole("searchbox", { name: "Поиск диалогов", exact: true })
    .fill("Предыдущий");
  await expect(
    history.getByRole("button", { name: /Текущий диалог/ }),
  ).toHaveCount(0);
  await expect(
    history.getByRole("button", { name: /Предыдущий диалог/ }),
  ).toBeVisible();
  expect(searches.at(-1)).toEqual({
    query: "Предыдущий",
    state: "ACTIVE",
    cursor: null,
  });
  await history.getByRole("button", { name: /Предыдущий диалог/ }).click();
  page.once("dialog", (prompt) => prompt.accept());
  await dialog
    .getByRole("button", { name: "Архивировать диалог", exact: true })
    .click();
  await expect.poll(() => archives).toBe(1);
  await expect(dialog).not.toHaveAttribute(
    "data-conversation-ref",
    "cnv_older",
  );
  if (mobile)
    await dialog
      .getByRole("button", { name: "История диалогов", exact: true })
      .click();
  await history
    .getByRole("combobox", { name: "Состояние диалогов", exact: true })
    .selectOption("ARCHIVED");
  await expect(
    history.getByRole("button", { name: /Предыдущий диалог/ }),
  ).toBeVisible();
  expect(searches.at(-1)).toEqual({
    query: "Предыдущий",
    state: "ARCHIVED",
    cursor: null,
  });
  await history.getByRole("button", { name: /Предыдущий диалог/ }).click();
  await expect(dialog).toHaveAttribute("data-conversation-ref", "cnv_older");
  await expect(
    dialog.getByRole("button", { name: "Архивировать диалог", exact: true }),
  ).toHaveCount(0);
  await expect(dialog.locator(".assistant-composer textarea")).toBeDisabled();
  await dialog
    .locator(".assistant-drawer__header")
    .getByRole("button", { name: "Закрыть", exact: true })
    .click();
}
