/** Разбирает contract-bounded capability list без локального реестра permissions. */
export function parseCapabilities(input: string): string[] | null {
  const values = input
    .split(",")
    .map((item) => item.trim())
    .filter(Boolean);
  if (
    values.length > 64 ||
    values.some((item) => item.length > 160) ||
    new Set(values).size !== values.length
  )
    return null;
  return values;
}
