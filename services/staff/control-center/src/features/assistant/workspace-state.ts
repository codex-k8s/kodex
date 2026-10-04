const assistantWorkspaceOpenKey = "kodex.assistant.workspace.open";
const assistantConversationKey = "kodex.assistant.workspace.conversation";

type WorkspaceStorage = Pick<Storage, "getItem" | "removeItem" | "setItem">;

export function restoreAssistantWorkspaceOpen(
  storage: WorkspaceStorage = window.sessionStorage,
): boolean {
  try {
    return storage.getItem(assistantWorkspaceOpenKey) === "1";
  } catch {
    return false;
  }
}

export function persistAssistantWorkspaceOpen(
  open: boolean,
  storage: WorkspaceStorage = window.sessionStorage,
): void {
  try {
    if (open) storage.setItem(assistantWorkspaceOpenKey, "1");
    else storage.removeItem(assistantWorkspaceOpenKey);
  } catch {
    // Недоступное session storage не должно блокировать работу помощника.
  }
}

function conversationKey(
  projectRef: string | undefined,
  assistantScope: "SYSTEM" | "PROJECT",
): string {
  return `${assistantConversationKey}.${assistantScope}.${projectRef ?? "all"}`;
}

export function restoreAssistantConversationRef(
  projectRef?: string,
  storage: WorkspaceStorage = window.sessionStorage,
  assistantScope: "SYSTEM" | "PROJECT" = "SYSTEM",
): string | undefined {
  try {
    return (
      storage.getItem(conversationKey(projectRef, assistantScope)) || undefined
    );
  } catch {
    return undefined;
  }
}

export function persistAssistantConversationRef(
  projectRef: string | undefined,
  conversationRef: string,
  storage: WorkspaceStorage = window.sessionStorage,
  assistantScope: "SYSTEM" | "PROJECT" = "SYSTEM",
): void {
  try {
    storage.setItem(
      conversationKey(projectRef, assistantScope),
      conversationRef,
    );
  } catch {
    // Недоступное session storage не должно блокировать выбор диалога.
  }
}
