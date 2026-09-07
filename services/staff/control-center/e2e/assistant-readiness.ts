import { expect, type Locator } from "@playwright/test";

// Оба entrypoint используют один fresh readiness/permission predicate.
export async function expectAssistantConversationActions(
  assistant: Locator,
  enabled: boolean,
): Promise<void> {
  for (const region of [
    ".assistant-drawer__header",
    ".assistant-conversation-sidebar",
  ]) {
    const action = assistant.locator(region).getByRole("button", {
      name: "Новый диалог",
      exact: true,
      includeHidden: true,
    });
    await expect(action).toHaveCount(1);
    await expect(action).toBeEnabled({ enabled });
  }
}
