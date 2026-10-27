import type { unwrap as unwrapFunction } from "@/shared/api/problem";

type Request = (headers: Record<string, string>) => Promise<unknown>;

// Синтетические заголовки тестовых мутаций; optional сохраняет исходное отсутствие If-Match при нуле.
function headers(
  version: number | undefined,
  key: string,
  csrf: string,
  optional: boolean,
): Record<string, string> {
  if (optional)
    return {
      "Idempotency-Key": key,
      "X-CSRF-Token": csrf,
      ...(version ? { "If-Match": `"${String(version)}"` } : {}),
    };
  return {
    "If-Match": `"${String(version)}"`,
    "Idempotency-Key": key,
    "X-CSRF-Token": csrf,
  };
}

// Синхронно передаёт тестовый request и не перехватывает его исключения.
export function fixedSyntheticMutation(
  request: Request,
  version: number,
): Promise<unknown> {
  return request(headers(version, "synthetic-key", "synthetic-csrf", false));
}

// Сохраняет асинхронный контракт create/update черновика с необязательной версией.
export async function optionalSecretMutation(
  request: Request,
  version: number | undefined,
  key: string,
): Promise<unknown> {
  return request(headers(version, key, "csrf", true));
}

// Сохраняет асинхронный контракт публикации с обязательным If-Match.
export async function requiredSecretMutation(
  request: Request,
  version: number,
  key: string,
): Promise<unknown> {
  return request(headers(version, key, "csrf", false));
}

// Для managed-конфигураций сохраняет unwrap и отдельные синтетические ключи сценариев.
export function managedSyntheticMutation(
  unwrap: typeof unwrapFunction,
  key: string,
  csrf: string,
) {
  return {
    etag: (version: number) => `"${String(version)}"`,
    mutate: (
      request: (
        headers: Record<string, string>,
      ) => Parameters<typeof unwrap>[0],
      version: number,
    ) => unwrap(request(headers(version, key, csrf, false))),
  };
}
