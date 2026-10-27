// Проверяет прежнюю широкую форму record: все ненулевые объекты, кроме массивов.
export function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

// Сравнивает точные перечисляемые собственные ключи без изменения expected.
export function hasExactKeys(
  value: Record<string, unknown>,
  expected: readonly string[],
): boolean {
  const actual = Object.keys(value).sort();
  const sortedExpected = [...expected].sort();
  return (
    actual.length === sortedExpected.length &&
    sortedExpected.every((key, index) => actual[index] === key)
  );
}
