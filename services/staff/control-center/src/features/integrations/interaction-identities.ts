import { requestSignal } from "@/shared/api/client";
import {
  bindInteractionIdentity,
  listInteractionIdentities,
  revokeInteractionIdentity,
} from "@/shared/api/generated/openapi/sdk.gen";
import type {
  IntegrationConnection,
  InteractionIdentity,
  InteractionIdentityBindInput,
  InteractionIdentityPage,
} from "@/shared/api/generated/openapi/types.gen";
import { mutate, type MutationHeaders } from "@/shared/api/mutation";
import { unwrap } from "@/shared/api/problem";

function headers(value: MutationHeaders) {
  if (!value["If-Match"])
    throw new Error("Interaction identity version is required");
  return {
    "If-Match": value["If-Match"],
    "Idempotency-Key": value["Idempotency-Key"],
    "X-CSRF-Token": value["X-CSRF-Token"],
  };
}
export function validIdentityInput(
  value: InteractionIdentityBindInput,
): boolean {
  return (
    typeof value.subjectRef === "string" &&
    !!value.subjectRef &&
    typeof value.externalTeamRef === "string" &&
    value.externalTeamRef.length > 0 &&
    value.externalTeamRef.length <= 128 &&
    typeof value.externalChannelRef === "string" &&
    value.externalChannelRef.length > 0 &&
    value.externalChannelRef.length <= 128 &&
    typeof value.externalUserDigest === "string" &&
    /^[a-f0-9]{64}$/.test(value.externalUserDigest)
  );
}
function identity(
  value: InteractionIdentity | null | undefined,
  connectionRef: string,
): InteractionIdentity {
  if (
    !value ||
    value.connectionRef !== connectionRef ||
    !value.ref ||
    !Number.isSafeInteger(value.version) ||
    value.version < 1 ||
    !Number.isSafeInteger(value.connectionVersion) ||
    value.connectionVersion < 1 ||
    !validIdentityInput(value) ||
    !["ACTIVE", "REVOKED"].includes(value.state)
  )
    throw new Error("Invalid interaction identity receipt");
  return {
    ref: value.ref,
    version: value.version,
    connectionRef: value.connectionRef,
    connectionVersion: value.connectionVersion,
    externalTeamRef: value.externalTeamRef,
    externalChannelRef: value.externalChannelRef,
    externalUserDigest: value.externalUserDigest,
    subjectRef: value.subjectRef,
    state: value.state,
  };
}
export async function readInteractionIdentities(
  connectionRef: string,
  pageToken: string | undefined,
  signal: AbortSignal,
): Promise<InteractionIdentityPage> {
  const page = (
    await unwrap(
      listInteractionIdentities({
        path: { connectionRef },
        query: { pageSize: 40, ...(pageToken ? { pageToken } : {}) },
        signal: requestSignal(signal),
      }),
    )
  ).data;
  if (
    !Array.isArray(page.items) ||
    typeof page.nextPageToken !== "string" ||
    (page.nextPageToken && page.nextPageToken === pageToken)
  )
    throw new Error("Invalid interaction identity page");
  const items = page.items.map((item) => identity(item, connectionRef));
  if (new Set(items.map((item) => item.ref)).size !== items.length)
    throw new Error("Duplicate interaction identities");
  return { items, nextPageToken: page.nextPageToken };
}
export async function createInteractionIdentity(
  connection: IntegrationConnection,
  input: InteractionIdentityBindInput,
): Promise<InteractionIdentity> {
  if (!validIdentityInput(input))
    throw new Error("Invalid interaction identity input");
  const result = identity(
    (
      await mutate(
        (value) =>
          bindInteractionIdentity({
            path: { connectionRef: connection.ref },
            headers: headers(value),
            body: {
              externalTeamRef: input.externalTeamRef,
              externalChannelRef: input.externalChannelRef,
              externalUserDigest: input.externalUserDigest,
              subjectRef: input.subjectRef,
            },
            signal: requestSignal(),
          }),
        connection.version,
      )
    ).data,
    connection.ref,
  );
  if (
    result.connectionVersion !== connection.version ||
    result.state !== "ACTIVE" ||
    result.subjectRef !== input.subjectRef ||
    result.externalTeamRef !== input.externalTeamRef ||
    result.externalChannelRef !== input.externalChannelRef ||
    result.externalUserDigest !== input.externalUserDigest
  )
    throw new Error("Interaction identity binding mismatch");
  return result;
}
export async function removeInteractionIdentity(
  current: InteractionIdentity,
): Promise<InteractionIdentity> {
  const result = identity(
    (
      await mutate(
        (value) =>
          revokeInteractionIdentity({
            path: { identityRef: current.ref },
            headers: headers(value),
            signal: requestSignal(),
          }),
        current.version,
      )
    ).data,
    current.connectionRef,
  );
  if (
    result.ref !== current.ref ||
    result.version <= current.version ||
    result.state !== "REVOKED" ||
    result.connectionVersion !== current.connectionVersion ||
    result.subjectRef !== current.subjectRef ||
    result.externalUserDigest !== current.externalUserDigest ||
    result.externalTeamRef !== current.externalTeamRef ||
    result.externalChannelRef !== current.externalChannelRef
  )
    throw new Error("Interaction identity revocation mismatch");
  return result;
}
