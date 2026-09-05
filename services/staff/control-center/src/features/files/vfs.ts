import { requestSignal } from "@/shared/api/client";
import {
  listVfsNodes,
  searchVfs,
} from "@/shared/api/generated/openapi/sdk.gen";
import type {
  VfsNode,
  VfsNodePage,
} from "@/shared/api/generated/openapi/types.gen";
import { unwrap } from "@/shared/api/problem";

export async function loadVfsPage(options: {
  path: string;
  query: string;
  projectRef?: string;
  pageToken?: string;
  signal: AbortSignal;
}): Promise<VfsNodePage> {
  const { projectRef, pageToken } = options;
  const signal = AbortSignal.any([options.signal, requestSignal()]);
  const query = options.query.trim();
  const pagination = { projectRef, pageToken, pageSize: 30 };
  const page = (
    await unwrap(
      query
        ? searchVfs({ query: { ...pagination, query }, signal })
        : listVfsNodes({
            query: { ...pagination, path: options.path },
            signal,
          }),
    )
  ).data;
  validateVfsPage(page, options);
  return page;
}

export function validateVfsPage(
  page: VfsNodePage,
  options: { projectRef?: string; path: string; query: string },
): void {
  if (
    !Array.isArray(page.items) ||
    !Number.isSafeInteger(page.total) ||
    page.total < 0 ||
    typeof page.nextPageToken !== "string" ||
    page.nextPageToken.length > 2048
  )
    throw new Error("Invalid VFS page");
  const refs = new Set<string>();
  for (const node of page.items) {
    if (
      !(node as VfsNode | null) ||
      typeof node.ref !== "string" ||
      !node.ref ||
      refs.has(node.ref) ||
      typeof node.name !== "string" ||
      !node.name ||
      typeof node.path !== "string" ||
      !node.path.startsWith("/projects/") ||
      typeof node.parentPath !== "string" ||
      typeof node.directory !== "boolean" ||
      ![
        "DIRECTORY",
        "PROJECT",
        "AGENT",
        "WORKFLOW",
        "RUN",
        "INPUT",
        "RESULT",
        "SKILL",
        "MEMORY",
        "AUTOMATION",
        "ENVIRONMENT",
        "AVATAR",
      ].includes(node.kind) ||
      !Number.isSafeInteger(node.sizeBytes) ||
      node.sizeBytes < 0 ||
      typeof node.projectRef !== "string" ||
      !node.projectRef ||
      typeof node.entityRef !== "string" ||
      typeof node.runRef !== "string" ||
      typeof node.digest !== "string" ||
      (options.projectRef && options.projectRef !== node.projectRef) ||
      (!options.query.trim() && node.parentPath !== options.path)
    )
      throw new Error("Invalid VFS node or scope");
    refs.add(node.ref);
  }
}

export function vfsEntityRoute(node: VfsNode): string | undefined {
  if (!node.projectRef || !node.entityRef) return undefined;
  const project = `/projects/${encodeURIComponent(node.projectRef)}`;
  const entity = encodeURIComponent(node.entityRef);
  switch (node.kind) {
    case "SKILL":
      return `${project}/context/skills/${entity}`;
    case "MEMORY":
      return `${project}/context/memory/${entity}`;
    case "PROJECT":
      return project;
    case "AGENT":
      return `${project}/agents/${entity}`;
    case "WORKFLOW":
      return `${project}/workflows/${entity}`;
    case "RUN":
      return `${project}/runs/${entity}`;
    case "AUTOMATION":
      return `${project}/automations?scheduleRef=${entity}`;
    case "ENVIRONMENT":
      return `${project}/environments/${entity}`;
    case "INPUT":
    case "RESULT":
    case "AVATAR":
      return `${project}/files?artifactRef=${entity}`;
    default:
      return undefined;
  }
}
