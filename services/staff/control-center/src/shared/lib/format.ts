export function formatDateTime(
  value: string | undefined,
  locale: string,
): string {
  if (!value) return "—";
  const date = new Date(value);
  if (Number.isNaN(date.valueOf())) return "—";
  return new Intl.DateTimeFormat(locale, {
    dateStyle: "medium",
    timeStyle: "short",
  }).format(date);
}

export function shortDigest(value: string | undefined): string {
  if (!value) return "—";
  return value.length > 18 ? `${value.slice(0, 10)}…${value.slice(-6)}` : value;
}

export function formatDuration(
  seconds: number | undefined,
  locale: string,
): string {
  if (seconds === undefined || !Number.isFinite(seconds)) return "—";
  return (
    new Intl.NumberFormat(locale, { maximumFractionDigits: 1 }).format(
      seconds,
    ) + " s"
  );
}
