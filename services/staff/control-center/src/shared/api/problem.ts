import { isRecord } from "@/shared/lib/exact-object";
import type {
  Problem,
  PromptTemplateDiagnostic,
  IntegrationCredentialPreflightIssue,
} from "@/shared/api/generated/openapi/types.gen";
import {
  assertOwnerRequest,
  ownerRequestSignal,
  OwnerContextChangedError,
} from "./owner-lifetime";
import {
  ingressProxyRecoveryCode,
  recoverIngressProxySession,
} from "./proxy-session-recovery";

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
  readonly title?: string;
  readonly detail?: string;
  readonly retryAfterSeconds?: number;
  readonly diagnostics: readonly PromptTemplateDiagnostic[];
  readonly integrationCredentialIssues: readonly IntegrationCredentialPreflightIssue[];

  constructor(value: {
    status: number;
    code: string;
    correlationId?: string;
    retryable: boolean;
    kind: ProblemKind;
    title?: string;
    detail?: string;
    retryAfterSeconds?: number;
    diagnostics?: readonly PromptTemplateDiagnostic[];
    integrationCredentialIssues?: readonly IntegrationCredentialPreflightIssue[];
  }) {
    super(value.code);
    this.name = "AppProblem";
    this.status = value.status;
    this.code = value.code;
    this.correlationId = value.correlationId;
    this.retryable = value.retryable;
    this.kind = value.kind;
    this.title = value.title;
    this.detail = value.detail;
    this.retryAfterSeconds = value.retryAfterSeconds;
    this.diagnostics = value.diagnostics ?? [];
    this.integrationCredentialIssues = (
      value.integrationCredentialIssues ?? []
    ).map((issue) => ({ ...issue }));
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
    typeof value.code === "string" &&
    /^[A-Z0-9_]{1,80}$/.test(value.code) &&
    "status" in value &&
    typeof value.status === "number" &&
    Number.isSafeInteger(value.status) &&
    value.status >= 400 &&
    value.status <= 599
  );
}

function isRetryable(value: unknown): value is { retryable: boolean } {
  return (
    typeof value === "object" &&
    value !== null &&
    "retryable" in value &&
    typeof value.retryable === "boolean"
  );
}

function isUnknownArray(value: unknown): value is unknown[] {
  return Array.isArray(value);
}

function promptDiagnostics(value: unknown): PromptTemplateDiagnostic[] {
  if (typeof value !== "object" || value === null || !("diagnostics" in value))
    return [];
  const diagnostics: unknown = value.diagnostics;
  if (!isUnknownArray(diagnostics) || diagnostics.length > 100) return [];
  const valid = diagnostics.every(
    (item): item is PromptTemplateDiagnostic =>
      typeof item === "object" &&
      item !== null &&
      "severity" in item &&
      (item.severity === "ERROR" || item.severity === "WARNING") &&
      "code" in item &&
      typeof item.code === "string" &&
      /^[A-Z0-9_]{1,80}$/.test(item.code) &&
      "message" in item &&
      typeof item.message === "string" &&
      item.message.length <= 500 &&
      "line" in item &&
      typeof item.line === "number" &&
      Number.isSafeInteger(item.line) &&
      item.line >= 1 &&
      "column" in item &&
      typeof item.column === "number" &&
      Number.isSafeInteger(item.column) &&
      item.column >= 1 &&
      (!("variableName" in item) ||
        (typeof item.variableName === "string" &&
          item.variableName.length <= 160)),
  );
  return valid ? diagnostics : [];
}

const integrationCredentialProblemCodes = new Set<string>([
  "CREDENTIAL_MISSING",
  "CREDENTIAL_CHECK_REQUIRED",
  "CREDENTIAL_INVALID",
  "CREDENTIAL_INSUFFICIENT",
  "CREDENTIAL_PERMISSION_UNCONFIRMED",
  "CREDENTIAL_EXPIRED",
  "CREDENTIAL_REVOKED",
  "CREDENTIAL_INITIATOR_REQUIRED",
  "CREDENTIAL_CONNECTION_UNAVAILABLE",
]);

