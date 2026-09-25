export const openAssistantEvent = "kodex:assistant:open";

export interface AssistantIntegrationPublicationRequest {
  configurationRef: string;
  revisionRef: string;
}

export function openAssistantWorkspace(): void {
  window.dispatchEvent(new CustomEvent(openAssistantEvent));
}

export function requestAssistantIntegrationPublication(
  request: AssistantIntegrationPublicationRequest,
): void {
  if (
    !/^mcfg_[A-Za-z0-9_-]{1,91}$/.test(request.configurationRef) ||
    !/^mrev_[A-Za-z0-9_-]{1,91}$/.test(request.revisionRef)
  )
    return;
  window.dispatchEvent(
    new CustomEvent(openAssistantEvent, { detail: request }),
  );
}
