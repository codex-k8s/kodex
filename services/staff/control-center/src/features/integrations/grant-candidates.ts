import * as sdk from "@/shared/api/generated/openapi/sdk.gen";
import type {
  IntegrationGrantCandidateContext,
  IntegrationGrantCandidatePins,
  IntegrationGrantConnectionCandidatePage,
  IntegrationGrantProjectCandidatePage,
  IntegrationGrantRecipientCandidatePage,
  IntegrationGrantCapabilityCandidatePage,
  ListIntegrationGrantConnectionCandidatesData,
  ListIntegrationGrantProjectCandidatesData,
  ListIntegrationGrantRecipientCandidatesData,
  ListIntegrationGrantCapabilityCandidatesData,
} from "@/shared/api/generated/openapi/types.gen";
import { requestSignal } from "@/shared/api/client";
import { unwrap } from "@/shared/api/problem";

export interface IntegrationGrantSelection {
  connectionRef: string;
  connectionVersion: number;
  projectRef: string;
  recipientKind: "AGENT" | "WORKFLOW";
  recipientRef: string;
  capabilityKey: string;
}

type CandidatePage =
  | IntegrationGrantConnectionCandidatePage
  | IntegrationGrantProjectCandidatePage
  | IntegrationGrantRecipientCandidatePage
  | IntegrationGrantCapabilityCandidatePage;
const contextKeys = [
  "connectionRef",
  "projectRef",
  "recipientKind",
  "recipientRef",
  "capabilityKey",
  "workflowRef",
  "stepKey",
] as const;
const versionKeys = [
  "connectionVersion",
  "definitionVersion",
  "definitionDigest",
  "projectVersion",
  "recipientVersion",
  "workflowRevisionRef",
] as const;
const reasons = [
  "READY",
  "CONNECTION_UNAVAILABLE",
  "RECIPIENT_UNAVAILABLE",
  "PACKAGE_UNAVAILABLE",
  "GRANT_UNAVAILABLE",
  "WORKFLOW_EXCLUDED",
] as const;
const digest = (value: string) => /^[a-f0-9]{64}$/.test(value);
function contextIdentity(context: IntegrationGrantCandidateContext): string {
  return JSON.stringify(contextKeys.map((key) => context[key] ?? ""));
}
export function requireCandidatePrefix(
  pins: IntegrationGrantCandidatePins,
  expected?: IntegrationGrantCandidatePins,
): void {
  if (!digest(pins.contextDigest))
    throw new Error("Invalid integration candidate digest");
  for (const key of versionKeys) {
    const value = pins[key];
    if (
      typeof value === "number" &&
      (!Number.isSafeInteger(value) || value < 1)
    )
      throw new Error("Invalid integration candidate version");
    if (expected?.[key] !== undefined && value !== expected[key])
      throw new Error("Integration candidate prefix changed");
  }
}
export function checkedCandidatePage<T extends CandidatePage>(
  page: T,
  context: IntegrationGrantCandidateContext,
  purpose: "GRANT" | "USE",
  expected?: IntegrationGrantCandidatePins,
): T {
  if (
    contextIdentity(page.context) !== contextIdentity(context) ||
    !Number.isSafeInteger(page.total) ||
    page.total < page.items.length ||
    !digest(page.contextDigest) ||
    page.pins.contextDigest !== page.contextDigest
  )
    throw new Error("Invalid integration candidate page");
  requireCandidatePrefix(page.pins, expected);
  const refs = new Set<string>();
  for (const item of page.items) {
    requireCandidatePrefix(item.pins, page.pins);
    const ref =
      "connectionRef" in item
        ? item.connectionRef
        : "recipientRef" in item
          ? item.recipientRef
          : "capability" in item
            ? item.capability.key
            : item.projectRef;
    if (!ref || refs.has(ref) || !reasons.includes(item.reason))
      throw new Error("Invalid integration candidate item");
    refs.add(ref);
    if ("usable" in item) {
      if (
        item.grantable !== (purpose === "GRANT" && item.reason === "READY") ||
        item.usable !== (purpose === "USE" && item.reason === "READY")
      )
        throw new Error("Invalid integration candidate authority");
    } else if (item.grantable !== (item.reason === "READY"))
      throw new Error("Invalid integration candidate authority");
    if (
      "recipientRef" in item &&
      (item.projectRef !== context.projectRef ||
        item.recipientKind !== context.recipientKind)
    )
      throw new Error("Invalid integration candidate recipient");
  }
  return page;
}

