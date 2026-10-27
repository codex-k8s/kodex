import type { MutationHeaders } from "./mutation";

// Копирует только три заголовка подготовленной versioned mutation, сохраняя прежний пустой
// fallback If-Match. Не читает cookie, не создаёт ключ, не выбирает version и не повторяет запрос.
export function versionedMutationHeaders(
  headers: MutationHeaders,
): Required<MutationHeaders> {
  return {
    "Idempotency-Key": headers["Idempotency-Key"],
    "X-CSRF-Token": headers["X-CSRF-Token"],
    "If-Match": headers["If-Match"] ?? "",
  };
}
