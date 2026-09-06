import type {
  Workflow,
  WorkflowInput,
} from "@/shared/api/generated/openapi/types.gen";
import type { PromptTarget } from "@/features/agents/detail/prompt-context";

export function workflowEditorInput(
  workflow: Workflow,
): Required<WorkflowInput> {
  const body = workflow.draft ?? workflow;
  return {
    name: workflow.name,
    purpose: workflow.purpose,
    coordinatorAgentRef: body.coordinatorAgentRef ?? "",
    maxConcurrency: body.maxConcurrency ?? 1,
    timeoutSeconds: body.timeoutSeconds ?? 7200,
    completionCriteria: body.completionCriteria ?? "",
    inputFields: body.inputFields.map((field) => ({
      key: field.key,
      label: field.label,
      description: field.description,
      valueType: field.valueType,
      required: field.required,
      options: [...field.options],
    })),
    steps: body.steps.map((step) => ({
      position: step.position,
      name: step.name,
      purpose: step.purpose,
      agentRef: step.agentRef ?? "",
      parallel: step.parallel,
      parallelGroup: step.parallelGroup,
      humanGate: step.humanGate,
      timeoutSeconds: step.timeoutSeconds,
      expectedResult: step.expectedResult,
      gateDecisions: [...step.gateDecisions],
      requiredCapabilityKeys: [...step.requiredCapabilityKeys],
    })),
  };
}

export function workflowStagePromptTarget(
  workflow: Workflow,
  position: number,
): PromptTarget | undefined {
  const body = workflow.draft ?? workflow;
  const revisionRef = workflow.draft?.ref ?? workflow.revisionRef;
  const stage = body.steps.find((step) => step.position === position);
  if (!stage || !revisionRef) return undefined;
  return {
    projectRef: workflow.projectRef,
    targetKind: "WORKFLOW_STAGE",
    targetRef: workflow.ref,
    context: {
      expectedWorkflowVersion: workflow.version,
      workflowRevisionRef: revisionRef,
      workflowStageKey: stage.ref,
    },
  };
}
