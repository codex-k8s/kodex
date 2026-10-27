import type {
  Workflow,
  WorkflowInput,
} from "@/shared/api/generated/openapi/types.gen";
import type { PromptTarget } from "@/features/agents/detail/prompt-context";

export type WorkflowEditorInput = Required<
  Omit<WorkflowInput, "resultEvidenceProfile" | "reworkPolicy">
> &
  Pick<WorkflowInput, "resultEvidenceProfile"> & {
    reworkPolicy: "" | "OWNER_APPROVED_UPSTREAM_V1";
  };
export type WorkflowEditorState = WorkflowEditorInput;

export function workflowEditorInput(workflow: Workflow): WorkflowEditorInput {
  const body = workflow.draft ?? workflow;
  return {
    executionMode: body.executionMode ?? "MANAGER_DRIVEN",
    reworkPolicy: body.reworkPolicy ?? "",
    resultEvidenceProfile: body.resultEvidenceProfile,
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
      ref: step.ref,
      dependsOn: [...(step.dependsOn ?? [])],
      resultRequirements: (step.resultRequirements ?? []).map((item) => ({
        ...item,
      })),
      position: step.position,
      name: step.name,
      purpose: step.purpose,
      agentRef: step.agentRef ?? "",
      parallel: step.parallel,
      parallelGroup: step.parallelGroup,
      humanGate: step.humanGate,
      timeoutSeconds: step.timeoutSeconds,
      ...(step.approvalTimeoutSeconds === undefined
        ? {}
        : { approvalTimeoutSeconds: step.approvalTimeoutSeconds }),
      expectedResult: step.expectedResult,
      gateDecisions: [...step.gateDecisions],
      requiredCapabilityKeys: [...step.requiredCapabilityKeys],
    })),
  };
}

export function workflowEditorAfterAuthoritativeUpdate(
  current: WorkflowEditorInput,
  saved: string,
  projectRef: string,
  workflowRef: string,
  workflow: Workflow,
): WorkflowEditorInput | undefined {
  if (
    !saved ||
    JSON.stringify(current) !== saved ||
    workflow.projectRef !== projectRef ||
    workflow.ref !== workflowRef
  )
    return undefined;
  return workflowEditorInput(workflow);
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
