import {
  changeSystemAssistantIntegrationGrant,
  getSystemAssistantIntegrationGrantCandidates,
  listIntegrationConnections,
} from "@/shared/api/generated/openapi/sdk.gen";
import type {
  IntegrationConnection,
  SystemAssistantIntegrationGrantCandidates,
  SystemAssistantIntegrationGrantCandidate,
  SystemAssistantIntegrationGrantInput,
} from "@/shared/api/generated/openapi/types.gen";
import { requestSignal } from "@/shared/api/client";
import { mutate } from "@/shared/api/mutation";
import { unwrap } from "@/shared/api/problem";
import { allowedIntegrationApprovalPolicies } from "@/features/integrations/ui/model";
import { isSystemAssistantGrantScope } from "./system-integration-grant-plan";
import {
  approvalScopeOptions,
  validApprovalScopeSelection,
} from "@/features/integrations/approval-scope-options";

export interface SystemGrantOwner {
  organizationRef: string;
  assistantRef: string;
  assistantVersion: number;
}

const candidateReasons = [
  "READY",
  "CONNECTION_UNAVAILABLE",
  "RECIPIENT_UNAVAILABLE",
  "PACKAGE_UNAVAILABLE",
  "GRANT_UNAVAILABLE",
  "WORKFLOW_EXCLUDED",
];
const positiveVersion = (value: number) =>
  Number.isSafeInteger(value) && value >= 1;

export function assertSystemGrantCandidates(
  page: SystemAssistantIntegrationGrantCandidates,
  owner: SystemGrantOwner,
  connection: IntegrationConnection,
): void {
  if (
    !isSystemAssistantGrantScope(page.scopeKind) ||
    !owner.organizationRef ||
    !owner.assistantRef ||
    !positiveVersion(owner.assistantVersion) ||
    page.organizationRef !== owner.organizationRef ||
    page.assistantRef !== owner.assistantRef ||
    page.assistantVersion !== owner.assistantVersion ||
    page.connectionRef !== connection.ref ||
    page.connectionVersion !== connection.version ||
    !positiveVersion(page.connectionVersion) ||
    page.definitionVersion !== connection.definitionVersion ||
    !page.definitionVersion ||
    page.definitionDigest !== connection.definitionDigest ||
    !/^[a-f0-9]{64}$/.test(page.definitionDigest) ||
    !Array.isArray(page.items) ||
    page.items.length > 100 ||
    !Number.isSafeInteger(page.total) ||
    page.total < page.items.length ||
    typeof page.nextPageToken !== "string" ||
    new Set(page.items.map((item) => item.capability.key)).size !==
      page.items.length
  )
    throw new Error("Invalid system assistant grant candidate pins");
  for (const item of page.items) {
    if (
      typeof item.capability.key !== "string" ||
      !item.capability.key ||
      item.capability.key.length > 160 ||
      typeof item.capability.name !== "string" ||
      typeof item.capability.description !== "string" ||
      !candidateReasons.includes(item.reason) ||
      typeof item.grantable !== "boolean" ||
      (item.grantable && item.reason !== "READY") ||
      !Number.isSafeInteger(item.currentGrantVersion) ||
      item.currentGrantVersion < 0 ||
      typeof item.currentGrantEnabled !== "boolean" ||
      !!item.currentGrantRef !== item.currentGrantVersion > 0 ||
      (item.currentGrantRef !== undefined &&
        (typeof item.currentGrantRef !== "string" || !item.currentGrantRef)) ||
      (item.currentGrantEnabled && !item.currentGrantRef) ||
      (item.currentGrantRef &&
        !["NONE", "HUMAN_EACH_EFFECT", "HUMAN_SCOPED"].includes(
          item.currentApprovalPolicy ?? "",
        )) ||
      !Array.isArray(item.currentApprovalScopePaths) ||
      item.currentApprovalScopePaths.length > 16 ||
      new Set(item.currentApprovalScopePaths).size !==
        item.currentApprovalScopePaths.length ||
      item.currentApprovalScopePaths.some(
        (path) => typeof path !== "string" || !path || path.length > 160,
      ) ||
      (item.currentApprovalPolicy !== "HUMAN_SCOPED" &&
        item.currentApprovalScopePaths.length > 0)
    )
      throw new Error("Invalid system assistant grant candidate");
  }
}

export function selectedSystemGrantPolicy(
  candidate: SystemAssistantIntegrationGrantCandidate,
): SystemAssistantIntegrationGrantInput["approvalPolicy"] | undefined {
  const allowed = allowedIntegrationApprovalPolicies(candidate.capability);
  const selected = candidate.currentGrantRef
    ? candidate.currentApprovalPolicy
    : candidate.capability.approvalPolicy;
  return selected && allowed.includes(selected) ? selected : undefined;
}

export function validSystemGrantSelection(
  candidate: SystemAssistantIntegrationGrantCandidate | undefined,
  policy: SystemAssistantIntegrationGrantInput["approvalPolicy"] | undefined,
  paths: readonly string[],
): boolean {
  if (
    !candidate?.grantable ||
    !policy ||
    !allowedIntegrationApprovalPolicies(candidate.capability).includes(policy)
  )
    return false;
  return policy === "HUMAN_SCOPED"
    ? validApprovalScopeSelection(
        paths,
        approvalScopeOptions(candidate.capability.inputSchema).filter(
          (path) => path.length <= 160,
        ),
      )
    : paths.length === 0;
}

export async function readSystemGrantConnections(
  query: string,
  cursor: string | undefined,
  signal: AbortSignal,
  pageSize = 40,
) {
  return (
    await unwrap(
      listIntegrationConnections({
        query: { query, pageToken: cursor, pageSize },
        signal: requestSignal(signal),
        cache: "no-store",
      }),
    )
  ).data;
}

export async function readSystemGrantCandidates(
  owner: SystemGrantOwner,
  connection: IntegrationConnection,
  query: string,
  cursor: string | undefined,
  signal: AbortSignal,
  pageSize = 40,
) {
  const page = (
    await unwrap(
      getSystemAssistantIntegrationGrantCandidates({
        query: {
          connectionRef: connection.ref,
          query,
          pageToken: cursor,
          pageSize,
        },
        signal: requestSignal(signal),
        cache: "no-store",
      }),
    )
  ).data;
  assertSystemGrantCandidates(page, owner, connection);
  return page;
}

export async function saveSystemGrant(
  connection: IntegrationConnection,
  input: SystemAssistantIntegrationGrantInput,
  signal: AbortSignal,
) {
  signal.throwIfAborted();
  const result = await mutate(
    (headers) =>
      changeSystemAssistantIntegrationGrant({
        body: input,
        headers: {
          "If-Match": headers["If-Match"] ?? "",
          "Idempotency-Key": headers["Idempotency-Key"],
          "X-CSRF-Token": headers["X-CSRF-Token"],
        },
        signal: requestSignal(signal),
      }),
    connection.version,
  );
  signal.throwIfAborted();
  if (
    result.data.ref !== connection.ref ||
    !positiveVersion(result.data.version) ||
    result.data.version < connection.version ||
    result.data.definitionVersion !== connection.definitionVersion ||
    result.data.definitionDigest !== connection.definitionDigest
  )
    throw new Error("System assistant grant receipt mismatch");
  return result.data;
}
