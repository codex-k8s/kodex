import * as sdk from "@/shared/api/generated/openapi/sdk.gen";
import type {
  ManagedConfiguration,
  RoleImageGitSourceInput,
} from "@/shared/api/generated/openapi/types.gen";
import { requestSignal } from "@/shared/api/client";
import { etag, idempotencyKey, mutateWithRetry } from "@/shared/api/mutation";
import { unwrap } from "@/shared/api/problem";

export const gitSourceRecoveryKey = "kodex.configuration.git-source-attempts";
export interface GitSourceAttempt {
  configurationRef: string;
  version: number;
  kind: "ROLE_IMAGE" | "INTEGRATION_DEFINITION";
  action: "CONFIGURE" | "REFRESH";
  key: string;
  input?: RoleImageGitSourceInput;
}
type RecoveryStorage = Pick<Storage, "getItem" | "setItem" | "removeItem">;
function validRef(value: unknown): value is string {
  return typeof value === "string" && /^[A-Za-z0-9_-]{1,128}$/.test(value);
}
function validText(value: unknown, max: number): value is string {
  return (
    typeof value === "string" &&
    value.length > 0 &&
    new TextEncoder().encode(value).length <= max &&
    !value.includes("\0") &&
    !/[\r\n\\]/.test(value)
  );
}
function validInput(value: unknown): value is RoleImageGitSourceInput {
  if (!value || typeof value !== "object") return false;
  const input = value as Partial<RoleImageGitSourceInput>;
  return (
    Object.keys(input).sort().join(",") ===
      "connectionRef,contentFormat,expectedConnectionVersion,path,refName,repositoryRef" &&
    validRef(input.connectionRef) &&
    Number.isSafeInteger(input.expectedConnectionVersion) &&
    (input.expectedConnectionVersion ?? 0) > 0 &&
    validText(input.repositoryRef, 256) &&
    validText(input.refName, 256) &&
    validText(input.path, 512) &&
    ["JSON", "YAML"].includes(input.contentFormat ?? "")
  );
}
function validAttempt(value: unknown): value is GitSourceAttempt {
  if (!value || typeof value !== "object") return false;
  const attempt = value as Partial<GitSourceAttempt>;
  const keys =
    attempt.action === "CONFIGURE"
      ? "action,configurationRef,input,key,kind,version"
      : "action,configurationRef,key,kind,version";
  return (
    Object.keys(attempt).sort().join(",") === keys &&
    validRef(attempt.configurationRef) &&
    Number.isSafeInteger(attempt.version) &&
    (attempt.version ?? 0) > 0 &&
    ["ROLE_IMAGE", "INTEGRATION_DEFINITION"].includes(attempt.kind ?? "") &&
    typeof attempt.key === "string" &&
    /^[0-9a-f-]{36}$/.test(attempt.key) &&
    (attempt.action === "CONFIGURE"
      ? validInput(attempt.input)
      : attempt.action === "REFRESH")
  );
}
function attempts(storage: RecoveryStorage): GitSourceAttempt[] {
  const raw = storage.getItem(gitSourceRecoveryKey);
  const result: unknown = raw ? JSON.parse(raw) : [];
  if (
    !Array.isArray(result) ||
    result.length > 20 ||
    !result.every(validAttempt) ||
    new Set(result.map((item) => item.configurationRef)).size !== result.length
  )
    throw new Error("Invalid Git source recovery metadata");
  return result;
}
export function pendingGitSource(
  ref: string,
  storage: RecoveryStorage,
): GitSourceAttempt | undefined {
  return attempts(storage).find((item) => item.configurationRef === ref);
}
export function rememberGitSource(
  attempt: GitSourceAttempt,
  storage: RecoveryStorage,
): void {
  if (!validAttempt(attempt))
    throw new Error("Invalid Git source mutation intent");
  const current = attempts(storage);
  const previous = current.find(
    (item) => item.configurationRef === attempt.configurationRef,
  );
  if (previous && JSON.stringify(previous) !== JSON.stringify(attempt))
    throw new Error("Git source mutation intent changed");
  if (!previous) {
    if (current.length >= 20)
      throw new Error("Git source recovery limit reached");
    current.push(attempt);
    storage.setItem(gitSourceRecoveryKey, JSON.stringify(current));
  }
}
export function forgetGitSource(
  attempt: GitSourceAttempt,
  storage: RecoveryStorage,
): void {
  const current = attempts(storage).filter(
    (item) =>
      item.configurationRef !== attempt.configurationRef ||
      item.key !== attempt.key,
  );
  if (current.length)
    storage.setItem(gitSourceRecoveryKey, JSON.stringify(current));
  else storage.removeItem(gitSourceRecoveryKey);
}
export function prepareGitSource(
  configuration: ManagedConfiguration,
  input?: RoleImageGitSourceInput,
): GitSourceAttempt {
  if (
    configuration.kind !== "ROLE_IMAGE" &&
    configuration.kind !== "INTEGRATION_DEFINITION"
  )
    throw new Error("Unsupported Git source configuration kind");
  const attempt: GitSourceAttempt = {
    configurationRef: configuration.ref,
    version: configuration.version,
    kind: configuration.kind,
    action: input ? "CONFIGURE" : "REFRESH",
    key: idempotencyKey(),
    ...(input ? { input: structuredClone(input) } : {}),
  };
  if (!validAttempt(attempt))
    throw new Error("Invalid Git source mutation intent");
  return attempt;
}
export async function executeGitSource(
  attempt: GitSourceAttempt,
  signal: AbortSignal,
) {
  signal.throwIfAborted();
  if (!validAttempt(attempt))
    throw new Error("Invalid Git source mutation intent");
  const data = (
    await mutateWithRetry(
      (headers) => {
        const options = {
          path: { configurationRef: attempt.configurationRef },
          headers: { ...headers, "If-Match": etag(attempt.version) },
          signal: requestSignal(signal),
        };
        if (attempt.action === "REFRESH")
          return attempt.kind === "ROLE_IMAGE"
            ? sdk.refreshRoleImageGitSource(options)
            : sdk.refreshIntegrationDefinitionGitSource(options);
        if (!attempt.input) throw new Error("Git source input is missing");
        return attempt.kind === "ROLE_IMAGE"
          ? sdk.configureRoleImageGitSource({ ...options, body: attempt.input })
          : sdk.configureIntegrationDefinitionGitSource({
              ...options,
              body: attempt.input,
            });
      },
      attempt.version,
      attempt.key,
    )
  ).data;
  signal.throwIfAborted();
  if (
    data.ref !== attempt.configurationRef ||
    data.kind !== attempt.kind ||
    data.version <= attempt.version ||
    data.managedBy !== "GIT" ||
    data.gitSource?.state !== "QUEUED"
  )
    throw new Error("Git source receipt mismatch");
  return data;
}
export async function gitSourceConnections(
  query: string,
  pageToken: string | undefined,
  signal: AbortSignal,
) {
  return (
    await unwrap(
      sdk.listIntegrationConnections({
        query: { query, pageToken, pageSize: 30 },
        signal: requestSignal(signal),
        cache: "no-store",
      }),
    )
  ).data;
}
export async function gitSourceConnection(
  connectionRef: string,
  signal: AbortSignal,
) {
  const result = (
    await unwrap(
      sdk.getIntegrationConnection({
        path: { connectionRef },
        signal: requestSignal(signal),
        cache: "no-store",
      }),
    )
  ).data;
  if (
    result.ref !== connectionRef ||
    !Number.isSafeInteger(result.version) ||
    result.version < 1
  )
    throw new Error("Git source connection readback mismatch");
  return result;
}
