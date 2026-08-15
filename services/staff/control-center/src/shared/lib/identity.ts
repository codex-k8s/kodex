import type { ApiReadback } from "@/shared/api/problem";
import { AppProblem, asProblem, unwrap } from "@/shared/api/problem";

const registryKey = "mattercodex.control-center.mutation-intents.v1";

interface MutationIntentRecord {
  scopeDigest: string;
  requestDigest: string;
  idempotencyKey: string;
}

interface GeneratedResponse<T> {
  data?: T;
  error?: unknown;
  response?: Response;
}

export interface TrackedMutationReadback<T> extends ApiReadback<T> {
  idempotencyKey: string;
  requestDigest: string;
}

let memoryRegistry: MutationIntentRecord[] = [];
let mutationGuard: (() => boolean) | null = null;
let registryEpoch = 0;
let registryQueue: Promise<unknown> = Promise.resolve();

export function setMutationGuard(guard: (() => boolean) | null): void {
  mutationGuard = guard;
}

function stableJson(value: unknown): string {
  if (value === null || typeof value !== "object") return JSON.stringify(value);
  if (Array.isArray(value)) return `[${value.map(stableJson).join(",")}]`;
  const object = value as Record<string, unknown>;
  return `{${Object.keys(object)
    .filter((key) => object[key] !== undefined)
    .sort()
    .map((key) => `${JSON.stringify(key)}:${stableJson(object[key])}`)
    .join(",")}}`;
}

async function sha256(value: string): Promise<string> {
  const bytes = new TextEncoder().encode(value);
  const digest = await crypto.subtle.digest("SHA-256", bytes);
  return [...new Uint8Array(digest)]
    .map((byte) => byte.toString(16).padStart(2, "0"))
    .join("");
}

function readRegistry(): MutationIntentRecord[] {
  try {
    const raw = sessionStorage.getItem(registryKey);
    if (!raw) return memoryRegistry;
    const value: unknown = JSON.parse(raw);
    if (!Array.isArray(value)) return memoryRegistry;
    return (value as unknown[]).filter((item): item is MutationIntentRecord => {
      const candidate = item as Partial<MutationIntentRecord>;
      return (
        typeof item === "object" &&
        item !== null &&
        typeof candidate.scopeDigest === "string" &&
        typeof candidate.requestDigest === "string" &&
        typeof candidate.idempotencyKey === "string"
      );
    });
  } catch {
    return memoryRegistry;
  }
}

function writeRegistry(records: MutationIntentRecord[]): void {
  memoryRegistry = records;
  try {
    sessionStorage.setItem(registryKey, JSON.stringify(records));
  } catch {
    // Memory fallback still preserves an unknown outcome for this tab lifecycle.
  }
}

function serializeRegistry<T>(operation: () => T | Promise<T>): Promise<T> {
  const result = registryQueue.then(operation, operation);
  registryQueue = result.then(
    () => undefined,
    () => undefined,
  );
  return result;
}

async function prepareMutation(
  scope: string,
  body: unknown,
  version?: number,
): Promise<MutationIntentRecord> {
  const epoch = registryEpoch;
  const [scopeDigest, requestDigest] = await Promise.all([
    sha256(scope),
    sha256(stableJson({ body, version: version ?? null })),
  ]);
  return serializeRegistry(() => {
    if (epoch !== registryEpoch)
      throw new AppProblem({
        status: 401,
        code: "MUTATION_SESSION_INVALIDATED",
        retryable: false,
        kind: "unauthorized",
      });
    const records = readRegistry();
    const existing = records.find(
      (item) =>
        item.scopeDigest === scopeDigest &&
        item.requestDigest === requestDigest,
    );
    if (existing) return existing;
    if (records.length >= 128) {
      throw new AppProblem({
        status: 0,
        code: "IDEMPOTENCY_INTENT_CAPACITY_EXCEEDED",
        retryable: false,
        kind: "unavailable",
      });
    }
    const record = {
      scopeDigest,
      requestDigest,
      idempotencyKey: crypto.randomUUID(),
    };
    writeRegistry([...records, record]);
    return record;
  });
}

