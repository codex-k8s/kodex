import {
  copyAccessResource,
  createResource,
  detachAccessResource,
  getResource,
  listResources,
  searchResources,
} from "@/shared/api/generated/openapi/sdk.gen";
import type {
  CopyAccessResource,
  CreateResource,
  DetachAccessResource,
  Resource,
  ResourceKind,
  ResourcePage,
} from "@/shared/api/generated/openapi/types.gen";
import { requestSignal } from "@/shared/api/client";
import { unwrap } from "@/shared/api/problem";
import { mutationHeaders } from "@/shared/lib/identity";

export async function fetchResources(
  kind: ResourceKind,
  parentId?: string,
): Promise<ResourcePage> {
  return (
    await unwrap(
      listResources({
        query: { kind, pageSize: 100, ...(parentId ? { parentId } : {}) },
        signal: requestSignal(),
      }),
    )
  ).data;
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
): Promise<ResourcePage> {
  return (
    await unwrap(
      searchResources({
        query: { kind, query, pageSize: 100 },
        signal: requestSignal(),
      }),
    )
  ).data;
}

export async function createMutableResource(
  body: CreateResource,
): Promise<Resource> {
  return (
    await unwrap(
      createResource({
        body,
        headers: mutationHeaders() as {
          "X-CSRF-Token": string;
          "Idempotency-Key": string;
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
    await unwrap(
      detachAccessResource({
        body,
        path: { resourceId: resource.id },
        headers: mutationHeaders(resource.version) as {
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
    await unwrap(
      copyAccessResource({
        body,
        path: { resourceId: resource.id },
        headers: mutationHeaders(resource.version) as {
          "X-CSRF-Token": string;
          "Idempotency-Key": string;
          "If-Match": string;
        },
        signal: requestSignal(),
      }),
    )
  ).data;
}
