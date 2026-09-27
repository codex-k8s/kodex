import type { RoleImageBuild } from "@/shared/api/generated/openapi/types.gen";

export const openAssistantEvent = "kodex:assistant:open";
export const assistantPlanAppliedEvent = "kodex:assistant:plan-applied";

export interface AssistantPlanAppliedDetail {
  projectRef?: string;
  kinds: string[];
}

export function notifyAssistantPlanApplied(
  detail: AssistantPlanAppliedDetail,
): void {
  window.dispatchEvent(new CustomEvent(assistantPlanAppliedEvent, { detail }));
}

export interface AssistantIntegrationPublicationRequest {
  configurationRef: string;
  revisionRef: string;
}

export interface AssistantRoleImageBuildDebugRequest {
  kind: "ROLE_IMAGE_BUILD_DEBUG";
  recipeRef: string;
  buildRef: string;
  attempt: number;
  stage: "FAILED" | "EXPIRED" | "DEAD_LETTER";
  safeErrorCode?: string;
  diagnosticCode?: string;
  diagnosticSummary?: string;
}

export function isAssistantRoleImageBuildDebugRequest(
  value: unknown,
): value is AssistantRoleImageBuildDebugRequest {
  if (!value || typeof value !== "object") return false;
  const request = value as Partial<AssistantRoleImageBuildDebugRequest>;
  const opaqueRef = /^[A-Za-z][A-Za-z0-9_-]{1,127}$/;
  const diagnosticCode = /^[A-Z0-9_]{1,80}$/;
  return (
    request.kind === "ROLE_IMAGE_BUILD_DEBUG" &&
    typeof request.recipeRef === "string" &&
    opaqueRef.test(request.recipeRef) &&
    typeof request.buildRef === "string" &&
    opaqueRef.test(request.buildRef) &&
    typeof request.attempt === "number" &&
    Number.isSafeInteger(request.attempt) &&
    request.attempt > 0 &&
    ["FAILED", "EXPIRED", "DEAD_LETTER"].includes(request.stage ?? "") &&
    (request.safeErrorCode === undefined ||
      (typeof request.safeErrorCode === "string" &&
        diagnosticCode.test(request.safeErrorCode))) &&
    (request.diagnosticCode === undefined ||
      (typeof request.diagnosticCode === "string" &&
        diagnosticCode.test(request.diagnosticCode))) &&
    (request.diagnosticSummary === undefined ||
      (typeof request.diagnosticSummary === "string" &&
        request.diagnosticSummary.length <= 256))
  );
}

export function requestAssistantRoleImageBuildDebug(
  build: RoleImageBuild,
): void {
  const request = {
    kind: "ROLE_IMAGE_BUILD_DEBUG",
    recipeRef: build.recipeRef,
    buildRef: build.ref,
    attempt: build.attempt,
    stage: build.stage,
    safeErrorCode: build.safeErrorCode,
    diagnosticCode: build.diagnosticCode,
    diagnosticSummary: build.diagnosticSummary,
  };
  if (!isAssistantRoleImageBuildDebugRequest(request)) return;
  window.dispatchEvent(
    new CustomEvent(openAssistantEvent, { detail: request }),
  );
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
