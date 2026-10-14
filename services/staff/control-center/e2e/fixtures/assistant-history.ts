import { expect, type Page } from "@playwright/test";
import type {
  AssistantConversation,
  SystemAssistant,
} from "../../src/shared/api/generated/openapi/types.gen";
import {
  scopedSyntheticSnapshot,
  type SyntheticSnapshotInstaller,
} from "./runs-catalog";
export async function checkAssistantHistory(
  page: Page,
  projectRef: string,
  assistant: SystemAssistant,
  installSnapshot: SyntheticSnapshotInstaller,
): Promise<void> {
  const conversation: AssistantConversation = {
    ref: "cnv_recent",
    assistantScope: "SYSTEM",
    assistantRef: assistant.ref,
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
  installSnapshot(
    "SYSTEM_ASSISTANT",
    scopedSyntheticSnapshot(projectRef, () => ({
      assistant: { assistant },
      conversations: {
        conversations: [conversation],
        page: { nextPageToken: "history_next" },
      },
    })),
  );
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
  await page.route("**/api/v1/assistant-conversations?**", async (route) => {
    const query = new URL(route.request().url()).searchParams;
    expect(query.get("projectRef")).toBe(projectRef);
    const size = Number(query.get("pageSize"));
    expect(Number.isSafeInteger(size)).toBe(true);
    expect(size).toBeGreaterThanOrEqual(6);
    expect(size).toBeLessThanOrEqual(100);
    expect(query.get("assistantScope")).toBe("SYSTEM");
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
  await page.goto(`/projects/${projectRef}/files`);
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
  // Видимый sentinel догружает серверную страницу без ручного нажатия.
  await expect(
    history.getByRole("button", { name: /Предыдущий диалог/ }),
  ).toBeVisible();
  expect(cursors).toEqual([null, "history_next"]);
  await history.getByRole("button", { name: /Предыдущий диалог/ }).click();
  await expect(dialog).toHaveAttribute("data-conversation-ref", "cnv_older");
  const composer = dialog.locator(".assistant-composer textarea");
  await composer.fill("Несохранённый текст");
  await composer.press("Escape");
  const confirmation = page
    .getByRole("dialog")
    .filter({ has: page.locator(".confirmation-dialog") });
  await expect(confirmation).toContainText("неотправленным сообщением");
  await confirmation
    .getByRole("button", { name: "Отмена", exact: true })
    .click();
  await expect(dialog).toBeVisible();
  await expect(composer).toHaveValue("Несохранённый текст");
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
  await dialog
    .getByRole("button", { name: "Удалить диалог", exact: true })
    .click();
  await expect(confirmation).toContainText("Переместить диалог в корзину?");
  await confirmation
    .getByRole("button", { name: "Удалить диалог", exact: true })
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
    dialog.getByRole("button", { name: "Удалить диалог", exact: true }),
  ).toHaveCount(0);
  await expect(dialog.locator(".assistant-composer textarea")).toBeDisabled();
  await dialog
    .locator(".assistant-drawer__header")
    .getByRole("button", { name: "Закрыть", exact: true })
    .click();
}
