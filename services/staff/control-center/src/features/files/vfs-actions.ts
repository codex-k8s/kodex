import { requestSignal } from "@/shared/api/client";
import { ownerRequestSignal } from "@/shared/api/owner-lifetime";
import * as sdk from "@/shared/api/generated/openapi/sdk.gen";
import type {
  ArtifactImpact,
  VfsNode,
  SkillBundle,
  KodexMemoryRecord,
} from "@/shared/api/generated/openapi/types.gen";
import { mutate } from "@/shared/api/mutation";
import { asProblem, unwrap, type AppProblem } from "@/shared/api/problem";
import {
  deleteArtifactItem,
  loadArtifactImpact,
  purgeArtifactItem,
  restoreArtifactItem,
} from "./api";

export type VfsBulkAction = "REMOVE" | "RESTORE" | "PURGE";
export function vfsActionController(): AbortController {
  const controller = new AbortController();
  const owner = ownerRequestSignal();
  const abort = () => controller.abort();
  owner.addEventListener("abort", abort, { once: true });
  controller.signal.addEventListener(
    "abort",
    () => owner.removeEventListener("abort", abort),
    { once: true },
  );
  if (owner.aborted) controller.abort();
  return controller;
}
export interface VfsPreparedItem {
  node: VfsNode;
  action: "DELETE" | "ARCHIVE" | "RESTORE" | "PURGE";
  impact?: ArtifactImpact;
}
export interface VfsActionReceipt {
  node: VfsNode;
  status: "SUCCEEDED" | "FAILED";
  problem?: AppProblem;
}
export function vfsAction(
  node: VfsNode,
  action: VfsBulkAction,
): VfsPreparedItem["action"] | undefined {
  if (
    !node.selectable ||
    !node.entityRef ||
    !Number.isSafeInteger(node.version) ||
    node.version < 1
  )
    return undefined;
  if (
    node.resourceKind !== "ARTIFACT" &&
    node.resourceKind !== "SKILL_BUNDLE" &&
    node.resourceKind !== "MEMORY_RECORD"
  )
    return undefined;
  const command =
    action === "REMOVE"
      ? node.resourceKind === "ARTIFACT"
        ? "DELETE"
        : "ARCHIVE"
      : action;
  return node.nextActions.includes(command) ? command : undefined;
}

export async function prepareVfsAction(
  nodes: readonly VfsNode[],
  action: VfsBulkAction,
  parent: AbortSignal,
): Promise<VfsPreparedItem[]> {
  if (
    !nodes.length ||
    nodes.length > 100 ||
    new Set(nodes.map((node) => `${node.resourceKind}:${node.entityRef}`))
      .size !== nodes.length
  )
    throw new Error("Invalid VFS selection");
  const signal = requestSignal(parent);
  const prepared: VfsPreparedItem[] = [];
  for (const node of nodes) {
    signal.throwIfAborted();
    const command = vfsAction(node, action);
    if (!command) throw new Error("VFS action is unavailable");
    const item: VfsPreparedItem = {
      node: { ...node, nextActions: [...node.nextActions] },
      action: command,
    };
    if (
      node.resourceKind === "ARTIFACT" &&
      (command === "DELETE" || command === "PURGE")
    ) {
      item.impact = await loadArtifactImpact(
        { ref: node.entityRef, version: node.version },
        command,
        signal,
      );
      signal.throwIfAborted();
      if (!item.impact.permitted)
        throw new Error("VFS artifact impact does not permit this action");
    }
    prepared.push(item);
  }
  return prepared;
}

function completedPurge(value: unknown, ref: string): boolean {
  return (
    typeof value === "object" &&
    value !== null &&
    "artifactRef" in value &&
    value.artifactRef === ref &&
    "lifecycleState" in value &&
    value.lifecycleState === "PURGED"
  );
}

