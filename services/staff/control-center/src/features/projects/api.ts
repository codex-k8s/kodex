import { versionedMutationHeaders } from "@/shared/api/versioned-mutation-headers";
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
  pageSize = 20,
) {
  return (
    await unwrap(
      listProjects({
        query: { query, pageToken, pageSize },
        signal: requestSignal(signal),
      }),
    )
  ).data;
}

export async function loadProjectTrash(
  pageToken: string | undefined,
  signal: AbortSignal,
  pageSize = 20,
) {
  return (
    await unwrap(
      listTrashedProjects({
        query: { pageToken, pageSize },
        signal: requestSignal(signal),
      }),
    )
  ).data;
}

export async function moveProjectToTrash(project: Project): Promise<Project> {
  return mutateProjectLifecycle(project, trashProject);
}

export async function restoreProjectFromTrash(
  project: Project,
): Promise<Project> {
  return mutateProjectLifecycle(project, restoreProject);
}

export async function purgeProjectFromTrash(
  project: Project,
): Promise<Project> {
  return mutateProjectLifecycle(project, purgeProject);
}

// Вызывает выбранную исходным adapter SDK-операцию корзины с прежними path, version, headers
// и signal, затем возвращает её Project receipt. Сам не выбирает действие, не меняет retry
// и не трактует 202 purge как завершение очистки: это остаётся контрактом отдельного SDK/caller.
async function mutateProjectLifecycle(
  project: Project,
  request: typeof trashProject | typeof restoreProject | typeof purgeProject,
): Promise<Project> {
  const response = await mutate(
    (headers) =>
      request({
        path: { projectRef: project.ref },
        headers: versionedMutationHeaders(headers),
        signal: requestSignal(),
      }),
    project.version,
  );
  return response.data;
}
