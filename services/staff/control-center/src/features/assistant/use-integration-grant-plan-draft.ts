import { computed, type Ref } from "vue";
import type {
  AssistantPlanOperationInput,
  SystemAssistantIntegrationGrantCandidate,
} from "@/shared/api/generated/openapi/types.gen";
import {
  operationInputs,
  operationParameter,
  type EditablePlanOperation,
} from "./model";
import { validSystemGrantSelection } from "./system-integration-grants";
import { useIntegrationGrantPlanPolicy } from "./use-integration-grant-plan-policy";

// Общая проекция редактируемых полей и признака valid; owner predicate остаётся у формы.
// Ошибочный ввод закрыто считается недоступным, сетевые запросы и полномочия здесь не меняются.
export function useIntegrationGrantPlanDraft(
  operation: () => EditablePlanOperation,
  ownerLifetime: AbortSignal,
  owner: (input: AssistantPlanOperationInput) => boolean,
  candidate: Readonly<
    Ref<SystemAssistantIntegrationGrantCandidate | undefined>
  >,
  loading: Readonly<Ref<boolean>>,
  failed: Readonly<Ref<boolean>>,
  changed: (key: string, selected: string | boolean | string[]) => void,
) {
  function value(key: string): unknown {
    try {
      return operationParameter(operation(), key);
    } catch {
      return undefined;
    }
  }
  const input = computed(() => {
    try {
      return operationInputs([operation()])[0];
    } catch {
      return undefined;
    }
  });
  const ownerValid = computed(
    () => !ownerLifetime.aborted && !!input.value && owner(input.value),
  );
  const { policy, paths, fields, changePolicy, togglePath } =
    useIntegrationGrantPlanPolicy(candidate, value, changed);
  const valid = computed(
    () =>
      ownerValid.value &&
      !loading.value &&
      !failed.value &&
      validSystemGrantSelection(candidate.value, policy.value, paths.value),
  );
  return {
    input,
    value,
    ownerValid,
    valid,
    policy,
    fields,
    changePolicy,
    togglePath,
  };
}
