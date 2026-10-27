import { computed, type Ref } from "vue";
import type {
  SystemAssistantIntegrationGrantCandidate,
  SystemAssistantIntegrationGrantInput,
} from "@/shared/api/generated/openapi/types.gen";
import { approvalScopeOptions } from "@/features/integrations/approval-scope-options";
import { allowedIntegrationApprovalPolicies } from "@/features/integrations/ui/model";

// Снимок только редактируемых UI полей; не содержит owner или authority pins.
export interface IntegrationGrantPlanPolicyFields {
  readonly policy:
    | SystemAssistantIntegrationGrantInput["approvalPolicy"]
    | undefined;
  readonly paths: readonly string[];
  readonly allowedPolicies: readonly SystemAssistantIntegrationGrantInput["approvalPolicy"][];
  readonly allowedPaths: readonly string[];
}

// Меняет только редактируемые параметры плана через callback вызывающей формы.
// Проверка SYSTEM/PROJECT owner, каталогов и версий остаётся у вызывающего кода.
export function useIntegrationGrantPlanPolicy(
  candidate: Readonly<
    Ref<SystemAssistantIntegrationGrantCandidate | undefined>
  >,
  value: (key: string) => unknown,
  changed: (key: string, selected: string | boolean | string[]) => void,
) {
  // Исторический или неизвестный выбор не заменяется политикой по умолчанию.
  const policy = computed(() =>
    allowedIntegrationApprovalPolicies(candidate.value?.capability).find(
      (item) => item === value("approvalPolicy"),
    ),
  );
  // Некорректный массив закрыто проецируется в пустой выбор без изменения ввода.
  const paths = computed<string[]>(() => {
    const selected = value("approvalScopePaths");
    return Array.isArray(selected) &&
      selected.every((path) => typeof path === "string")
      ? selected
      : [];
  });
  // Доступные значения берутся из актуального кандидата, а не из ввода формы.
  const allowedPolicies = computed(() =>
    allowedIntegrationApprovalPolicies(candidate.value?.capability),
  );
  // Только объявленные схемой пути допустимой длины доступны для выбора.
  const allowedPaths = computed(() =>
    approvalScopeOptions(candidate.value?.capability.inputSchema).filter(
      (path) => path.length <= 160,
    ),
  );
  // Смена политики сначала сбрасывает прежние пути; порядок событий сохраняется.
  function changePolicy(selected: string): void {
    if (
      !allowedPolicies.value.includes(
        selected as SystemAssistantIntegrationGrantInput["approvalPolicy"],
      )
    )
      return;
    changed("approvalScopePaths", []);
    changed("approvalPolicy", selected);
  }
  // Неизвестный путь игнорируется; выбор копируется, удаляет дубли и сортируется.
  function togglePath(path: string, checked: boolean): void {
    if (!allowedPaths.value.includes(path)) return;
    changed(
      "approvalScopePaths",
      checked
        ? [...new Set([...paths.value, path])].sort()
        : paths.value.filter((item) => item !== path),
    );
  }
  // Один реактивный снимок для общего представления без копии состояния формы.
  const fields = computed<IntegrationGrantPlanPolicyFields>(() => ({
    policy: policy.value,
    paths: paths.value,
    allowedPolicies: allowedPolicies.value,
    allowedPaths: allowedPaths.value,
  }));
  return {
    policy,
    paths,
    fields,
    allowedPolicies,
    allowedPaths,
    changePolicy,
    togglePath,
  };
}
