import {
  prepareInstructionsImpact,
  commandAgentInstructions,
  getAgent,
} from "@/shared/api/generated/openapi/sdk.gen";
import type {
  Agent,
  RevisionImpactPlan,
  InstructionPublicationResult,
} from "@/shared/api/generated/openapi/types.gen";
import { requestSignal } from "@/shared/api/client";
import { etag, mutate } from "@/shared/api/mutation";
import { unwrap } from "@/shared/api/problem";

export async function readInstructionPublicationAgent(
  agentRef: string,
): Promise<Agent> {
  const agent = (
    await unwrap(
      getAgent({
        path: { agentRef },
        signal: requestSignal(),
        cache: "no-store",
      }),
    )
  ).data;
  if (agent.ref !== agentRef)
    throw new Error("Instruction publication agent mismatch");
  return agent;
}
import {
  checkedPublicationPlan,
  publicationPlanIdentity,
  publicationSelection,
} from "@/features/runtime/publication-impact";

export async function prepareInstructionPublication(
  agent: Agent,
): Promise<RevisionImpactPlan> {
  if (!agent.draftInstructions || agent.draftInstructions.state !== "VALID")
    throw new Error("Valid instruction draft is required");
  const plan = checkedPublicationPlan(
    (
      await mutate(
        (headers) =>
          prepareInstructionsImpact({
            path: { agentRef: agent.ref },
            headers: { ...headers, "If-Match": etag(agent.version) },
            signal: requestSignal(),
          }),
        agent.version,
      )
    ).data,
  );
  if (
    plan.kind !== "AGENT_INSTRUCTIONS" ||
    plan.sourceRef !== agent.ref ||
    plan.sourceVersion !== agent.version ||
    plan.draftRef !== agent.draftInstructions.ref ||
    plan.draftVersion !== agent.draftInstructions.version ||
    plan.state !== "PREPARED"
  )
    throw new Error("Instruction publication plan mismatch");
  return plan;
}
export async function publishInstructions(
  agent: Agent,
  plan: RevisionImpactPlan,
  selected: string[],
  key: string,
): Promise<InstructionPublicationResult> {
  if (
    plan.kind !== "AGENT_INSTRUCTIONS" ||
    plan.sourceRef !== agent.ref ||
    plan.sourceVersion !== agent.version ||
    plan.draftRef !== agent.draftInstructions?.ref ||
    plan.draftVersion !== agent.draftInstructions.version
  )
    throw new Error("Instruction publication intent changed");
  const input = publicationSelection(plan, selected);
  const result = (
    await mutate(
      (headers) =>
        commandAgentInstructions({
          path: { agentRef: agent.ref },
          body: { action: "PUBLISH", ...input },
          headers: { ...headers, "If-Match": etag(agent.version) },
          signal: requestSignal(),
        }),
      agent.version,
      key,
    )
  ).data;
  if (!("plan" in result))
    throw new Error("Instruction publication receipt is missing");
  checkedPublicationPlan(result.plan);
  if (
    publicationPlanIdentity(result.plan) !== publicationPlanIdentity(plan) ||
    result.plan.state !== "APPLIED" ||
    result.agent.ref !== agent.ref ||
    result.agent.projectRef !== agent.projectRef ||
    result.agent.version <= agent.version
  )
    throw new Error("Instruction publication receipt mismatch");
  return result;
}
