import {
  getIntegrationConnection,
  setIntegrationCredentialPolicy,
} from "@/shared/api/generated/openapi/sdk.gen";
import type { IntegrationConnection } from "@/shared/api/generated/openapi/types.gen";
import { requestSignal } from "@/shared/api/client";
import { etag, mutate } from "@/shared/api/mutation";
import { unwrap } from "@/shared/api/problem";

export type IntegrationCredentialPolicy =
  IntegrationConnection["credentialPolicy"];

function checkedConnection(
  value: IntegrationConnection | undefined,
  ref: string,
): IntegrationConnection {
  if (
    !value ||
    value.ref !== ref ||
    !Number.isSafeInteger(value.version) ||
    value.version < 1 ||
    !["PROJECT_SHARED", "INITIATOR_REQUIRED"].includes(
      value.credentialPolicy,
    ) ||
    !value.definitionKey ||
    !value.definitionVersion ||
    !/^[a-f0-9]{64}$/.test(value.definitionDigest) ||
    !Array.isArray(value.nextActions) ||
    !Array.isArray(value.grants)
  )
    throw new Error("Integration credential policy response is invalid");
  return value;
}

export async function readCredentialPolicy(ref: string, signal: AbortSignal) {
  const result = await unwrap(
    getIntegrationConnection({
      path: { connectionRef: ref },
      signal: requestSignal(signal),
    }),
  );
  return checkedConnection(result.data, ref);
}

// Отдельная owner command: ровно одна попытка, без configure общего ключа.
export async function saveCredentialPolicy(
  current: IntegrationConnection,
  policy: IntegrationCredentialPolicy,
  signal: AbortSignal,
) {
  if (
    !["PROJECT_SHARED", "INITIATOR_REQUIRED"].includes(policy) ||
    current.credentialPolicy === policy ||
    current.state !== "DISABLED" ||
    !current.nextActions.includes("EDIT") ||
    current.grants.some((grant) => grant.enabled)
  )
    throw new Error("Integration credential policy change is unavailable");
  const response = await mutate(
    (headers) =>
      setIntegrationCredentialPolicy({
        path: { connectionRef: current.ref },
        body: { policy },
        headers: { ...headers, "If-Match": etag(current.version) },
        signal: requestSignal(signal),
      }),
    current.version,
  );
  const value = checkedConnection(response.data, current.ref);
  if (
    value.credentialPolicy !== policy ||
    value.version !== current.version + 1 ||
    value.definitionKey !== current.definitionKey ||
    value.definitionVersion !== current.definitionVersion ||
    value.definitionDigest !== current.definitionDigest ||
    value.state !== "DISABLED"
  )
    throw new Error("Integration credential policy receipt is invalid");
  return value;
}
