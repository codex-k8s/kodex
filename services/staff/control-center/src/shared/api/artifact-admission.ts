/** Обычные файлы с явным NOT_SCANNED остаются доступными без выдуманной AV-аттестации.
 * Функция не проверяет права/receipt и не применяется к импорту skills или исполняемых bundles. */
export function artifactContentAdmitted(state: unknown): boolean {
  return state === "CLEAN" || state === "NOT_SCANNED";
}