function loader<T extends CandidatePage>(
  context: IntegrationGrantCandidateContext,
  purpose: "GRANT" | "USE",
  fetch: (
    query: string,
    cursor: string | undefined,
    signal: AbortSignal,
  ) => Promise<T>,
  expected?: IntegrationGrantCandidatePins,
) {
  let snapshot: string | undefined;
  let snapshotQuery = "";
  let lastCursor: string | undefined;
  return async (
    query: string,
    cursor: string | undefined,
    signal: AbortSignal,
  ): Promise<T> => {
    const page = checkedCandidatePage(
      await fetch(query, cursor, signal),
      context,
      purpose,
      expected,
    );
    signal.throwIfAborted();
    if (
      cursor &&
      (snapshotQuery !== query ||
        snapshot !== page.contextDigest ||
        lastCursor !== cursor)
    )
      throw new Error("Integration candidate snapshot changed");
    if (page.nextPageToken && page.nextPageToken === cursor)
      throw new Error("Integration candidate cursor repeated");
    snapshot = page.contextDigest;
    snapshotQuery = query;
    lastCursor = page.nextPageToken;
    return page;
  };
}
type ConnectionQuery = Omit<
  ListIntegrationGrantConnectionCandidatesData["query"],
  "query" | "pageSize" | "pageToken"
>;
export function connectionCandidates(context: ConnectionQuery) {
  const { purpose, ...scope } = context;
  return loader(
    scope,
    purpose,
    async (query, cursor, signal) =>
      (
        await unwrap(
          sdk.listIntegrationGrantConnectionCandidates({
            query: { ...context, query, pageToken: cursor, pageSize: 40 },
            signal: requestSignal(signal),
            cache: "no-store",
          }),
        )
      ).data,
  );
}
type ProjectQuery = Omit<
  ListIntegrationGrantProjectCandidatesData["query"],
  "query" | "pageSize" | "pageToken"
>;
export function projectCandidates(
  context: ProjectQuery,
  expected?: IntegrationGrantCandidatePins,
) {
  return loader(
    context,
    "GRANT",
    async (query, cursor, signal) =>
      (
        await unwrap(
          sdk.listIntegrationGrantProjectCandidates({
            query: { ...context, query, pageToken: cursor, pageSize: 40 },
            signal: requestSignal(signal),
            cache: "no-store",
          }),
        )
      ).data,
    expected,
  );
}
type RecipientQuery = Omit<
  ListIntegrationGrantRecipientCandidatesData["query"],
  "query" | "pageSize" | "pageToken"
>;
export function recipientCandidates(
  context: RecipientQuery,
  expected?: IntegrationGrantCandidatePins,
) {
  return loader(
    context,
    "GRANT",
    async (query, cursor, signal) =>
      (
        await unwrap(
          sdk.listIntegrationGrantRecipientCandidates({
            query: { ...context, query, pageToken: cursor, pageSize: 40 },
            signal: requestSignal(signal),
            cache: "no-store",
          }),
        )
      ).data,
    expected,
  );
}
type CapabilityQuery = Omit<
  ListIntegrationGrantCapabilityCandidatesData["query"],
  "query" | "pageSize" | "pageToken"
>;
export function capabilityCandidates(
  context: CapabilityQuery,
  expected?: IntegrationGrantCandidatePins,
) {
  return loader(
    context,
    "GRANT",
    async (query, cursor, signal) =>
      (
        await unwrap(
          sdk.listIntegrationGrantCapabilityCandidates({
            query: { ...context, query, pageToken: cursor, pageSize: 40 },
            signal: requestSignal(signal),
            cache: "no-store",
          }),
        )
      ).data,
    expected,
  );
}
