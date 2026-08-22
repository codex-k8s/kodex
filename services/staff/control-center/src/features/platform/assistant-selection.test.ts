import { describe, expect, it } from "vitest";

import { selectedConversation } from "./assistant-selection";

const conversations = [
  { ref: "conversation_recent", updatedAt: "2026-08-23T10:00:00Z" },
  { ref: "conversation_previous", updatedAt: "2026-08-23T09:00:00Z" },
] as Parameters<typeof selectedConversation>[0];

describe("selectedConversation", () => {
  it("выбирает последний диалог при первом открытии", () => {
    expect(selectedConversation(conversations, undefined)?.ref).toBe(
      "conversation_recent",
    );
  });

  it("не переиспользует существующий диалог после действия Новый диалог", () => {
    expect(selectedConversation(conversations, null)).toBeUndefined();
  });

  it("сохраняет явный выбор пользователя", () => {
    expect(
      selectedConversation(conversations, "conversation_previous")?.ref,
    ).toBe("conversation_previous");
  });
});
