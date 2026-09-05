import { requestSignal } from "@/shared/api/client";
import {
  getRuntimeEnvironmentImpact,
  rebindRuntimeEnvironment,
  getRuntimeSecretImpact,
  rebindRuntimeSecret,
} from "@/shared/api/generated/openapi/sdk.gen";
import type {
  RuntimeEnvironmentConsumer,
  RuntimeEnvironmentImpact,
  RuntimeEnvironmentRebindResult,
  RuntimeSecretImpact,
  RuntimeSecretRebindSelection,
  RuntimeSecretRebindResult,
} from "@/shared/api/generated/openapi/types.gen";
import { mutate, type MutationHeaders } from "@/shared/api/mutation";
import { unwrap } from "@/shared/api/problem";
const positive = (value: number) => Number.isSafeInteger(value) && value > 0;
function headers(value: MutationHeaders) {
  if (!value["If-Match"])
    throw new Error("Revision impact version is required");
  return {
    "If-Match": value["If-Match"],
    "Idempotency-Key": value["Idempotency-Key"],
    "X-CSRF-Token": value["X-CSRF-Token"],
  };
}
export function consumerKey(value: RuntimeEnvironmentConsumer): string {
  return JSON.stringify([
    value.agentRef,
    value.agentVersion,
    value.bindingRef,
    value.bindingVersion,
    value.versionRef,
    value.projectRef,
  ]);
}
function validConsumer(
  value: RuntimeEnvironmentConsumer | null | undefined,
): boolean {
  return (
    !!value &&
    !!value.agentRef &&
    positive(value.agentVersion) &&
    !!value.bindingRef &&
    positive(value.bindingVersion) &&
    !!value.versionRef &&
    !!value.projectRef
  );
}
function validPage(
  total: number,
  cursor: string,
  previous: string | undefined,
  length: number,
): boolean {
  return (
    Number.isSafeInteger(total) &&
    total >= length &&
    typeof cursor === "string" &&
    cursor.length <= 512 &&
    (!cursor || cursor !== previous)
  );
}
export async function readEnvironmentImpact(
  environmentRef: string,
  versionRef: string,
  pageToken: string | undefined,
  signal: AbortSignal,
  query = "",
): Promise<RuntimeEnvironmentImpact> {
  const result = (
    await unwrap(
      getRuntimeEnvironmentImpact({
        path: { environmentRef, versionRef },
        query: {
          pageSize: 40,
          ...(pageToken ? { pageToken } : {}),
          ...(query.trim() ? { query: query.trim() } : {}),
        },
        signal: requestSignal(signal),
      }),
    )
  ).data;
  if (
    result.environmentRef !== environmentRef ||
    result.targetVersionRef !== versionRef ||
    !positive(result.environmentVersion) ||
    !result.targetDigest ||
    !Array.isArray(result.consumers) ||
    result.consumers.some((item) => !validConsumer(item)) ||
    new Set(result.consumers.map((item) => item.agentRef)).size !==
      result.consumers.length ||
    !validPage(
      result.total,
      result.nextPageToken,
      pageToken,
      result.consumers.length,
    )
  )
    throw new Error("Invalid runtime environment impact");
  return result;
}
export async function applyEnvironmentRebind(
  impact: RuntimeEnvironmentImpact,
  consumers: RuntimeEnvironmentConsumer[],
): Promise<RuntimeEnvironmentRebindResult> {
  const eligible = new Set(impact.consumers.map(consumerKey));
  if (
    !consumers.length ||
    consumers.length > 100 ||
    new Set(consumers.map((item) => item.agentRef)).size !== consumers.length ||
    consumers.some((item) => !eligible.has(consumerKey(item)))
  )
    throw new Error("Invalid environment rebind selection");
  const result = (
    await mutate(
      (value) =>
        rebindRuntimeEnvironment({
          path: {
            environmentRef: impact.environmentRef,
            versionRef: impact.targetVersionRef,
          },
          body: { consumers },
          headers: headers(value),
          signal: requestSignal(),
        }),
      impact.environmentVersion,
    )
  ).data;
  if (
    !Array.isArray(result.bindings) ||
    result.bindings.length !== consumers.length ||
    new Set(result.bindings.map((item) => item.agentRef)).size !==
      consumers.length ||
    result.bindings.some((binding) => {
      const old = consumers.find((item) => item.agentRef === binding.agentRef);
      return (
        !old ||
        binding.ref !== old.bindingRef ||
        !positive(binding.version) ||
        binding.version <= old.bindingVersion ||
        binding.environmentRef !== impact.environmentRef ||
        binding.versionRef !== impact.targetVersionRef ||
        binding.digest !== impact.targetDigest
      );
    })
  )
    throw new Error("Environment rebind receipt mismatch");
  return result;
}
export async function readSecretImpact(
  secretRef: string,
  revision: number,
  pageToken: string | undefined,
  signal: AbortSignal,
  query = "",
): Promise<RuntimeSecretImpact> {
  const result = (
    await unwrap(
      getRuntimeSecretImpact({
        path: { secretRef, revision },
        query: {
          pageSize: 40,
          ...(pageToken ? { pageToken } : {}),
          ...(query.trim() ? { query: query.trim() } : {}),
        },
        signal: requestSignal(signal),
      }),
    )
  ).data;
  if (
    result.secretRef !== secretRef ||
    result.targetRevision !== revision ||
    !positive(result.secretVersion) ||
    !Array.isArray(result.consumers) ||
    !validPage(
      result.total,
      result.nextPageToken,
      pageToken,
      result.consumers.length,
    ) ||
    result.consumers.some(
      (item) =>
        !item.environmentRef ||
        !positive(item.environmentVersion) ||
        !item.environmentVersionRef ||
        !item.projectRef ||
        !Array.isArray(item.secretRevisions) ||
        item.secretRevisions.some((value) => !positive(value)) ||
        (item.consumer &&
          (!validConsumer(item.consumer) ||
            item.consumer.versionRef !== item.environmentVersionRef ||
            item.consumer.projectRef !== item.projectRef)),
    )
  )
    throw new Error("Invalid runtime secret impact");
  return result;
}
export async function applySecretRebind(
  impact: RuntimeSecretImpact,
  selections: RuntimeSecretRebindSelection[],
): Promise<RuntimeSecretRebindResult> {
  const consumers = selections.flatMap((item) => item.consumers);
  if (
    !selections.length ||
    selections.length > 32 ||
    consumers.length > 100 ||
    new Set(consumers.map((item) => item.agentRef)).size !== consumers.length ||
    new Set(selections.map((item) => item.environmentRef)).size !==
      selections.length
  )
    throw new Error("Invalid secret rebind selection");
  for (const selection of selections) {
    const eligible = impact.consumers.filter(
      (item) =>
        item.environmentRef === selection.environmentRef &&
        item.environmentVersion === selection.expectedEnvironmentVersion &&
        item.environmentVersionRef === selection.sourceVersionRef,
    );
    if (
      !eligible.length ||
      selection.consumers.some(
        (consumer) =>
          !eligible.some(
            (item) =>
              item.consumer &&
              consumerKey(item.consumer) === consumerKey(consumer),
          ),
      )
    )
      throw new Error("Secret rebind selection is outside impact snapshot");
  }
  const result = (
    await mutate(
      (value) =>
        rebindRuntimeSecret({
          path: {
            secretRef: impact.secretRef,
            revision: impact.targetRevision,
          },
          body: { selections },
          headers: headers(value),
          signal: requestSignal(),
        }),
      impact.secretVersion,
    )
  ).data;
  if (
    !Array.isArray(result.environments) ||
    result.environments.length !== selections.length ||
    new Set(result.environments.map((item) => item.environmentRef)).size !==
      selections.length ||
    !Array.isArray(result.bindings) ||
    result.bindings.length !== consumers.length ||
    new Set(result.bindings.map((item) => item.agentRef)).size !==
      consumers.length
  )
    throw new Error("Incomplete secret rebind receipt");
  for (const environment of result.environments) {
    const selection = selections.find(
      (item) => item.environmentRef === environment.environmentRef,
    );
    const source = impact.consumers.find(
      (item) =>
        item.environmentRef === environment.environmentRef &&
        item.environmentVersionRef === selection?.sourceVersionRef,
    );
    if (
      !selection ||
      !source ||
      environment.projectRef !== source.projectRef ||
      !positive(environment.environmentVersion) ||
      environment.environmentVersion <= selection.expectedEnvironmentVersion ||
      !environment.versionRef ||
      !environment.digest
    )
      throw new Error("Secret environment receipt mismatch");
  }
  for (const binding of result.bindings) {
    const selection = selections.find((item) =>
      item.consumers.some((consumer) => consumer.agentRef === binding.agentRef),
    );
    const old = selection?.consumers.find(
      (item) => item.agentRef === binding.agentRef,
    );
    const environment = result.environments.find(
      (item) => item.environmentRef === selection?.environmentRef,
    );
    if (
      !old ||
      !environment ||
      binding.ref !== old.bindingRef ||
      !positive(binding.version) ||
      binding.version <= old.bindingVersion ||
      binding.environmentRef !== environment.environmentRef ||
      binding.versionRef !== environment.versionRef ||
      binding.digest !== environment.digest
    )
      throw new Error("Secret binding receipt mismatch");
  }
  return result;
}
