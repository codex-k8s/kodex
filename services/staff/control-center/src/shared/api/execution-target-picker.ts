import { requestSignal } from "@/shared/api/client";
import {
  listAgents,
  listWorkflows,
} from "@/shared/api/generated/openapi/sdk.gen";
import type { Agent, Workflow } from "@/shared/api/generated/openapi/types.gen";
import { unwrap } from "@/shared/api/problem";
import type {
  AsyncEntityOption,
  AsyncEntityOptionPage,
} from "@/shared/ui/async-entity-picker";

export type ExecutionTargetType = "AGENT" | "WORKFLOW";

export interface ExecutionTargetPickerOption extends AsyncEntityOption {
  target: Agent | Workflow;
  targetType: ExecutionTargetType;
}

export type ExecutionTargetPickerLoader = (
  query: string,
  cursor: string | undefined,
  signal: AbortSignal,
) => Promise<AsyncEntityOptionPage>;

const targetPageSize = 40;

function combinedSignal(signal: AbortSignal): AbortSignal {
  return AbortSignal.any([signal, requestSignal()]);
}

function optionalQuery(query: string): string | undefined {
  const value = query.trim();
  return value || undefined;
}

export function isEligibleAgent(agent: Agent): boolean {
  return agent.enabled && !agent.system && agent.nextActions.includes("LAUNCH");
}

export function isEligibleWorkflow(workflow: Workflow): boolean {
  return (
    workflow.state === "PUBLISHED" && workflow.nextActions.includes("LAUNCH")
  );
}

export function toExecutionTargetOption(
  targetType: ExecutionTargetType,
  target: Agent | Workflow,
): ExecutionTargetPickerOption {
  return {
    ref: target.ref,
    title: target.name,
    description: target.purpose,
    meta:
      targetType === "AGENT"
        ? (target as Agent).roleDefinitionName
        : `v${String(target.version)}`,
    target,
    targetType,
  };
}

export function selectedExecutionTargetOption(
  targetType: ExecutionTargetType,
  target: Agent | Workflow | undefined,
): ExecutionTargetPickerOption | undefined {
  if (!target) return undefined;
  return toExecutionTargetOption(targetType, target);
}

export function targetRefAfterTypeChange(
  currentType: ExecutionTargetType,
  nextType: ExecutionTargetType,
  currentRef: string,
): string {
  return currentType === nextType ? currentRef : "";
}

export function createExecutionTargetPickerLoader(
  projectRef: string,
  targetType: ExecutionTargetType,
): ExecutionTargetPickerLoader {
  return async (query, cursor, signal) => {
    const searchQuery = optionalQuery(query);
    const visited = new Set(cursor ? [cursor] : []);

    async function loadEligiblePage(
      pageToken?: string,
    ): Promise<AsyncEntityOptionPage> {
      const response =
        targetType === "AGENT"
          ? await unwrap(
              listAgents({
                path: { projectRef },
                query: {
                  pageSize: targetPageSize,
                  ...(pageToken ? { pageToken } : {}),
                  ...(searchQuery ? { query: searchQuery } : {}),
                },
                signal: combinedSignal(signal),
              }),
            )
          : await unwrap(
              listWorkflows({
                path: { projectRef },
                query: {
                  pageSize: targetPageSize,
                  ...(pageToken ? { pageToken } : {}),
                  ...(searchQuery ? { query: searchQuery } : {}),
                },
                signal: combinedSignal(signal),
              }),
            );
      const items = response.data.items.filter((item) =>
        targetType === "AGENT"
          ? isEligibleAgent(item as Agent)
          : isEligibleWorkflow(item as Workflow),
      );
      const nextPageToken = response.data.nextPageToken || undefined;
      if (items.length > 0 || !nextPageToken || visited.has(nextPageToken)) {
        return {
          items: items.map((item) => toExecutionTargetOption(targetType, item)),
          ...(nextPageToken && !visited.has(nextPageToken)
            ? { nextPageToken }
            : {}),
        };
      }
      visited.add(nextPageToken);
      return loadEligiblePage(nextPageToken);
    }

    return loadEligiblePage(cursor);
  };
}
