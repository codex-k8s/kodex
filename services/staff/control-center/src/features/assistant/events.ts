export const openAssistantEvent = "kodex:assistant:open";

export function openAssistantWorkspace(): void {
  window.dispatchEvent(new CustomEvent(openAssistantEvent));
}
