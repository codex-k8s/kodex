import type {
  RoleImageBuild,
  Run,
  RunNode,
} from "@/shared/api/generated/openapi/types.gen";

export const openAssistantEvent = "kodex:assistant:open";
export const assistantPlanAppliedEvent = "kodex:assistant:plan-applied";

export function requestAssistantSettings(): void {
  window.dispatchEvent(
    new CustomEvent(openAssistantEvent, { detail: { kind: "SETTINGS" } }),
  );
}

export function isAssistantSettingsRequest(
  value: unknown,
): value is { kind: "SETTINGS" } {
  return Boolean(
    value &&
    typeof value === "object" &&
    "kind" in value &&
    value.kind === "SETTINGS",
  );
}

export interface AssistantSetupRequest {
  kind: "SETUP";
  step: "project" | "image" | "environment" | "team" | "launch";
}

export function isAssistantSetupRequest(
  value: unknown,
): value is AssistantSetupRequest {
  if (!value || typeof value !== "object") return false;
  const request = value as Partial<AssistantSetupRequest>;
  return (
    request.kind === "SETUP" &&
    ["project", "image", "environment", "team", "launch"].includes(
      request.step ?? "",
    )
  );
}

export function requestAssistantSetup(
  step: AssistantSetupRequest["step"],
): void {
  const detail: AssistantSetupRequest = { kind: "SETUP", step };
  window.dispatchEvent(new CustomEvent(openAssistantEvent, { detail }));
}

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

export interface AssistantRunDebugRequest {
  kind: "RUN_DEBUG";
  runRef: string;
  rootRunRef: string;
  targetType: "AGENT" | "WORKFLOW";
  attempt: number;
  safeErrorCode?: string;
  failedNodes: Array<{
    nodeRef: string;
    type: RunNode["type"];
    agentRef?: string;
    safeErrorCode?: string;
  }>;
}

function isAssistantRunDebugNode(
  value: unknown,
): value is AssistantRunDebugRequest["failedNodes"][number] {
  if (!value || typeof value !== "object") return false;
  const node = value as Partial<
    AssistantRunDebugRequest["failedNodes"][number]
  >;
  const opaqueRef = /^[A-Za-z][A-Za-z0-9_-]{1,127}$/;
  const diagnosticCode = /^[A-Z0-9_]{1,80}$/;
  return (
    typeof node.nodeRef === "string" &&
    opaqueRef.test(node.nodeRef) &&
    [
      "ROOT_PROCESS",
      "AGENT_EXECUTION",
      "HUMAN_GATE",
      "EXTERNAL_ACTION",
    ].includes(node.type ?? "") &&
    (node.agentRef === undefined ||
      (typeof node.agentRef === "string" && opaqueRef.test(node.agentRef))) &&
    (node.safeErrorCode === undefined ||
      (typeof node.safeErrorCode === "string" &&
        diagnosticCode.test(node.safeErrorCode)))
  );
}

export function isAssistantRunDebugRequest(
  value: unknown,
): value is AssistantRunDebugRequest {
  if (!value || typeof value !== "object") return false;
  const request = value as Partial<AssistantRunDebugRequest>;
  const opaqueRef = /^[A-Za-z][A-Za-z0-9_-]{1,127}$/;
  const diagnosticCode = /^[A-Z0-9_]{1,80}$/;
  const nodes = (value as { failedNodes?: unknown }).failedNodes;
  return (
    request.kind === "RUN_DEBUG" &&
    typeof request.runRef === "string" &&
    opaqueRef.test(request.runRef) &&
    typeof request.rootRunRef === "string" &&
    opaqueRef.test(request.rootRunRef) &&
    (request.targetType === "AGENT" || request.targetType === "WORKFLOW") &&
    typeof request.attempt === "number" &&
    Number.isSafeInteger(request.attempt) &&
    request.attempt > 0 &&
    (request.safeErrorCode === undefined ||
      (typeof request.safeErrorCode === "string" &&
        diagnosticCode.test(request.safeErrorCode))) &&
    Array.isArray(nodes) &&
    nodes.length > 0 &&
    nodes.length <= 20 &&
    nodes.every(isAssistantRunDebugNode)
  );
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

export function requestAssistantRunDebug(run: Run, nodes: RunNode[]): void {
  if (run.state !== "FAILED" || run.target.type === "SYSTEM_ASSISTANT") return;
  const request: AssistantRunDebugRequest = {
    kind: "RUN_DEBUG",
    runRef: run.ref,
    rootRunRef: run.rootRunRef,
    targetType: run.target.type,
    attempt: run.attempt,
    safeErrorCode: run.safeErrorCode,
    failedNodes: nodes
      .filter((node) => node.state === "FAILED")
      .slice(0, 20)
      .map((node) => ({
        nodeRef: node.ref,
        type: node.type,
        agentRef: node.agentRef,
        safeErrorCode: node.safeErrorCode,
      })),
  };
  if (!isAssistantRunDebugRequest(request)) return;
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
