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