function integrationCredentialIssues(
  value: unknown,
): IntegrationCredentialPreflightIssue[] | undefined {
  if (
    !isRecord(value) ||
    !isUnknownArray(value.integrationCredentialIssues) ||
    value.integrationCredentialIssues.length < 1 ||
    value.integrationCredentialIssues.length > 256
  )
    return undefined;
  const result: IntegrationCredentialPreflightIssue[] = [];
  const seen = new Set<string>();
  for (const item of value.integrationCredentialIssues) {
    if (
      !isRecord(item) ||
      Object.keys(item).some(
        (key) =>
          ![
            "connectionRef",
            "connectionName",
            "capabilityKey",
            "problem",
          ].includes(key),
      ) ||
      typeof item.connectionRef !== "string" ||
      !/^[A-Za-z0-9_-]{8,96}$/.test(item.connectionRef) ||
      typeof item.capabilityKey !== "string" ||
      !/^[A-Za-z0-9_.:-]{1,256}$/.test(item.capabilityKey) ||
      typeof item.problem !== "string" ||
      !integrationCredentialProblemCodes.has(item.problem) ||
      (item.connectionName !== undefined &&
        (typeof item.connectionName !== "string" ||
          Array.from(item.connectionName).length > 200 ||
          /[\r\n\0]/.test(item.connectionName)))
    )
      return undefined;
    const key = `${item.connectionRef}:${item.capabilityKey}`;
    if (seen.has(key)) return undefined;
    seen.add(key);
    result.push({
      connectionRef: item.connectionRef,
      capabilityKey: item.capabilityKey,
      problem: item.problem as IntegrationCredentialPreflightIssue["problem"],
      ...(typeof item.connectionName === "string" && item.connectionName
        ? { connectionName: item.connectionName }
        : {}),
    });
  }
  return result;
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
  const credentialIssues =
    code === "INTEGRATION_CREDENTIALS_REQUIRED"
      ? integrationCredentialIssues(value)
      : [];
  if (
    code === "INTEGRATION_CREDENTIALS_REQUIRED" &&
    (status !== 409 ||
      (response && response.status !== 409) ||
      !credentialIssues ||
      !isRecord(value) ||
      value.retryable !== false)
  )
    return new AppProblem({
      status: 502,
      code: "INVALID_UPSTREAM_RESPONSE",
      retryable: false,
      kind: "unavailable",
    });
  const correlationId =
    isProblem(value) && typeof value.correlationId === "string"
      ? value.correlationId
      : undefined;
  const title =
    isProblem(value) && typeof value.title === "string"
      ? value.title
      : undefined;
  const detail =
    isProblem(value) && typeof value.detail === "string"
      ? value.detail
      : undefined;
  const retryable = isRetryable(value)
    ? value.retryable
    : status === 0 || status === 429 || status >= 500;
  const transcriptionRateLimit =
    status === 429 && code === "TRANSCRIPTION_RATE_LIMITED";
  const retryAfter = response?.headers.get("Retry-After") ?? "";
  const retryAfterSeconds =
    transcriptionRateLimit &&
    /^[1-9][0-9]{0,2}$/.test(retryAfter) &&
    Number(retryAfter) <= 300
      ? Number(retryAfter)
      : undefined;
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
  return new AppProblem({
    status,
    code,
    retryable: transcriptionRateLimit
      ? retryable && retryAfterSeconds !== undefined
      : retryable,
    ...(retryAfterSeconds === undefined ? {} : { retryAfterSeconds }),
    kind,
    ...(correlationId ? { correlationId } : {}),
    ...(title ? { title } : {}),
    ...(detail ? { detail } : {}),
    diagnostics: promptDiagnostics(value),
    integrationCredentialIssues: credentialIssues,
  });
}

export interface ApiReadback<T> {
  data: T;
  etag?: string;
  location?: string;
}

export async function unwrap<T>(
  request: Promise<GeneratedResponse<T>>,
): Promise<ApiReadback<NonNullable<T>>> {
  const scope = ownerRequestSignal();
  let result: GeneratedResponse<T>;
  try {
    result = await request;
  } catch (error) {
    assertOwnerRequest(scope);
    throw error;
  }
  assertOwnerRequest(scope);
  if (!result.response) {
    const problem = normalizeProblem(result.error);
    notifyUnauthorized(problem);
    throw problem;
  }
  if (!result.response.ok || result.error !== undefined) {
    if (recoverIngressProxySession(result.error, result.response))
      throw new AppProblem({
        status: 401,
        code: ingressProxyRecoveryCode,
        kind: "unavailable",
        retryable: false,
      });
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
  if (error instanceof OwnerContextChangedError)
    return new AppProblem({
      status: 0,
      code: "OWNER_CONTEXT_CHANGED",
      retryable: false,
      kind: "unknown",
    });
  return normalizeProblem(error);
}