async function applyItem(
  item: VfsPreparedItem,
  signal: AbortSignal,
): Promise<void> {
  signal.throwIfAborted();
  const { node, action, impact } = item;
  if (
    !vfsAction(
      node,
      action === "DELETE" || action === "ARCHIVE" ? "REMOVE" : action,
    )
  )
    throw new Error("VFS action is unavailable");
  if (node.resourceKind === "ARTIFACT") {
    const target = { ref: node.entityRef, version: node.version };
    if (action === "PURGE") {
      if (!impact) throw new Error("VFS artifact impact is missing");
      const result = await purgeArtifactItem(target, impact, signal);
      signal.throwIfAborted();
      if (!completedPurge(result, node.entityRef))
        throw new Error("VFS purge receipt does not match selection");
      return;
    }
    const result =
      action === "RESTORE"
        ? await restoreArtifactItem(target, signal)
        : action === "DELETE" && impact
          ? await deleteArtifactItem(target, impact, signal)
          : undefined;
    signal.throwIfAborted();
    if (
      !result ||
      result.ref !== node.entityRef ||
      result.projectRef !== node.projectRef ||
      result.lifecycleState !== (action === "RESTORE" ? "ACTIVE" : "DELETED") ||
      result.version <= node.version
    )
      throw new Error("VFS artifact receipt does not match selection");
    return;
  }
  if (action === "DELETE") throw new Error("Invalid VFS context action");
  const result = await mutate<SkillBundle | KodexMemoryRecord>((headers) => {
    if (!headers["If-Match"])
      throw new Error("VFS resource version is missing");
    const versioned = { ...headers, "If-Match": headers["If-Match"] };
    if (node.resourceKind === "SKILL_BUNDLE") {
      const operation = {
        ARCHIVE: sdk.archiveSkillBundle,
        RESTORE: sdk.restoreSkillBundle,
        PURGE: sdk.purgeSkillBundle,
      }[action];
      return operation({
        path: { bundleRef: node.entityRef },
        headers: versioned,
        signal,
      });
    }
    const operation = {
      ARCHIVE: sdk.archiveMemoryRecord,
      RESTORE: sdk.restoreMemoryRecord,
      PURGE: sdk.purgeMemoryRecord,
    }[action];
    return operation({
      path: { recordRef: node.entityRef },
      headers: versioned,
      signal,
    });
  }, node.version);
  signal.throwIfAborted();
  if (
    result.data.ref !== node.entityRef ||
    result.data.projectRef !== node.projectRef ||
    result.data.state !==
      (action === "RESTORE"
        ? "ACTIVE"
        : action === "ARCHIVE"
          ? "ARCHIVED"
          : "PURGED") ||
    result.data.version <= node.version
  )
    throw new Error("VFS context receipt does not match selection");
}

export async function downloadVfsNode(
  node: VfsNode,
  parent: AbortSignal,
): Promise<Blob> {
  if (
    node.resourceKind !== "ARTIFACT" ||
    !node.nextActions.includes("DOWNLOAD")
  )
    throw new Error("VFS download is unavailable");
  const signal = requestSignal(parent);
  const result = await unwrap(
    sdk.downloadArtifact({
      path: { artifactRef: node.entityRef },
      query: { purpose: "DOWNLOAD" },
      parseAs: "blob",
      signal,
    }),
  );
  signal.throwIfAborted();
  if (!(result.data instanceof Blob))
    throw new Error("Invalid VFS content response");
  return result.data;
}

export async function applyVfsAction(
  items: readonly VfsPreparedItem[],
  parent: AbortSignal,
): Promise<VfsActionReceipt[]> {
  const signal = requestSignal(parent);
  const receipts: VfsActionReceipt[] = [];
  for (const item of items) {
    signal.throwIfAborted();
    try {
      await applyItem(item, signal);
      receipts.push({ node: item.node, status: "SUCCEEDED" });
    } catch (error) {
      signal.throwIfAborted();
      receipts.push({
        node: item.node,
        status: "FAILED",
        problem: asProblem(error),
      });
    }
  }
  return receipts;
}
