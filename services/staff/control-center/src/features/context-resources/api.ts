import * as sdk from "@/shared/api/generated/openapi/sdk.gen";
import type {
  ContextResourceState,
  SkillBundle,
  SkillBundleSpecification,
  SkillBundleRevision,
  KodexMemoryRecord,
  MemoryRecordSpecification,
  MemoryRecordRevision,
} from "@/shared/api/generated/openapi/types.gen";
import { requestSignal } from "@/shared/api/client";
import { mutate, type MutationHeaders } from "@/shared/api/mutation";
import { unwrap } from "@/shared/api/problem";
export type ContextKind = "skills" | "memory";
export type ContextItem = SkillBundle | KodexMemoryRecord;
export type ContextRevision = SkillBundleRevision | MemoryRecordRevision;
function versioned(headers: MutationHeaders) {
  if (!headers["If-Match"])
    throw new Error("Context resource version is required");
  return {
    "If-Match": headers["If-Match"],
    "Idempotency-Key": headers["Idempotency-Key"],
    "X-CSRF-Token": headers["X-CSRF-Token"],
  };
}
function checkItem<T extends ContextItem>(
  item: T,
  projectRef?: string,
  resourceRef?: string,
): T {
  if (
    !item.ref ||
    !Number.isSafeInteger(item.version) ||
    item.version < 1 ||
    !item.projectRef ||
    (projectRef && item.projectRef !== projectRef) ||
    (resourceRef && item.ref !== resourceRef)
  )
    throw new Error("Context resource scope mismatch");
  if (
    "currentRevision" in item &&
    item.currentRevision &&
    "redacted" in item.currentRevision
  ) {
    if (
      ((item.state === "EXPIRED" || item.state === "PURGED") &&
        !item.currentRevision.redacted) ||
      (item.currentRevision.redacted && item.currentRevision.summary !== "")
    )
      throw new Error("Invalid memory redaction");
  }
  return item;
}
export async function listContext(
  kind: ContextKind,
  options: {
    projectRef?: string;
    agentRef?: string;
    query: string;
    state: ContextResourceState;
    pageToken?: string;
    signal: AbortSignal;
  },
) {
  const { signal, ...query } = options;
  const request = {
    query: { ...query, pageSize: 40 },
    signal: requestSignal(signal),
  };
  const page =
    kind === "skills"
      ? (await unwrap(sdk.listSkillBundles(request))).data
      : (await unwrap(sdk.listMemoryRecords(request))).data;
  if (
    !Number.isSafeInteger(page.total) ||
    page.total < page.items.length ||
    page.nextPageToken.length > 512 ||
    (page.nextPageToken && page.nextPageToken === options.pageToken)
  )
    throw new Error("Invalid context resource page");
  return {
    ...page,
    items: page.items.map((item) => checkItem(item, options.projectRef)),
  };
}
export async function readSkill(
  bundleRef: string,
  signal: AbortSignal,
): Promise<SkillBundle> {
  return checkItem(
    (
      await unwrap(
        sdk.getSkillBundle({
          path: { bundleRef },
          signal: requestSignal(signal),
        }),
      )
    ).data,
    undefined,
    bundleRef,
  );
}
export async function readMemory(
  recordRef: string,
  signal: AbortSignal,
): Promise<KodexMemoryRecord> {
  return checkItem(
    (
      await unwrap(
        sdk.getMemoryRecord({
          path: { recordRef },
          signal: requestSignal(signal),
        }),
      )
    ).data,
    undefined,
    recordRef,
  );
}
export async function saveSkill(
  projectRef: string,
  specification: SkillBundleSpecification,
  bundle?: SkillBundle,
): Promise<SkillBundle> {
  const draft = bundle?.draftRevision;
  const existing = draft && !["PUBLISHED", "DISCARDED"].includes(draft.state);
  const result = existing
    ? await mutate(
        (headers) =>
          sdk.saveSkillBundleDraft({
            path: { bundleRef: bundle.ref, revisionRef: draft.ref },
            body: specification,
            headers: versioned(headers),
            signal: requestSignal(),
          }),
        bundle.version,
      )
    : await mutate(
        (headers) =>
          sdk.createSkillBundleDraft({
            path: { projectRef },
            body: {
              ...(bundle ? { bundleRef: bundle.ref } : {}),
              specification,
            },
            headers: { ...headers },
            signal: requestSignal(),
          }),
        bundle?.version,
      );
  return checkItem(result.data, projectRef, bundle?.ref);
}
export async function transitionSkill(
  bundle: SkillBundle,
  action: "validate" | "publish" | "discard",
  revision: SkillBundleRevision,
): Promise<SkillBundle> {
  const operations = {
    validate: sdk.validateSkillBundleDraft,
    publish: sdk.publishSkillBundleDraft,
    discard: sdk.discardSkillBundleDraft,
  };
  return checkItem(
    (
      await mutate(
        (headers) =>
          operations[action]({
            path: { bundleRef: bundle.ref, revisionRef: revision.ref },
            body: { expectedDigest: revision.digest },
            headers: versioned(headers),
            signal: requestSignal(),
          }),
        bundle.version,
      )
    ).data,
    bundle.projectRef,
    bundle.ref,
  );
}
export async function reviewSkill(
  bundle: SkillBundle,
  revision: SkillBundleRevision,
  decision: "APPROVE" | "REJECT",
  comment: string,
): Promise<SkillBundle> {
  return checkItem(
    (
      await mutate(
        (headers) =>
          sdk.reviewSkillBundleDraft({
            path: { bundleRef: bundle.ref, revisionRef: revision.ref },
            body: { expectedDigest: revision.digest, decision, comment },
            headers: versioned(headers),
            signal: requestSignal(),
          }),
        bundle.version,
      )
    ).data,
    bundle.projectRef,
    bundle.ref,
  );
}
export async function saveMemory(
  projectRef: string,
  specification: MemoryRecordSpecification,
  record?: KodexMemoryRecord,
  agentRef?: string,
): Promise<KodexMemoryRecord> {
  const result = record
    ? await mutate(
        (headers) =>
          sdk.reviseMemoryRecord({
            path: { recordRef: record.ref },
            body: specification,
            headers: versioned(headers),
            signal: requestSignal(),
          }),
        record.version,
      )
    : await mutate((headers) =>
        sdk.createMemoryRecord({
          path: { projectRef },
          body: { specification, ...(agentRef ? { agentRef } : {}) },
          headers: { ...headers },
          signal: requestSignal(),
        }),
      );
  return checkItem(result.data, projectRef, record?.ref);
}
export async function lifecycle(
  kind: ContextKind,
  item: ContextItem,
  action: "archive" | "restore" | "purge",
): Promise<ContextItem> {
  if (kind === "skills") {
    const operations = {
      archive: sdk.archiveSkillBundle,
      restore: sdk.restoreSkillBundle,
      purge: sdk.purgeSkillBundle,
    };
    return checkItem(
      (
        await mutate(
          (headers) =>
            operations[action]({
              path: { bundleRef: item.ref },
              headers: versioned(headers),
              signal: requestSignal(),
            }),
          item.version,
        )
      ).data,
      item.projectRef,
      item.ref,
    );
  }
  const operations = {
    archive: sdk.archiveMemoryRecord,
    restore: sdk.restoreMemoryRecord,
    purge: sdk.purgeMemoryRecord,
  };
  return checkItem(
    (
      await mutate(
        (headers) =>
          operations[action]({
            path: { recordRef: item.ref },
            headers: versioned(headers),
            signal: requestSignal(),
          }),
        item.version,
      )
    ).data,
    item.projectRef,
    item.ref,
  );
}
export async function history(
  kind: ContextKind,
  ref: string,
  pageToken: string | undefined,
  signal: AbortSignal,
): Promise<{ items: ContextRevision[]; total: number; nextPageToken: string }> {
  const query = { pageSize: 40, pageToken };
  const page =
    kind === "skills"
      ? (
          await unwrap(
            sdk.listSkillBundleRevisions({
              path: { bundleRef: ref },
              query,
              signal: requestSignal(signal),
            }),
          )
        ).data
      : (
          await unwrap(
            sdk.listMemoryRecordRevisions({
              path: { recordRef: ref },
              query,
              signal: requestSignal(signal),
            }),
          )
        ).data;
  if (
    page.total < page.items.length ||
    (page.nextPageToken && page.nextPageToken === pageToken) ||
    page.nextPageToken.length > 512 ||
    page.items.some(
      (item) => "redacted" in item && item.redacted && item.summary !== "",
    )
  )
    throw new Error("Invalid context history");
  return page;
}
