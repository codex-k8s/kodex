const effortOrder = [
  "none",
  "minimal",
  "low",
  "medium",
  "high",
  "xhigh",
  "max",
  "ultra",
];

export function orderedReasoningEfforts(values: readonly string[]): string[] {
  const rank = (value: string): number => {
    const index = effortOrder.indexOf(value);
    return index < 0 ? effortOrder.length : index;
  };
  return [...values].sort(
    (left, right) => rank(left) - rank(right) || left.localeCompare(right),
  );
}

export function catalogDate(value: string | undefined, locale: string): string {
  if (!value) return "";
  const date = new Date(value);
  if (!Number.isFinite(date.getTime())) return "";
  return new Intl.DateTimeFormat(locale, { dateStyle: "medium" }).format(date);
}
