import { runtimeEnvironmentPolicyPath } from "@/features/session/reauth";
import type {
  RuntimeEnvironmentInput,
  RuntimeEnvironmentPolicyInput,
  RuntimeEnvironmentTool,
  RuntimeEnvironmentValue,
  RuntimeSecretBinding,
  RuntimeVolumeInput,
} from "@/shared/api/generated/openapi/types.gen";
import type { AppProblem } from "@/shared/api/problem";

import {
  normalizeRuntimeEnvironmentInput,
  validateEnvironmentInput,
} from "./environment-form";

const opaqueReferencePattern = /^[A-Za-z0-9_-]{8,128}$/;
const draftLifetimeMs = 5 * 60 * 1000;
const allowedFutureSkewMs = 30 * 1000;

export const runtimeEnvironmentPolicyDraftStorageKey =
  "kodex.runtime-environment-policy.reauth-draft";
export const freshAuthenticationRequiredCode = "FRESH_AUTHENTICATION_REQUIRED";

export type RuntimeEnvironmentPolicyDraftOperation = "CREATE" | "PUBLISH";

export function requiresRuntimeEnvironmentPolicyReauth(
  problem: Pick<AppProblem, "code" | "status">,
): boolean {
  return (
    problem.status === 403 && problem.code === freshAuthenticationRequiredCode
  );
}

