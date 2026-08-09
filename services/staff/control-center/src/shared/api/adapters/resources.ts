import {
  copyAccessResource,
  createResource,
  deleteResource,
  detachAccessResource,
  getResource,
  listResources,
  manageAccessResource,
  searchResources,
  transitionResource,
  updateResource,
} from "@/shared/api/generated/openapi/sdk.gen";
import type {
  CopyAccessResource,
  CreateResource,
  DetachAccessResource,
  ManageAccessResource,
  Resource,
  ResourceKind,
  ResourcePage,
  TransitionResource,
  UpdateResource,
} from "@/shared/api/generated/openapi/types.gen";
import { requestSignal } from "@/shared/api/client";
import { collectAllPages } from "@/shared/api/pagination";
import { unwrap } from "@/shared/api/problem";
import { executeMutation } from "@/shared/lib/identity";

export async function fetchResources(
  kind: ResourceKind,
  parentId?: string,
): Promise<ResourcePage> {
  const result = await collectAllPages(
    async (pageToken) =>
      (
        await unwrap(
          listResources({
            query: {
              kind,
              pageSize: 100,
              ...(parentId ? { parentId } : {}),
              ...(pageToken ? { pageToken } : {}),
            },
            signal: requestSignal(),
          }),
        )
      ).data,
    (page) => page.resources,
  );
  return { resources: result.values };
}

export async function fetchResource(
  resourceId: string,
  expectedKind: ResourceKind,
): Promise<Resource> {
  return (
    await unwrap(
      getResource({
        path: { resourceId },
        query: { expectedKind },
        signal: requestSignal(),
      }),
    )
  ).data;
}

export async function searchAuthoritativeResources(
  kind: ResourceKind,
  query: string,
  pageToken?: string,
): Promise<ResourcePage> {
  return (
    await unwrap(
      searchResources({
        query: {
          kind,
          query,
          pageSize: 100,
          ...(pageToken ? { pageToken } : {}),
        },
        signal: requestSignal(),
      }),
    )
  ).data;
}

export async function createMutableResource(
  body: CreateResource,
): Promise<Resource> {
  return (
    await executeMutation(
      `resource:create:${body.kind}:${body.parentId ?? "root"}`,
      body,
      undefined,
      (headers) =>
        createResource({
          body,
          headers: headers as {
            "X-CSRF-Token": string;
            "Idempotency-Key": string;
          },
          signal: requestSignal(),
        }),
    )
  ).data;
}

export async function updateMutableResource(
  resource: Resource,
  body: UpdateResource,
): Promise<Resource> {
  return (
    await executeMutation(
      `resource:update:${resource.id}`,
      body,
      resource.version,
      (headers) =>
        updateResource({
          body,
          path: { resourceId: resource.id },
          headers: headers as {
            "X-CSRF-Token": string;
            "Idempotency-Key": string;
            "If-Match": string;
          },
          signal: requestSignal(),
        }),
    )
  ).data;
}

export async function transitionMutableResource(
  resource: Resource,
  body: TransitionResource,
): Promise<Resource> {
  return (
    await executeMutation(
      `resource:transition:${resource.id}`,
      body,
      resource.version,
      (headers) =>
        transitionResource({
          body,
          path: { resourceId: resource.id },
          headers: headers as {
            "X-CSRF-Token": string;
            "Idempotency-Key": string;
            "If-Match": string;
          },
          signal: requestSignal(),
        }),
    )
  ).data;
}

export async function deleteMutableResource(
  resource: Resource,
): Promise<Resource> {
  return (
    await executeMutation(
      `resource:delete:${resource.id}`,
      {},
      resource.version,
      (headers) =>
        deleteResource({
          path: { resourceId: resource.id },
          headers: headers as {
            "X-CSRF-Token": string;
            "Idempotency-Key": string;
            "If-Match": string;
          },
          signal: requestSignal(),
        }),
    )
  ).data;
}

export async function commandAccessResource(
  body: ManageAccessResource,
  version?: number,
): Promise<Resource> {
  return (
    await executeMutation(
      `access-resource:${body.action}:${body.resourceId ?? body.kind}`,
      body,
      version,
      (headers) =>
        manageAccessResource({
          body,
          headers: headers as {
            "X-CSRF-Token": string;
            "Idempotency-Key": string;
            "If-Match"?: string;
          },
          signal: requestSignal(),
        }),
    )
  ).data;
}

export async function detachGitResource(
  resource: Resource,
  body: DetachAccessResource,
): Promise<Resource> {
  return (
    await executeMutation(
      `access-resource:detach:${resource.id}`,
      body,
      resource.version,
      (headers) =>
        detachAccessResource({
          body,
          path: { resourceId: resource.id },
          headers: headers as {
            "X-CSRF-Token": string;
            "Idempotency-Key": string;
            "If-Match": string;
          },
          signal: requestSignal(),
        }),
    )
  ).data;
}

export async function copyGitResource(
  resource: Resource,
  body: CopyAccessResource,
): Promise<Resource> {
  return (
    await executeMutation(
      `access-resource:copy:${resource.id}`,
      body,
      resource.version,
      (headers) =>
        copyAccessResource({
          body,
          path: { resourceId: resource.id },
          headers: headers as {
            "X-CSRF-Token": string;
            "Idempotency-Key": string;
            "If-Match": string;
          },
          signal: requestSignal(),
        }),
    )
  ).data;
}
