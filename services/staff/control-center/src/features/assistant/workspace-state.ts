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

function conversationKey(projectRef?: string): string {
  return `${assistantConversationKey}.${projectRef ?? "all"}`;
}

export function restoreAssistantConversationRef(
  projectRef?: string,
  storage: WorkspaceStorage = window.sessionStorage,
): string | undefined {
  try {
    return storage.getItem(conversationKey(projectRef)) || undefined;
  } catch {
    return undefined;
  }
}

export function persistAssistantConversationRef(
  projectRef: string | undefined,
  conversationRef: string,
  storage: WorkspaceStorage = window.sessionStorage,
): void {
  try {
    storage.setItem(conversationKey(projectRef), conversationRef);
  } catch {
    // Недоступное session storage не должно блокировать выбор диалога.
  }
}
