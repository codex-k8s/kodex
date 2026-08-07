import { defineStore } from "pinia";
import { computed, reactive, ref } from "vue";

import {
  copyGitResource,
  createMutableResource,
  detachGitResource,
  fetchResources,
} from "@/shared/api/adapters/resources";
import type {
  AccessResourceKind,
  CopyAccessResource,
  CreateResource,
  DetachAccessResource,
  Resource,
  ResourceKind,
} from "@/shared/api/generated/openapi/types.gen";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import {
  beginRequest,
  failRequest,
  finishRequest,
  invalidate,
  remoteState,
} from "@/shared/lib/remote";

const workspaceKinds: ResourceKind[] = [
  "CHAT",
  "CREDENTIAL_BINDING",
  "REPOSITORY_WORKSPACE",
  "INTEGRATION",
  "TEAM",
  "ROLE",
  "PROMPT_PROFILE",
];

function isAccessKind(kind: ResourceKind): kind is AccessResourceKind {
  return kind === "TEAM" || kind === "ROLE" || kind === "PROMPT_PROFILE";
}

export const useWorkspaceResourcesStore = defineStore(
  "workspace-resources",
  () => {
    const resources = reactive(remoteState<Resource[]>([]));
    const projectId = ref<string | null>(null);
    const mutationProblem = ref<AppProblem | null>(null);
    const mutating = ref(false);
    let mutationVersion = 0;
    const repositories = computed(() =>
      resources.data.filter((item) => item.kind === "REPOSITORY_WORKSPACE"),
    );
    const chats = computed(() =>
      resources.data.filter((item) => item.kind === "CHAT"),
    );
    const access = computed(() =>
      resources.data.filter((item) => isAccessKind(item.kind)),
    );
    const credentials = computed(() =>
      resources.data.filter((item) => item.kind === "CREDENTIAL_BINDING"),
    );

    async function load(selectedProjectId: string): Promise<void> {
      projectId.value = selectedProjectId;
      const request = beginRequest(resources);
      try {
        const pages = await Promise.all(
          workspaceKinds.map((kind) => fetchResources(kind, selectedProjectId)),
        );
        const items = pages
          .flatMap((page) => page.resources)
          .filter((item) => item.projectId === selectedProjectId);
        finishRequest(resources, request, items, items.length === 0);
      } catch (error) {
        failRequest(resources, request, asProblem(error));
      }
    }

    async function mutate(
      operation: () => Promise<Resource>,
    ): Promise<Resource | null> {
      if (!projectId.value) return null;
      const selectedProjectId = projectId.value;
      const version = ++mutationVersion;
      invalidate(resources);
      mutating.value = true;
      mutationProblem.value = null;
      try {
        const result = await operation();
        if (
          version !== mutationVersion ||
          projectId.value !== selectedProjectId
        )
          return null;
        await load(selectedProjectId);
        if (version !== mutationVersion) return null;
        return result;
      } catch (error) {
        if (version !== mutationVersion) return null;
        mutationProblem.value = asProblem(error);
        if (mutationProblem.value.kind === "conflict")
          resources.phase = "conflict";
        return null;
      } finally {
        if (version === mutationVersion) mutating.value = false;
      }
    }

    const create = (body: CreateResource) =>
      mutate(() => createMutableResource(body));
    const detach = (resource: Resource) => {
      if (!isAccessKind(resource.kind)) return Promise.resolve(null);
      const body: DetachAccessResource = { kind: resource.kind };
      return mutate(() => detachGitResource(resource, body));
    };
    const copy = (resource: Resource, name: string) => {
      if (!isAccessKind(resource.kind)) return Promise.resolve(null);
      const body: CopyAccessResource = { kind: resource.kind, name };
      return mutate(() => copyGitResource(resource, body));
    };

    return {
      resources,
      repositories,
      chats,
      access,
      credentials,
      mutationProblem,
      mutating,
      load,
      create,
      detach,
      copy,
    };
  },
);
