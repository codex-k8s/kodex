import type {
  AccessRole,
  AccessSubject,
} from "@/shared/api/generated/openapi/types.gen";
import type { AsyncEntityOption } from "@/shared/ui/async-entity-picker";

// selection сохраняет одиночное строковое значение и сбрасывает остальные варианты picker.
export function selection(value: string | null | readonly string[]): string {
  return typeof value === "string" ? value : "";
}

// createPickerLabels переводит подписи при каждом вызове, учитывая текущую локаль.
export function createPickerLabels(translate: (key: string) => string) {
  return (label: string, searchPlaceholder: string) => ({
    label,
    searchPlaceholder,
    loading: translate("common.loading"),
    loadingMore: translate("common.loading"),
    empty: translate("common.empty"),
    error: translate("errors.default"),
    retry: translate("common.retry"),
  });
}

// accessSubjectOption сохраняет ссылку и отображаемое имя участника.
export function accessSubjectOption(subject: AccessSubject): AsyncEntityOption {
  return { ref: subject.ref, title: subject.displayName };
}

// accessRoleOption выбирает ref роли или конкретной версии по назначению формы.
export function accessRoleOption(
  role: AccessRole,
  value: "ROLE" | "VERSION",
): AsyncEntityOption {
  return {
    ref: value === "VERSION" ? role.currentVersion.ref : role.ref,
    title: role.currentVersion.name,
    description: role.currentVersion.description,
    meta: `v${String(role.currentVersion.revision)}`,
  };
}
