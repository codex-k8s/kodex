import { defineStore } from "pinia";
import { computed, reactive, ref } from "vue";

import {
  commandAccessResource,
  copyGitResource,
  createMutableResource,
  deleteMutableResource,
  detachGitResource,
  fetchResources,
  transitionMutableResource,
  updateMutableResource,
} from "@/shared/api/adapters/resources";
import {
  fetchIntegrationDefinitions,
  fetchRoleDefinitions,
} from "@/shared/api/adapters/owner-control";
import { toWorkspaceResourceModel } from "@/features/workspace-resources/model";
import type {
  AccessResourceKind,
  AccessSpecInput,
  CopyAccessResource,
  DetachAccessResource,
  IntegrationDefinition,
  MutableResourceKind,
  Resource,
  ResourceKind,
  ResourceSpecInput,
  UpdateResource,
} from "@/shared/api/generated/openapi/types.gen";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import {
  beginRequest,
  failRequest,
  finishRequest,
  invalidate,
  remoteState,
  resetRemoteState,
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
    const selectorResources = reactive(remoteState<Resource[]>([]));
    const integrationDefinitions = reactive(
      remoteState<IntegrationDefinition[]>([]),
    );
    const capabilityOptions = reactive(remoteState<string[]>([]));
    const projectId = ref<string | null>(null);
    const mutationProblem = ref<AppProblem | null>(null);
    const mutating = ref(false);
    let mutationVersion = 0;
    const authoritativeResources = new Map<string, Resource>();
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
    const integrations = computed(() =>
      resources.data.filter((item) => item.kind === "INTEGRATION"),
    );

    async function load(selectedProjectId: string): Promise<void> {
      if (projectId.value !== selectedProjectId) {
        mutationVersion += 1;
        resetRemoteState(resources, []);
        mutationProblem.value = null;
        mutating.value = false;
      }
      projectId.value = selectedProjectId;
      const request = beginRequest(resources);
      try {
        const [pages, selectors, definitions, roleDefinitions] =
          await Promise.all([
            Promise.all(
              workspaceKinds.map((kind) =>
                fetchResources(kind, selectedProjectId),
              ),
            ),
            Promise.all([
              fetchResources("AGENT"),
              fetchResources("ROLE_IMAGE_RECIPE"),
            ]),
            fetchIntegrationDefinitions(),
            fetchRoleDefinitions(),
          ]);
        const items = pages
          .flatMap((page) => page.resources)
          .filter((item) => item.projectId === selectedProjectId);
        if (projectId.value === selectedProjectId) {
          authoritativeResources.clear();
          items.forEach((item) => authoritativeResources.set(item.id, item));
          const models = items.map(toWorkspaceResourceModel);
          finishRequest(resources, request, models, models.length === 0);
          const selections = selectors.flatMap((page) => page.resources);
          selectorResources.data = selections;
          selectorResources.phase = selections.length ? "ready" : "empty";
          integrationDefinitions.data = definitions.definitions;
          integrationDefinitions.phase = definitions.definitions.length
            ? "ready"
            : "empty";
          const capabilities = [
            ...roleDefinitions.resources.flatMap(
              (item) => item.spec.roleDefinition?.capabilities ?? [],
            ),
            ...items.flatMap((item) => item.spec.role?.capabilities ?? []),
          ]
            .filter((value, index, values) => values.indexOf(value) === index)
            .sort((left, right) => left.localeCompare(right));
          capabilityOptions.data = capabilities;
          capabilityOptions.phase = capabilities.length ? "ready" : "empty";
        }
      } catch (error) {
        if (projectId.value === selectedProjectId)
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

    const saveMutable = (
      parentId: string,
      kind: MutableResourceKind,
      resource: Resource | null,
      name: string,
      spec: ResourceSpecInput,
    ) => {
      if (!resource)
        return mutate(() =>
          createMutableResource({ kind, name, parentId, spec }),
        );
      const authoritative = authoritativeResources.get(resource.id) ?? resource;
      const body = { kind, name, spec };
      const credential = body.spec.credentialBinding;
      const repository = body.spec.repositoryWorkspace;
      const integration = body.spec.integration;
      const safeBody: UpdateResource = {
        ...body,
        spec: {
          ...body.spec,
          ...(credential && authoritative.spec.credentialBinding
            ? {
                credentialBinding: {
                  ...credential,
                  immutableSecretRef:
                    credential.immutableSecretRef ||
                    authoritative.spec.credentialBinding.immutableSecretRef,
                  principalRef:
                    credential.principalRef ||
                    authoritative.spec.credentialBinding.principalRef,
                },
              }
            : {}),
          ...(repository && authoritative.spec.repositoryWorkspace
            ? {
                repositoryWorkspace: {
                  ...repository,
                  repositoryRef:
                    repository.repositoryRef ||
                    authoritative.spec.repositoryWorkspace.repositoryRef,
                },
              }
            : {}),
          ...(integration && authoritative.spec.integration
            ? {
                integration: {
                  ...integration,
                  endpointRef:
                    integration.endpointRef ||
                    authoritative.spec.integration.endpointRef,
                },
              }
            : {}),
        },
      };
      return mutate(() => updateMutableResource(authoritative, safeBody));
    };
    const transitionMutable = (
      resource: Resource,
      targetState: "ACTIVE" | "PAUSED" | "ARCHIVED",
    ) =>
      mutate(() =>
        transitionMutableResource(
          authoritativeResources.get(resource.id) ?? resource,
          { targetState, reasonCode: "OWNER_REQUEST" },
        ),
      );
    const deleteWorkspaceResource = (resource: Resource) =>
      mutate(() =>
        deleteMutableResource(
          authoritativeResources.get(resource.id) ?? resource,
        ),
      );
    const saveAccess = (
      kind: AccessResourceKind,
      resource: Resource | null,
      name: string,
      spec: AccessSpecInput,
    ) =>
      mutate(() =>
        commandAccessResource(
          {
            kind,
            action: resource ? "UPDATE" : "CREATE",
            ...(resource ? { resourceId: resource.id } : {}),
            name,
            spec,
          },
          resource?.version,
        ),
      );
    const executeAccessAction = (
      resource: Resource,
      action: "ACTIVATE" | "PAUSE" | "ARCHIVE" | "DELETE",
    ) => {
      if (!isAccessKind(resource.kind)) return Promise.resolve(null);
      const kind = resource.kind;
      return mutate(() =>
        commandAccessResource(
          { kind, action, resourceId: resource.id },
          resource.version,
        ),
      );
    };
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

    function reset(): void {
      mutationVersion += 1;
      authoritativeResources.clear();
      projectId.value = null;
      resetRemoteState(resources, []);
      resetRemoteState(selectorResources, []);
      resetRemoteState(integrationDefinitions, []);
      resetRemoteState(capabilityOptions, []);
      mutationProblem.value = null;
      mutating.value = false;
    }

    return {
      resources,
      selectorResources,
      integrationDefinitions,
      capabilityOptions,
      repositories,
      chats,
      access,
      credentials,
      integrations,
      mutationProblem,
      mutating,
      load,
      saveMutable,
      transitionMutable,
      deleteWorkspaceResource,
      saveAccess,
      executeAccessAction,
      detach,
      copy,
      reset,
    };
  },
);
