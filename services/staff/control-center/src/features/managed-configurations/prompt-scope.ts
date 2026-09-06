import type {
  PromptTemplateScope,
  PromptTemplateScopeInput,
} from "@/shared/api/generated/openapi/types.gen";

export function promptScopeInput(
  scope?: PromptTemplateScope,
): PromptTemplateScopeInput | undefined {
  if (!scope) return undefined;
  return {
    targetKind: scope.targetKind,
    targetRef: scope.targetRef,
    templateKind: scope.templateKind,
    ...(scope.contextPin.agentRef
      ? { agentRef: scope.contextPin.agentRef }
      : {}),
    ...(scope.contextPin.workflowRevisionRef
      ? { workflowRevisionRef: scope.contextPin.workflowRevisionRef }
      : {}),
    ...(scope.contextPin.workflowStageKey
      ? { workflowStageKey: scope.contextPin.workflowStageKey }
      : {}),
    expectedContextDigest: scope.contextPin.digest,
  };
}
