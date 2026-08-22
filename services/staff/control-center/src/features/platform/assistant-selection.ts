import type { AssistantConversation } from "@/shared/api/generated/openapi";

export function selectedConversation(
  conversations: AssistantConversation[],
  selectedRef: string | null | undefined,
): AssistantConversation | undefined {
  if (selectedRef === null) return undefined;
  if (selectedRef) {
    return conversations.find(
      (conversation) => conversation.ref === selectedRef,
    );
  }
  return conversations[0];
}
