import { asProblem, unwrap, type ApiReadback } from "@/shared/api/problem";
import { assertOwnerRequest, ownerRequestSignal } from "./owner-lifetime";

const mutationRetryDelaysMs = [0, 200, 600] as const;

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

function mutationHeaders(
  version: number | undefined,
  requestIdempotencyKey: string,
): MutationHeaders {
  const headers: MutationHeaders = {
    "Idempotency-Key": requestIdempotencyKey,
    "X-CSRF-Token": csrfToken(),
  };
  if (version !== undefined) headers["If-Match"] = etag(version);
  return headers;
}

async function executeMutation<T>(
  request: (headers: MutationHeaders) => Promise<GeneratedResponse<T>>,
  headers: MutationHeaders,
): Promise<ApiReadback<NonNullable<T>>> {
  try {
    return await unwrap(request(headers));
  } catch (error) {
    throw asProblem(error);
  }
}

export async function mutate<T>(
  request: (headers: MutationHeaders) => Promise<GeneratedResponse<T>>,
  version?: number,
  requestIdempotencyKey = idempotencyKey(),
): Promise<ApiReadback<NonNullable<T>>> {
  return executeMutation(
    request,
    mutationHeaders(version, requestIdempotencyKey),
  );
}

export async function mutateWithRetry<T>(
  request: (headers: MutationHeaders) => Promise<GeneratedResponse<T>>,
  version?: number,
  requestIdempotencyKey = idempotencyKey(),
): Promise<ApiReadback<NonNullable<T>>> {
  const scope = ownerRequestSignal();
  const headers = mutationHeaders(version, requestIdempotencyKey);
  for (const delayMs of mutationRetryDelaysMs) {
    if (delayMs > 0) {
      await new Promise<void>((resolve) =>
        globalThis.setTimeout(resolve, delayMs),
      );
    }
    try {
      assertOwnerRequest(scope);
      return await executeMutation(request, headers);
    } catch (error) {
      assertOwnerRequest(scope);
      const problem = asProblem(error);
      if (!problem.retryable || delayMs === mutationRetryDelaysMs.at(-1)) {
        throw problem;
      }
    }
  }
  throw new Error("Mutation retry attempts were not executed");
}
