import type { Problem } from "@/shared/api/generated/openapi/types.gen";

export type ProblemKind =
  | "unauthorized"
  | "forbidden"
  | "not-found"
  | "conflict"
  | "unavailable"
  | "unknown";

export class AppProblem extends Error {
  readonly status: number;
  readonly code: string;
  readonly correlationId?: string;
  readonly retryable: boolean;
  readonly kind: ProblemKind;

  constructor(value: {
    status: number;
    code: string;
    correlationId?: string;
    retryable: boolean;
    kind: ProblemKind;
  }) {
    super(value.code);
    this.name = "AppProblem";
    this.status = value.status;
    this.code = value.code;
    this.correlationId = value.correlationId;
    this.retryable = value.retryable;
    this.kind = value.kind;
  }
}

interface GeneratedResponse<T> {
  data?: T;
  error?: unknown;
  response?: Response;
}

let unauthorizedHandler: (() => void) | null = null;
let unauthorizedNotified = false;

export function setUnauthorizedHandler(handler: (() => void) | null): void {
  unauthorizedHandler = handler;
  if (handler === null) unauthorizedNotified = false;
}

export function resetUnauthorizedNotification(): void {
  unauthorizedNotified = false;
}

function notifyUnauthorized(problem: AppProblem): void {
  if (problem.kind !== "unauthorized" || unauthorizedNotified) return;
  unauthorizedNotified = true;
  unauthorizedHandler?.();
}

export function notifyAuthoritativeUnauthorized(): void {
  notifyUnauthorized(
    new AppProblem({
      status: 401,
      code: "UNAUTHENTICATED",
      retryable: false,
      kind: "unauthorized",
    }),
  );
}

function isProblem(value: unknown): value is Problem {
  return (
    typeof value === "object" &&
    value !== null &&
    "code" in value &&
    "status" in value
  );
}

export function normalizeProblem(
  value: unknown,
  response?: Response,
): AppProblem {
  const status =
    isProblem(value) && typeof value.status === "number"
      ? value.status
      : (response?.status ?? 0);
  const code =
    isProblem(value) && typeof value.code === "string" ? value.code : "UNKNOWN";
  const correlationId =
    isProblem(value) && typeof value.correlationId === "string"
      ? value.correlationId
      : undefined;
  const retryable =
    isProblem(value) && typeof value.retryable === "boolean"
      ? value.retryable
      : status === 0 || status === 429 || status >= 500;
  const kind: ProblemKind =
    status === 401
      ? "unauthorized"
      : status === 403
        ? "forbidden"
        : status === 404
          ? "not-found"
          : status === 409 || status === 412
            ? "conflict"
            : status === 0 || status === 429 || status >= 500
              ? "unavailable"
              : "unknown";
  return new AppProblem(
    correlationId === undefined
      ? { status, code, retryable, kind }
      : { status, code, correlationId, retryable, kind },
  );
}

export interface ApiReadback<T> {
  data: T;
  etag?: string;
  location?: string;
}

export async function unwrap<T>(
  request: Promise<GeneratedResponse<T>>,
): Promise<ApiReadback<NonNullable<T>>> {
  const result = await request;
  if (!result.response) {
    const problem = normalizeProblem(result.error);
    notifyUnauthorized(problem);
    throw problem;
  }
  if (!result.response.ok || result.error !== undefined) {
    const problem = normalizeProblem(result.error, result.response);
    notifyUnauthorized(problem);
    throw problem;
  }
  const readback: ApiReadback<NonNullable<T>> = {
    data: result.data as NonNullable<T>,
  };
  const etagValue = result.response.headers.get("ETag");
  const location = result.response.headers.get("Location");
  if (etagValue) readback.etag = etagValue;
  if (location) readback.location = location;
  return readback;
}

export function asProblem(error: unknown): AppProblem {
  if (error instanceof AppProblem) return error;
  return normalizeProblem(error);
}
