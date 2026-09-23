import {
  getProject,
  listProjects,
  listTrashedProjects,
  purgeProject,
  restoreProject,
  trashProject,
} from "@/shared/api/generated/openapi/sdk.gen";
import { requestSignal } from "@/shared/api/client";
import { unwrap } from "@/shared/api/problem";
import { mutate } from "@/shared/api/mutation";
import type { Project } from "@/shared/api/generated/openapi/types.gen";

export async function loadProject(projectRef: string, signal: AbortSignal) {
  return (
    await unwrap(
      getProject({ path: { projectRef }, signal: requestSignal(signal) }),
    )
  ).data;
}

export async function searchProjects(
  query: string,
  pageToken: string | undefined,
  signal: AbortSignal,
) {
  return (
    await unwrap(
      listProjects({
        query: { query, pageToken, pageSize: 30 },
        signal: requestSignal(signal),
      }),
    )
  ).data;
}

export async function loadProjectTrash(
  pageToken: string | undefined,
  signal: AbortSignal,
) {
  return (
    await unwrap(
      listTrashedProjects({
        query: { pageToken, pageSize: 30 },
        signal: requestSignal(signal),
      }),
    )
  ).data;
}

export async function moveProjectToTrash(project: Project): Promise<Project> {
  const response = await mutate(
    (headers) =>
      trashProject({
        path: { projectRef: project.ref },
        headers: {
          "Idempotency-Key": headers["Idempotency-Key"],
          "X-CSRF-Token": headers["X-CSRF-Token"],
          "If-Match": headers["If-Match"] ?? "",
        },
        signal: requestSignal(),
      }),
    project.version,
  );
  return response.data;
}

export async function restoreProjectFromTrash(
  project: Project,
): Promise<Project> {
  const response = await mutate(
    (headers) =>
      restoreProject({
        path: { projectRef: project.ref },
        headers: {
          "Idempotency-Key": headers["Idempotency-Key"],
          "X-CSRF-Token": headers["X-CSRF-Token"],
          "If-Match": headers["If-Match"] ?? "",
        },
        signal: requestSignal(),
      }),
    project.version,
  );
  return response.data;
}

export async function purgeProjectFromTrash(project: Project): Promise<Project> {
  const response = await mutate(
    (headers) =>
      purgeProject({
        path: { projectRef: project.ref },
        headers: {
          "Idempotency-Key": headers["Idempotency-Key"],
          "X-CSRF-Token": headers["X-CSRF-Token"],
          "If-Match": headers["If-Match"] ?? "",
        },
        signal: requestSignal(),
      }),
    project.version,
  );
  return response.data;
}
