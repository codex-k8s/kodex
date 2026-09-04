const assistantWorkspaceOpenKey = "kodex.assistant.workspace.open";

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
