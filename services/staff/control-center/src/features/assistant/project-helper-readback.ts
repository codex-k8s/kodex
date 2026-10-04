import {
  readProjectAssistant,
  readProjectAssistantAgent,
} from "@/features/assistant/api";
import {
  assistantProjectHelperScope,
  hasAssistantProjectHelperLocator,
} from "@/features/assistant/model";
import type {
  Agent,
  AssistantPlan,
  AssistantPlanOperationInput,
} from "@/shared/api/generated/openapi/types.gen";

export async function readAssistantProjectHelper(
  plan: AssistantPlan,
  operation: AssistantPlanOperationInput,
  organizationRef: string | undefined,
  signal: AbortSignal,
): Promise<Agent | undefined> {
  if (!hasAssistantProjectHelperLocator(operation)) return;
  const scope = assistantProjectHelperScope(plan, operation, organizationRef);
  if (!scope) throw new Error("Assistant project helper owner pins mismatch");
  const profile = await readProjectAssistant(scope.projectRef, signal);
  if (
    profile.ref !== scope.profileRef ||
    profile.agentRef !== scope.agentRef ||
    profile.projectRef !== scope.projectRef
  )
    throw new Error("Assistant project helper profile readback mismatch");
  return readProjectAssistantAgent(profile, signal);
}
