import { expect, type Page } from "@playwright/test";
import type { AssistantConversation } from "../../src/shared/api/generated/openapi/types.gen";
export async function checkAssistantHistory(
  page: Page,
  projectRef: string,
): Promise<void> {
  const conversation: AssistantConversation = {
    ref: "cnv_recent",
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
  await page.route("**/api/v1/assistant-conversations*", async (route) => {
    const query = new URL(route.request().url()).searchParams;
    expect(query.get("projectRef")).toBe(projectRef);
    expect(query.get("pageSize")).toBe("40");
    const cursor = query.get("pageToken");
    cursors.push(cursor);
    await route.fulfill({
      json: cursor
        ? {
            items: [
              {
                ...conversation,
                ref: "cnv_older",
                title: "Предыдущий диалог",
                updatedAt: "2026-09-04T01:00:00Z",
              },
            ],
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
  await dialog
    .locator(".assistant-drawer__header")
    .getByRole("button", { name: "Закрыть", exact: true })
    .click();
}
