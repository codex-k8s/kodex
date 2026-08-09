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
import { executeMutation } from "@/shared/lib/identity";

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
    await executeMutation("project:create", body, undefined, (headers) =>
      createProject({
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

export async function updateWorkspace(
  resource: Resource,
  body: UpdateProject,
): Promise<Resource> {
  return (
    await executeMutation(
      `project:update:${resource.id}`,
      body,
      resource.version,
      (headers) =>
        updateProject({
          body,
          path: { projectId: resource.id },
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

export async function deleteWorkspace(resource: Resource): Promise<Resource> {
  return (
    await executeMutation(
      `project:delete:${resource.id}`,
      {},
      resource.version,
      (headers) =>
        deleteProject({
          path: { projectId: resource.id },
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
