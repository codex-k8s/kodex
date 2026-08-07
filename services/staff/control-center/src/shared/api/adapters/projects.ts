import {
  createProject,
  deleteProject,
  listProjects,
  updateProject,
} from "@/shared/api/generated/openapi/sdk.gen";
import type {
  CreateProject,
  Resource,
  ResourcePage,
  UpdateProject,
} from "@/shared/api/generated/openapi/types.gen";
import { requestSignal } from "@/shared/api/client";
import { unwrap } from "@/shared/api/problem";
import { mutationHeaders } from "@/shared/lib/identity";

export async function fetchProjects(pageToken?: string): Promise<ResourcePage> {
  return (
    await unwrap(
      listProjects({
        query: { pageSize: 100, ...(pageToken ? { pageToken } : {}) },
        signal: requestSignal(),
      }),
    )
  ).data;
}

export async function createWorkspace(body: CreateProject): Promise<Resource> {
  return (
    await unwrap(
      createProject({
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

export async function updateWorkspace(
  resource: Resource,
  body: UpdateProject,
): Promise<Resource> {
  return (
    await unwrap(
      updateProject({
        body,
        path: { projectId: resource.id },
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

export async function deleteWorkspace(resource: Resource): Promise<Resource> {
  return (
    await unwrap(
      deleteProject({
        path: { projectId: resource.id },
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
