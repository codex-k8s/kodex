import { requestSignal } from "@/shared/api/client";
import {
  listAccessRoles,
  listAccessSubjects,
  listAgents,
  listIntegrationConnections,
  listProjects,
  listWorkflows,
} from "@/shared/api/generated/openapi/sdk.gen";
import type {
  AccessRole,
  AccessSubject,
  AccessSubjectKind,
} from "@/shared/api/generated/openapi/types.gen";
import { unwrap } from "@/shared/api/problem";
import type { AsyncEntityOptionPage } from "@/shared/ui/async-entity-picker";

export type AccessSubjectCapture = (items: AccessSubject[]) => void;
export type AccessRoleCapture = (items: AccessRole[]) => void;

export async function accessSubjectOptions(
  kind: AccessSubjectKind | undefined,
  query: string,
  pageToken: string | undefined,
  signal: AbortSignal,
  pageSize = 20,
  capture?: AccessSubjectCapture,
): Promise<AsyncEntityOptionPage> {
  const page = (
    await unwrap(
      listAccessSubjects({
        query: {
          ...(query ? { query } : {}),
          ...(kind ? { kind } : {}),
          ...(pageToken ? { pageToken } : {}),
          pageSize,
        },
        signal: requestSignal(signal),
      }),
    )
  ).data;
  capture?.(page.items);
  return {
    items: page.items.map((item) => ({
      ref: item.ref,
      title: item.displayName,
    })),
    nextPageToken: page.nextPageToken,
  };
}

export async function accessRoleOptions(
  query: string,
  pageToken: string | undefined,
  signal: AbortSignal,
  pageSize = 20,
  capture?: AccessRoleCapture,
  value: "ROLE" | "VERSION" = "ROLE",
): Promise<AsyncEntityOptionPage> {
  const page = (
    await unwrap(
      listAccessRoles({
        query: {
          ...(query ? { query } : {}),
          ...(pageToken ? { pageToken } : {}),
          pageSize,
          includeArchived: false,
        },
        signal: requestSignal(signal),
      }),
    )
  ).data;
  capture?.(page.items);
  return {
    items: page.items.map((item) => ({
      ref: value === "VERSION" ? item.currentVersion.ref : item.ref,
      title: item.currentVersion.name,
      description: item.currentVersion.description,
      meta: `v${String(item.currentVersion.revision)}`,
    })),
    nextPageToken: page.nextPageToken,
  };
}

export async function accessProjectOptions(
  query: string,
  pageToken: string | undefined,
  signal: AbortSignal,
  pageSize = 20,
): Promise<AsyncEntityOptionPage> {
  const page = (
    await unwrap(
      listProjects({
        query: {
          ...(query ? { query } : {}),
          ...(pageToken ? { pageToken } : {}),
          pageSize,
        },
        signal: requestSignal(signal),
      }),
    )
  ).data;
  return {
    items: page.items.map((item) => ({
      ref: item.ref,
      title: item.name,
      description: item.purpose,
    })),
    nextPageToken: page.nextPageToken,
  };
}

export async function accessAgentOptions(
  projectRef: string,
  query: string,
  pageToken: string | undefined,
  signal: AbortSignal,
  pageSize = 20,
): Promise<AsyncEntityOptionPage> {
  if (!projectRef) return { items: [] };
  const page = (
    await unwrap(
      listAgents({
        path: { projectRef },
        query: {
          ...(query ? { query } : {}),
          ...(pageToken ? { pageToken } : {}),
          pageSize,
        },
        signal: requestSignal(signal),
      }),
    )
  ).data;
  return {
    items: page.items.map((item) => ({
      ref: item.ref,
      title: item.name,
      description: item.roleDescription,
    })),
    nextPageToken: page.nextPageToken,
  };
}

export async function accessWorkflowOptions(
  projectRef: string,
  query: string,
  pageToken: string | undefined,
  signal: AbortSignal,
  pageSize = 20,
): Promise<AsyncEntityOptionPage> {
  if (!projectRef) return { items: [] };
  const page = (
    await unwrap(
      listWorkflows({
        path: { projectRef },
        query: {
          ...(query ? { query } : {}),
          ...(pageToken ? { pageToken } : {}),
          pageSize,
        },
        signal: requestSignal(signal),
      }),
    )
  ).data;
  return {
    items: page.items.map((item) => ({
      ref: item.ref,
      title: item.name,
      description: item.purpose,
    })),
    nextPageToken: page.nextPageToken,
  };
}

export async function accessIntegrationOptions(
  query: string,
  pageToken: string | undefined,
  signal: AbortSignal,
  pageSize = 20,
): Promise<AsyncEntityOptionPage> {
  const page = (
    await unwrap(
      listIntegrationConnections({
        query: {
          ...(query ? { query } : {}),
          ...(pageToken ? { pageToken } : {}),
          pageSize,
        },
        signal: requestSignal(signal),
      }),
    )
  ).data;
  return {
    items: page.items.map((item) => ({
      ref: item.ref,
      title: item.name,
      description: item.definitionKey,
    })),
    nextPageToken: page.nextPageToken,
  };
}
