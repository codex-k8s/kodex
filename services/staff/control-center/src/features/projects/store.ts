import { defineStore } from "pinia";
import { reactive, ref } from "vue";

import {
  createWorkspace,
  deleteWorkspace,
  fetchProjects,
  updateWorkspace,
} from "@/shared/api/adapters/projects";
import type {
  CreateProject,
  Resource,
  UpdateProject,
} from "@/shared/api/generated/openapi/types.gen";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import {
  acceptRequest,
  beginRequest,
  failRequest,
  finishRequest,
  invalidate,
  remoteState,
} from "@/shared/lib/remote";

export const useProjectsStore = defineStore("projects", () => {
  const projects = reactive(remoteState<Resource[]>([]));
  const nextPageToken = ref<string | null>(null);
  const mutationProblem = ref<AppProblem | null>(null);
  const mutating = ref(false);
  let mutationVersion = 0;

  async function load(): Promise<void> {
    const request = beginRequest(projects);
    try {
      const page = await fetchProjects();
      finishRequest(
        projects,
        request,
        page.resources,
        page.resources.length === 0,
      );
      if (acceptRequest(projects, request))
        nextPageToken.value = page.nextPageToken ?? null;
    } catch (error) {
      failRequest(projects, request, asProblem(error));
    }
  }

  async function loadMore(): Promise<void> {
    if (!nextPageToken.value || projects.phase === "loading") return;
    const token = nextPageToken.value;
    const request = beginRequest(projects);
    const current = [...projects.data];
    try {
      const page = await fetchProjects(token);
      finishRequest(projects, request, [...current, ...page.resources], false);
      if (acceptRequest(projects, request))
        nextPageToken.value = page.nextPageToken ?? null;
    } catch (error) {
      failRequest(projects, request, asProblem(error));
    }
  }

  async function mutate(
    operation: () => Promise<Resource>,
  ): Promise<Resource | null> {
    const version = ++mutationVersion;
    invalidate(projects);
    mutating.value = true;
    mutationProblem.value = null;
    try {
      const result = await operation();
      if (version !== mutationVersion) return null;
      await load();
      if (version !== mutationVersion) return null;
      return result;
    } catch (error) {
      if (version !== mutationVersion) return null;
      mutationProblem.value = asProblem(error);
      if (mutationProblem.value.kind === "conflict")
        projects.phase = "conflict";
      return null;
    } finally {
      if (version === mutationVersion) mutating.value = false;
    }
  }

  const create = (body: CreateProject) => mutate(() => createWorkspace(body));
  const update = (resource: Resource, body: UpdateProject) =>
    mutate(() => updateWorkspace(resource, body));
  const remove = (resource: Resource) =>
    mutate(() => deleteWorkspace(resource));

  return {
    projects,
    nextPageToken,
    mutationProblem,
    mutating,
    load,
    loadMore,
    create,
    update,
    remove,
  };
});