export interface RuntimeEnvironmentPolicyDraft {
  readonly environmentRef?: string;
  readonly expectedVersion: number | null;
  readonly input: RuntimeEnvironmentInput;
  readonly issuedAt: number;
  readonly operation: RuntimeEnvironmentPolicyDraftOperation;
  readonly projectRef: string;
  readonly returnPath: string;
  readonly version: 1;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function hasExactKeys(
  value: Record<string, unknown>,
  expected: readonly string[],
): boolean {
  const actual = Object.keys(value).sort();
  const sortedExpected = [...expected].sort();
  return (
    actual.length === sortedExpected.length &&
    sortedExpected.every((key, index) => actual[index] === key)
  );
}

function isStringRecord(
  value: unknown,
  keys: readonly string[],
): value is Record<string, string> {
  return (
    isRecord(value) &&
    hasExactKeys(value, keys) &&
    keys.every((key) => typeof value[key] === "string")
  );
}

function parseTools(value: unknown): RuntimeEnvironmentTool[] | undefined {
  if (!Array.isArray(value) || value.length > 128) return undefined;
  if (
    !value.every((item) =>
      isStringRecord(item, ["command", "description", "name", "usageHint"]),
    )
  )
    return undefined;
  return value as RuntimeEnvironmentTool[];
}

function parseValues(value: unknown): RuntimeEnvironmentValue[] | undefined {
  if (!Array.isArray(value) || value.length > 128) return undefined;
  if (!value.every((item) => isStringRecord(item, ["name", "value"])))
    return undefined;
  return value as RuntimeEnvironmentValue[];
}

function parseSecretBindings(
  value: unknown,
): RuntimeSecretBinding[] | undefined {
  if (!Array.isArray(value) || value.length > 128) return undefined;
  if (!value.every((item) => isStringRecord(item, ["name", "secretRef"])))
    return undefined;
  return value as RuntimeSecretBinding[];
}

function parseVolumes(value: unknown): RuntimeVolumeInput[] | undefined {
  if (!Array.isArray(value) || value.length > 16) return undefined;
  if (
    !value.every(
      (item) =>
        isRecord(item) &&
        hasExactKeys(item, ["kind", "name", "sizeMib"]) &&
        (item.kind === "EPHEMERAL_DISK" || item.kind === "EPHEMERAL_MEMORY") &&
        typeof item.name === "string" &&
        typeof item.sizeMib === "number" &&
        Number.isSafeInteger(item.sizeMib),
    )
  )
    return undefined;
  return value as RuntimeVolumeInput[];
}

function parsePolicy(
  value: unknown,
): RuntimeEnvironmentPolicyInput | undefined {
  if (
    !isRecord(value) ||
    !hasExactKeys(value, [
      "kubernetesAccess",
      "networkDestinations",
      "resources",
      "volumes",
    ]) ||
    (value.kubernetesAccess !== "NONE" &&
      value.kubernetesAccess !== "READ_OWN_EXECUTION") ||
    !Array.isArray(value.networkDestinations) ||
    !value.networkDestinations.every((item) =>
      ["DNS", "RUNTIME_CALLBACK", "PROVIDER_PROXY", "KUBERNETES_API"].includes(
        String(item),
      ),
    ) ||
    !isRecord(value.resources) ||
    !hasExactKeys(value.resources, [
      "cpuLimitMilli",
      "cpuRequestMilli",
      "ephemeralStorageLimitMib",
      "ephemeralStorageRequestMib",
      "memoryLimitMib",
      "memoryRequestMib",
    ]) ||
    !Object.values(value.resources).every(
      (item) => typeof item === "number" && Number.isSafeInteger(item),
    )
  )
    return undefined;
  const volumes = parseVolumes(value.volumes);
  if (!volumes) return undefined;
  return {
    resources: value.resources as RuntimeEnvironmentPolicyInput["resources"],
    volumes,
    networkDestinations:
      value.networkDestinations as RuntimeEnvironmentPolicyInput["networkDestinations"],
    kubernetesAccess: value.kubernetesAccess,
  };
}

function parseInput(value: unknown): RuntimeEnvironmentInput | undefined {
  if (
    !isRecord(value) ||
    !hasExactKeys(value, [
      "description",
      "imageArtifactRef",
      "name",
      "policy",
      "secretBindings",
      "tools",
      "values",
    ]) ||
    typeof value.name !== "string" ||
    typeof value.description !== "string" ||
    typeof value.imageArtifactRef !== "string"
  )
    return undefined;
  const tools = parseTools(value.tools);
  const values = parseValues(value.values);
  const secretBindings = parseSecretBindings(value.secretBindings);
  const policy = parsePolicy(value.policy);
  if (!tools || !values || !secretBindings || !policy) return undefined;
  const input: RuntimeEnvironmentInput = {
    name: value.name,
    description: value.description,
    imageArtifactRef: value.imageArtifactRef,
    tools,
    values,
    secretBindings,
    policy,
  };
  return validateEnvironmentInput(input).length === 0 ? input : undefined;
}

export function createRuntimeEnvironmentPolicyDraft(input: {
  readonly environmentRef?: string;
  readonly expectedVersion?: number;
  readonly form: RuntimeEnvironmentInput;
  readonly operation: RuntimeEnvironmentPolicyDraftOperation;
  readonly projectRef: string;
  readonly now?: number;
}): RuntimeEnvironmentPolicyDraft {
  const expectedVersion = input.expectedVersion ?? null;
  if (!opaqueReferencePattern.test(input.projectRef))
    throw new Error("Runtime environment draft project reference is invalid");
  if (
    (input.operation === "CREATE" &&
      (input.environmentRef !== undefined || expectedVersion !== null)) ||
    (input.operation === "PUBLISH" &&
      (input.environmentRef === undefined ||
        !opaqueReferencePattern.test(input.environmentRef) ||
        expectedVersion === null ||
        !Number.isSafeInteger(expectedVersion) ||
        expectedVersion < 1))
  )
    throw new Error("Runtime environment draft operation is invalid");
  const normalized = normalizeRuntimeEnvironmentInput(input.form);
  if (validateEnvironmentInput(normalized).length > 0)
    throw new Error("Runtime environment draft is invalid");
  return {
    ...(input.environmentRef ? { environmentRef: input.environmentRef } : {}),
    expectedVersion,
    input: normalized,
    issuedAt: input.now ?? Date.now(),
    operation: input.operation,
    projectRef: input.projectRef,
    returnPath: runtimeEnvironmentPolicyPath(
      input.projectRef,
      input.environmentRef,
    ),
    version: 1,
  };
}

export function storeRuntimeEnvironmentPolicyDraft(
  draft: RuntimeEnvironmentPolicyDraft,
  storage: Pick<Storage, "setItem">,
): void {
  storage.setItem(
    runtimeEnvironmentPolicyDraftStorageKey,
    JSON.stringify(draft),
  );
}

export function discardRuntimeEnvironmentPolicyDraft(
  storage: Pick<Storage, "removeItem">,
): void {
  storage.removeItem(runtimeEnvironmentPolicyDraftStorageKey);
}

export function consumeRuntimeEnvironmentPolicyDraft(
  storage: Pick<Storage, "getItem" | "removeItem">,
  expected: {
    readonly environmentRef?: string;
    readonly expectedVersion?: number;
    readonly operation: RuntimeEnvironmentPolicyDraftOperation;
    readonly projectRef: string;
  },
  now = Date.now(),
): RuntimeEnvironmentInput {
  const raw = storage.getItem(runtimeEnvironmentPolicyDraftStorageKey);
  storage.removeItem(runtimeEnvironmentPolicyDraftStorageKey);
  if (raw === null)
    throw new Error("Runtime environment re-auth draft is unavailable");

  let value: unknown;
  try {
    value = JSON.parse(raw);
  } catch {
    throw new Error("Runtime environment re-auth draft is invalid");
  }
  if (!isRecord(value))
    throw new Error("Runtime environment re-auth draft is invalid");
  const hasEnvironment = Object.hasOwn(value, "environmentRef");
  const expectedKeys = [
    ...(hasEnvironment ? ["environmentRef"] : []),
    "expectedVersion",
    "input",
    "issuedAt",
    "operation",
    "projectRef",
    "returnPath",
    "version",
  ] as const;
  const parsedInput = parseInput(value.input);
  if (
    !hasExactKeys(value, expectedKeys) ||
    value.version !== 1 ||
    typeof value.issuedAt !== "number" ||
    !Number.isSafeInteger(value.issuedAt) ||
    value.issuedAt > now + allowedFutureSkewMs ||
    now - value.issuedAt > draftLifetimeMs ||
    value.operation !== expected.operation ||
    value.projectRef !== expected.projectRef ||
    value.environmentRef !== expected.environmentRef ||
    value.expectedVersion !== (expected.expectedVersion ?? null) ||
    value.returnPath !==
      runtimeEnvironmentPolicyPath(
        expected.projectRef,
        expected.environmentRef,
      ) ||
    !parsedInput
  )
    throw new Error("Runtime environment re-auth draft does not match editor");
  return parsedInput;
}
