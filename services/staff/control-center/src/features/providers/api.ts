import { requestSignal } from "@/shared/api/client";
import {
  authorizeProviderAccountApiKey,
  createProviderAccount as createProviderAccountRequest,
  deleteProviderAccount,
  getProviderAccount,
  listProviderAccounts,
  listProviderDefinitions,
  reauthorizeProviderAccountDeviceCode,
  revokeProviderAccount as revokeProviderAccountRequest,
  setProviderAccountEnabled as setProviderAccountEnabledRequest,
  startProviderAccountDeviceAuthorization,
  verifyProviderAccountDeviceAuthorization,
} from "@/shared/api/generated/openapi/sdk.gen";
import { mutate, type MutationHeaders } from "@/shared/api/mutation";
import { unwrap } from "@/shared/api/problem";
import { checkMutationRejection } from "@/shared/api/mutation-rejection";
import type { ProviderAccountUsageContext } from "@/shared/api/generated/openapi/types.gen";
import { usageQuery } from "./usage";

import type {
  ProviderAccount,
  ProviderAccountCreateInput,
  ProviderAccountPage,
  ProviderDefinitionKey,
  ProviderDefinitionPage,
} from "./model";

function mutationHeaders(headers: MutationHeaders): {
  "Idempotency-Key": string;
  "X-CSRF-Token": string;
} {
  return {
    "Idempotency-Key": headers["Idempotency-Key"],
    "X-CSRF-Token": headers["X-CSRF-Token"],
  };
}

function versionedHeaders(headers: MutationHeaders): {
  "Idempotency-Key": string;
  "If-Match": string;
  "X-CSRF-Token": string;
} {
  if (!headers["If-Match"])
    throw new Error("Provider account version header is unavailable");
  return { ...mutationHeaders(headers), "If-Match": headers["If-Match"] };
}

export async function loadProviderDefinitions(
  query: string,
  pageToken?: string,
  signal: AbortSignal = requestSignal(),
): Promise<ProviderDefinitionPage> {
  return (
    await unwrap(
      listProviderDefinitions({
        query: {
          pageSize: 50,
          ...(query.trim() ? { query: query.trim() } : {}),
          ...(pageToken ? { pageToken } : {}),
        },
        signal,
      }),
    )
  ).data;
}

export async function loadProviderAccounts(
  query: string,
  pageToken?: string,
  signal: AbortSignal = requestSignal(),
  definitionKey?: ProviderDefinitionKey,
  usageContext?: ProviderAccountUsageContext,
): Promise<ProviderAccountPage> {
  return (
    await unwrap(
      listProviderAccounts({
        query: {
          pageSize: 40,
          ...usageQuery(usageContext),
          ...(definitionKey ? { definitionKey } : {}),
          ...(query.trim() ? { query: query.trim() } : {}),
          ...(pageToken ? { pageToken } : {}),
        },
        signal,
      }),
    )
  ).data;
}

export async function loadProviderAccount(
  providerAccountRef: string,
  signal: AbortSignal = requestSignal(),
  usageContext?: ProviderAccountUsageContext,
): Promise<ProviderAccount> {
  return (
    await unwrap(
      getProviderAccount({
        path: { providerAccountRef },
        query: usageQuery(usageContext),
        signal,
      }),
    )
  ).data;
}

export async function createProviderAccount(
  input: ProviderAccountCreateInput,
): Promise<ProviderAccount> {
  return (
    await mutate((headers) =>
      createProviderAccountRequest({
        body: input,
        headers: mutationHeaders(headers),
        signal: requestSignal(),
      }),
    )
  ).data;
}

export async function startDeviceAuthorization(
  account: ProviderAccount,
): Promise<ProviderAccount> {
  return (
    await mutate(
      (headers) =>
        startProviderAccountDeviceAuthorization({
          path: { providerAccountRef: account.ref },
          headers: versionedHeaders(headers),
          signal: requestSignal(),
        }),
      account.version,
    )
  ).data;
}

export async function verifyDeviceAuthorization(
  account: ProviderAccount,
  key?: string,
): Promise<ProviderAccount> {
  return (
    await mutate(
      (headers) =>
        verifyProviderAccountDeviceAuthorization({
          path: { providerAccountRef: account.ref },
          headers: versionedHeaders(headers),
          signal: requestSignal(),
        }).then(checkMutationRejection),
      account.version,
      key,
    )
  ).data;
}

export async function reauthorizeProviderDevice(
  account: ProviderAccount,
  key?: string,
): Promise<ProviderAccount> {
  return (
    await mutate(
      (headers) =>
        reauthorizeProviderAccountDeviceCode({
          path: { providerAccountRef: account.ref },
          headers: versionedHeaders(headers),
          signal: requestSignal(),
        }).then(checkMutationRejection),
      account.version,
      key,
    )
  ).data;
}

export async function authorizeProviderApiKey(
  account: ProviderAccount,
  apiKey: string,
): Promise<ProviderAccount> {
  return (
    await mutate(
      (headers) =>
        authorizeProviderAccountApiKey({
          path: { providerAccountRef: account.ref },
          body: { apiKey },
          headers: versionedHeaders(headers),
          signal: requestSignal(),
        }),
      account.version,
    )
  ).data;
}

export async function revokeProviderAccount(
  account: ProviderAccount,
): Promise<ProviderAccount> {
  return (
    await mutate(
      (headers) =>
        revokeProviderAccountRequest({
          path: { providerAccountRef: account.ref },
          headers: versionedHeaders(headers),
          signal: requestSignal(),
        }),
      account.version,
    )
  ).data;
}

export async function deleteProviderAccountRecord(
  account: ProviderAccount,
  key?: string,
): Promise<ProviderAccount> {
  return (
    await mutate(
      (headers) =>
        deleteProviderAccount({
          path: { providerAccountRef: account.ref },
          headers: versionedHeaders(headers),
          signal: requestSignal(),
        }).then(checkMutationRejection),
      account.version,
      key,
    )
  ).data;
}

export async function setProviderAccountEnabled(
  account: ProviderAccount,
  enabled: boolean,
): Promise<ProviderAccount> {
  return (
    await mutate(
      (headers) =>
        setProviderAccountEnabledRequest({
          path: { providerAccountRef: account.ref },
          body: { enabled },
          headers: versionedHeaders(headers),
          signal: requestSignal(),
        }),
      account.version,
    )
  ).data;
}
