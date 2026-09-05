import { getAgentEffectiveCapabilities } from "@/shared/api/generated/openapi/sdk.gen";
import type { AgentEffectiveCapability } from "@/shared/api/generated/openapi/types.gen";
import { requestSignal } from "@/shared/api/client";
import { AppProblem, unwrap } from "@/shared/api/problem";

export interface EffectiveCapabilityScope {
  agentRef: string;
  agentVersion?: number;
  projectRef?: string;
  workflowRef?: string;
  stepKey?: string;
}

export function effectiveCapabilityIdentity(
  item: AgentEffectiveCapability,
): string {
  return `${item.key}:${item.connectionRef ?? ""}:${item.grantRef ?? ""}`;
}

export async function loadEffectiveCapabilities(
  scope: EffectiveCapabilityScope,
  query: string,
  pageToken: string | undefined,
  digest: string | undefined,
  signal: AbortSignal,
) {
  if (Boolean(scope.workflowRef) !== Boolean(scope.stepKey))
    throw new Error("Incomplete workflow capability scope");
  const page = (
    await unwrap(
      getAgentEffectiveCapabilities({
        path: { agentRef: scope.agentRef },
        query: {
          query,
          pageToken,
          pageSize: 30,
          workflowRef: scope.workflowRef,
          stepKey: scope.stepKey,
        },
        signal: requestSignal(signal),
      }),
    )
  ).data;
  signal.throwIfAborted();
  if (
    page.agentRef !== scope.agentRef ||
    (scope.projectRef && page.projectRef !== scope.projectRef) ||
    (page.workflowRef ?? "") !== (scope.workflowRef ?? "") ||
    (page.stepKey ?? "") !== (scope.stepKey ?? "") ||
    !Number.isSafeInteger(page.total) ||
    page.total < page.items.length ||
    new Set(page.items.map(effectiveCapabilityIdentity)).size !==
      page.items.length
  )
    throw new Error("Invalid effective capability scope or page");
  if (
    (scope.agentVersion !== undefined &&
      page.agentVersion !== scope.agentVersion) ||
    (digest && page.digest !== digest)
  )
    throw new AppProblem({
      status: 412,
      code: "VERSION_MISMATCH",
      kind: "conflict",
      retryable: true,
    });
  return page;
}

export function canChangePlatformCapability(
  item: AgentEffectiveCapability,
  canManage: boolean,
): boolean {
  return (
    item.source === "PLATFORM" &&
    canManage &&
    (item.requested || item.grantable)
  );
}
