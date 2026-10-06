import {
  getAgent,
  getProjectAssistant,
  getProjectAssistantIntegrationGrantCandidates,
} from "@/shared/api/generated/openapi/sdk.gen";
import type {
  IntegrationConnection,
  ProjectAssistantIntegrationGrantCandidates,
} from "@/shared/api/generated/openapi/types.gen";
import { requestSignal } from "@/shared/api/client";
import { unwrap } from "@/shared/api/problem";
import {
  assertSystemGrantCandidates,
  type SystemGrantOwner,
} from "./system-integration-grants";
export interface ProjectGrantOwner extends SystemGrantOwner {
  projectRef: string;
  assistantProfileRef: string;
  profileVersion: number;
}
export async function readProjectGrantOwner(
  organizationRef: string,
  projectRef: string,
  assistantRef: string,
  signal: AbortSignal,
): Promise<ProjectGrantOwner> {
  signal.throwIfAborted();
  if (!organizationRef || !projectRef || !assistantRef)
    throw new Error("Invalid project assistant grant owner");
  const profile = (
    await unwrap(
      getProjectAssistant({
        path: { projectRef },
        signal: requestSignal(signal),
        cache: "no-store",
      }),
    )
  ).data;
  signal.throwIfAborted();
  if (
    profile.projectRef !== projectRef ||
    profile.agentRef !== assistantRef ||
    !profile.ref ||
    profile.state !== "ACTIVE" ||
    !Number.isSafeInteger(profile.version) ||
    profile.version < 1
  )
    throw new Error("Invalid project assistant grant profile pins");
  const agent = (
    await unwrap(
      getAgent({
        path: { agentRef: profile.agentRef },
        signal: requestSignal(signal),
        cache: "no-store",
      }),
    )
  ).data;
  signal.throwIfAborted();
  const system: unknown = agent.system;
  const enabled: unknown = agent.enabled;
  if (
    agent.ref !== assistantRef ||
    agent.projectRef !== projectRef ||
    system !== false ||
    enabled !== true ||
    !Number.isSafeInteger(agent.version) ||
    agent.version < 1
  )
    throw new Error("Invalid project assistant grant owner pins");
  return {
    organizationRef,
    projectRef,
    assistantRef: agent.ref,
    assistantVersion: agent.version,
    assistantProfileRef: profile.ref,
    profileVersion: profile.version,
  };
}
export function assertProjectGrantCandidates(
  page: ProjectAssistantIntegrationGrantCandidates,
  owner: ProjectGrantOwner,
  connection: IntegrationConnection,
): void {
  if (
    !owner.projectRef ||
    !owner.assistantProfileRef ||
    !Number.isSafeInteger(owner.profileVersion) ||
    owner.profileVersion < 1 ||
    page.projectRef !== owner.projectRef ||
    page.assistantProfileRef !== owner.assistantProfileRef ||
    page.profileVersion !== owner.profileVersion
  )
    throw new Error("Invalid project assistant grant candidate owner");
  assertSystemGrantCandidates(page, owner, connection);
}
export async function readProjectGrantCandidates(
  owner: ProjectGrantOwner,
  connection: IntegrationConnection,
  query: string,
  cursor: string | undefined,
  signal: AbortSignal,
  pageSize = 40,
) {
  const page = (
    await unwrap(
      getProjectAssistantIntegrationGrantCandidates({
        path: { projectRef: owner.projectRef },
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
  signal.throwIfAborted();
  assertProjectGrantCandidates(page, owner, connection);
  return page;
}
