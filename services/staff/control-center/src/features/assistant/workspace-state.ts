const assistantWorkspaceOpenKey = "kodex.assistant.workspace.open";
const assistantConversationKey = "kodex.assistant.workspace.conversation";
const assistantScopeKey = "kodex.assistant.workspace.scope";

type WorkspaceStorage = Pick<Storage, "getItem" | "removeItem" | "setItem">;

export function restoreAssistantScope(
  projectRef?: string,
  storage?: WorkspaceStorage,
): "SYSTEM" | "PROJECT" {
  if (!projectRef) return "SYSTEM";
  try {
    const source =
      storage ??
      (typeof window !== "undefined" ? window.sessionStorage : undefined);
    return source?.getItem(`${assistantScopeKey}.${projectRef}`) === "PROJECT"
      ? "PROJECT"
      : "SYSTEM";
  } catch {
    return "SYSTEM";
  }
}

export function persistAssistantScope(
  projectRef: string | undefined,
  scope: "SYSTEM" | "PROJECT",
  storage?: WorkspaceStorage,
): void {
  if (!projectRef) return;
  try {
    const source =
      storage ??
      (typeof window !== "undefined" ? window.sessionStorage : undefined);
    source?.setItem(`${assistantScopeKey}.${projectRef}`, scope);
  } catch {
    // Предпочтение интерфейса не должно блокировать выбор помощника.
  }
}

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
