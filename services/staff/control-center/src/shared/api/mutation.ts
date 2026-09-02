import { asProblem, unwrap, type ApiReadback } from "@/shared/api/problem";

interface GeneratedResponse<T> {
  data?: T;
  error?: unknown;
  response?: Response;
}

export interface MutationHeaders {
  "Idempotency-Key": string;
  "X-CSRF-Token": string;
  "If-Match"?: string;
}

function readCookie(name: string): string | undefined {
  const prefix = `${encodeURIComponent(name)}=`;
  for (const part of document.cookie.split(";")) {
    const value = part.trim();
    if (value.startsWith(prefix))
      return decodeURIComponent(value.slice(prefix.length));
  }
  return undefined;
}

export function csrfToken(): string {
  const value = readCookie("__Host-kodex-csrf");
  if (!value || value.length < 43 || value.length > 256)
    throw new Error("CSRF token is unavailable");
  return value;
}

export function etag(version: number): string {
  if (!Number.isSafeInteger(version) || version < 1)
    throw new Error("Resource version is invalid");
  return `"${String(version)}"`;
}

export function idempotencyKey(): string {
  return crypto.randomUUID();
}

export async function mutate<T>(
  request: (headers: MutationHeaders) => Promise<GeneratedResponse<T>>,
  version?: number,
  requestIdempotencyKey = idempotencyKey(),
): Promise<ApiReadback<NonNullable<T>>> {
  const headers: MutationHeaders = {
    "Idempotency-Key": requestIdempotencyKey,
    "X-CSRF-Token": csrfToken(),
  };
  if (version !== undefined) headers["If-Match"] = etag(version);
  try {
    return await unwrap(request(headers));
  } catch (error) {
    throw asProblem(error);
  }
}
