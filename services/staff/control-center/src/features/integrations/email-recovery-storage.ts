// Общий предел безопасных recovery metadata EMAIL, прежний для чтения и добавления.
export const emailRecoveryEntryLimit = 20;

// Возвращает null только для отсутствующего/пустого Storage value; JSON null
// отклоняется. Проверки полей, дубликатов и запись остаются у владельца схемы.
// JSON parse/getItem errors проходят без перехвата, кэширования или повторов.
export function readEmailRecoveryEntries(
  storage: Pick<Storage, "getItem">,
  storageKey: string,
  invalidMessage: string,
): unknown[] | null {
  const raw = storage.getItem(storageKey);
  if (!raw) return null;
  const parsed: unknown = JSON.parse(raw);
  if (!Array.isArray(parsed) || parsed.length > emailRecoveryEntryLimit)
    throw new Error(invalidMessage);
  const entries: unknown[] = parsed;
  return entries;
}
