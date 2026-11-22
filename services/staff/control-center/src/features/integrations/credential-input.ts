// Выбирает только формат однократного ввода; definition key не выдаёт доступ к ключу или SSH.
export function usesMultilineSSHCredential(definitionKey: string): boolean {
  return definitionKey === "ssh-managed" || definitionKey === "ssh-execute";
}
