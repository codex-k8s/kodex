import { getRuntimeRevisionDiff } from "@/shared/api/generated/openapi/sdk.gen";
import type {
  PublicRuntimeRevisionIdentity,
  RuntimeRevisionDiff,
  RuntimeRevisionDiffChange,
} from "@/shared/api/generated/openapi/types.gen";
import { requestSignal } from "@/shared/api/client";
import { unwrap } from "@/shared/api/problem";

const components: readonly RuntimeRevisionDiffChange["component"][] = [
  "PROVIDER",
  "MODEL",
  "RUNTIME_PROFILE",
  "RUNTIME_CONFIGURATION",
  "PROVIDER_POLICY",
  "CONFIG_OVERLAY",
  "ENVIRONMENT",
  "ENVIRONMENT_BINDING",
  "INSTRUCTION",
  "INTEGRATION_GRANTS",
  "IMAGE",
];
function present(value: unknown): boolean {
  return typeof value === "object" && value !== null;
}
function validIdentity(value: PublicRuntimeRevisionIdentity): boolean {
  return (
    typeof value.ref === "string" &&
    value.ref.length > 0 &&
    Number.isSafeInteger(value.version) &&
    value.version > 0 &&
    Number.isSafeInteger(value.attempt) &&
    value.attempt > 0 &&
    /^[a-f0-9]{64}$/.test(value.revisionDigest) &&
    Number.isFinite(Date.parse(value.createdAt))
  );
}

export async function loadRuntimeRevisionDiff(
  runRef: string,
  sessionRef: string,
  signal: AbortSignal,
): Promise<RuntimeRevisionDiff> {
  const result = (
    await unwrap(
      getRuntimeRevisionDiff({
        path: { runRef },
        signal: requestSignal(signal),
      }),
    )
  ).data;
  if (
    !present(result.current) ||
    !validIdentity(result.current) ||
    result.current.runRef !== runRef ||
    result.current.sessionRef !== sessionRef ||
    (result.previous &&
      (!validIdentity(result.previous) ||
        result.previous.sessionRef !== sessionRef ||
        result.previous.ref === result.current.ref)) ||
    !Array.isArray(result.changes) ||
    result.changes.length > components.length ||
    new Set(result.changes.map((change) => change.component)).size !==
      result.changes.length ||
    result.changes.some(
      (change) =>
        !components.includes(change.component) || !present(change.current),
    )
  )
    throw new Error("Invalid runtime revision diff boundary");
  return result;
}
