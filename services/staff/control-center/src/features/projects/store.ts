import { defineStore } from "pinia";
import { reactive, ref } from "vue";

import {
  createWorkspace,
  deleteWorkspace,
  fetchProjects,
  updateWorkspace,
} from "@/shared/api/adapters/projects";
import type {
  ProjectLocale,
  Resource,
} from "@/shared/api/generated/openapi/types.gen";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import {
  acceptRequest,
  beginRequest,
  failRequest,
  finishRequest,
  invalidate,
  remoteState,
  resetRemoteState,
} from "@/shared/lib/remote";
import { type ProjectModel, toProjectModel } from "./model";

export const useProjectsStore = defineStore("projects", () => {
  const projects = reactive(remoteState<ProjectModel[]>([]));
  const authoritative = new Map<string, Resource>();
  const nextPageToken = ref<string | null>(null);
  const mutationProblem = ref<AppProblem | null>(null);
  const mutating = ref(false);
  let mutationVersion = 0;

  async function load(): Promise<void> {
    const request = beginRequest(projects);
    try {
      const page = await fetchProjects();
      authoritative.clear();
      page.resources.forEach((item) => authoritative.set(item.id, item));
      finishRequest(
        projects,
        request,
        page.resources.map(toProjectModel),
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
      page.resources.forEach((item) => authoritative.set(item.id, item));
      finishRequest(
        projects,
        request,
        [...current, ...page.resources.map(toProjectModel)],
        false,
      );
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

  const create = (
    name: string,
    slug: string,
    description: string,
    locale: ProjectLocale,
  ) =>
    mutate(() =>
      createWorkspace({ name, spec: { slug, description, locale } }),
    );
  const update = (
    resource: ProjectModel,
    name: string,
    slug: string,
    description: string,
    locale: ProjectLocale,
  ) =>
    mutate(() => {
      const current = authoritative.get(resource.id);
      if (!current)
        return Promise.reject(new Error("Project readback is unavailable"));
      return updateWorkspace(current, {
        name,
        spec: { slug, description, locale },
      });
    });
  const remove = (resource: ProjectModel) =>
    mutate(() => {
      const current = authoritative.get(resource.id);
      if (!current)
        return Promise.reject(new Error("Project readback is unavailable"));
      return deleteWorkspace(current);
    });

  function invalidatePending(): void {
    mutationVersion += 1;
    invalidate(projects);
    mutationProblem.value = null;
    mutating.value = false;
  }

  function reset(): void {
    mutationVersion += 1;
    resetRemoteState(projects, []);
    authoritative.clear();
    nextPageToken.value = null;
    mutationProblem.value = null;
    mutating.value = false;
  }

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
    invalidatePending,
    reset,
  };
});