function finishMutation(record: MutationIntentRecord): Promise<void> {
  return serializeRegistry(() => {
    writeRegistry(
      readRegistry().filter(
        (item) =>
          item.scopeDigest !== record.scopeDigest ||
          item.requestDigest !== record.requestDigest,
      ),
    );
  });
}

export function clearMutationIntents(): void {
  registryEpoch += 1;
  writeRegistry([]);
}

export function etag(version: number): string {
  if (!Number.isSafeInteger(version) || version < 1)
    throw new Error("Resource version is invalid");
  return `"${String(version)}"`;
}

export function readCookie(name: string): string | undefined {
  const prefix = `${encodeURIComponent(name)}=`;
  for (const part of document.cookie.split(";")) {
    const value = part.trim();
    if (value.startsWith(prefix))
      return decodeURIComponent(value.slice(prefix.length));
  }
  return undefined;
}

export function csrfToken(): string {
  const value = readCookie("__Host-mattercodex-csrf");
  if (!value || value.length < 43 || value.length > 64) {
    throw new Error("CSRF token is unavailable");
  }
  return value;
}

function mutationHeaders(
  idempotencyKey: string,
  version?: number,
  csrfProtected = true,
): Record<string, string> {
  const headers: Record<string, string> = {
    "Idempotency-Key": idempotencyKey,
  };
  if (csrfProtected) headers["X-CSRF-Token"] = csrfToken();
  if (version !== undefined) headers["If-Match"] = etag(version);
  return headers;
}

async function executeTrackedMutation<T>(
  scope: string,
  body: unknown,
  version: number | undefined,
  request: (headers: Record<string, string>) => Promise<GeneratedResponse<T>>,
  csrfProtected: boolean,
): Promise<TrackedMutationReadback<NonNullable<T>>> {
  if (!scope.startsWith("session:") && mutationGuard && !mutationGuard()) {
    throw new AppProblem({
      status: 0,
      code: "AUTHORITATIVE_PROJECTION_STALE",
      retryable: true,
      kind: "unavailable",
    });
  }
  const record = await prepareMutation(scope, body, version);
  try {
    const result = await unwrap(
      request(mutationHeaders(record.idempotencyKey, version, csrfProtected)),
    );
    await finishMutation(record);
    return {
      ...result,
      idempotencyKey: record.idempotencyKey,
      requestDigest: record.requestDigest,
    };
  } catch (error) {
    const problem = asProblem(error);
    if (!problem.retryable && problem.status !== 0)
      await finishMutation(record);
    throw error;
  }
}

export function executeMutation<T>(
  scope: string,
  body: unknown,
  version: number | undefined,
  request: (headers: Record<string, string>) => Promise<GeneratedResponse<T>>,
): Promise<TrackedMutationReadback<NonNullable<T>>> {
  return executeTrackedMutation(scope, body, version, request, true);
}

/** Выполняет единственную bootstrap-мутацию до выдачи session и CSRF cookies. */
export function executeSessionAdmission<T>(
  body: unknown,
  request: (headers: Record<string, string>) => Promise<GeneratedResponse<T>>,
): Promise<TrackedMutationReadback<NonNullable<T>>> {
  return executeTrackedMutation(
    "session:admit",
    body,
    undefined,
    request,
    false,
  );
}

export async function pendingMutationKey(
  scope: string,
  body: unknown,
  version?: number,
): Promise<string | null> {
  const scopeDigest = await sha256(scope);
  const requestDigest = await sha256(
    stableJson({ body, version: version ?? null }),
  );
  return serializeRegistry(
    () =>
      readRegistry().find(
        (item) =>
          item.scopeDigest === scopeDigest &&
          item.requestDigest === requestDigest,
      )?.idempotencyKey ?? null,
  );
}

/** Завершает intent только после отдельного авторитетного readback. */
export async function completeMutationIntent(
  scope: string,
  body: unknown,
  version?: number,
): Promise<void> {
  const scopeDigest = await sha256(scope);
  const requestDigest = await sha256(
    stableJson({ body, version: version ?? null }),
  );
  await serializeRegistry(() => {
    writeRegistry(
      readRegistry().filter(
        (item) =>
          item.scopeDigest !== scopeDigest ||
          item.requestDigest !== requestDigest,
      ),
    );
  });
}
