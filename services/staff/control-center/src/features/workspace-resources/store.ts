import { defineStore } from "pinia";
import { computed, reactive, ref } from "vue";

import { workspaceResourcesApi } from "@/features/workspace-resources/api";
import {
  toWorkspaceResourceModel,
  toWorkspaceSelectorModel,
  type WorkspaceResourceModel,
  type WorkspaceSelectorModel,
} from "@/features/workspace-resources/model";
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
    const resources = reactive(remoteState<WorkspaceResourceModel[]>([]));
    const selectorResources = reactive(
      remoteState<WorkspaceSelectorModel[]>([]),
    );
    const integrationDefinitions = reactive(
      remoteState<IntegrationDefinition[]>([]),
    );
    const capabilityOptions = reactive(remoteState<string[]>([]));
    const projectId = ref<string | null>(null);
    const mutationProblem = ref<AppProblem | null>(null);
    const mutating = ref(false);
    let mutationVersion = 0;
    const authoritativeResources = new Map<string, Resource>();
    const authoritative = (resource: WorkspaceResourceModel): Resource => {
      const value = authoritativeResources.get(resource.id);
      if (!value)
        throw new Error("Authoritative workspace resource is unavailable");
      return value;
    };
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
                workspaceResourcesApi.list(kind, selectedProjectId),
              ),
            ),
            Promise.all([
              workspaceResourcesApi.list("AGENT"),
              workspaceResourcesApi.list("ROLE_IMAGE_RECIPE"),
            ]),
            workspaceResourcesApi.listIntegrationDefinitions(),
            workspaceResourcesApi.listRoleDefinitions(),
          ]);
        const items = pages
          .flatMap((page) => page.resources)
          .filter((item) => item.projectId === selectedProjectId);
        if (projectId.value === selectedProjectId) {
          authoritativeResources.clear();
          items.forEach((item) => authoritativeResources.set(item.id, item));
          const models = items.map(toWorkspaceResourceModel);
          finishRequest(resources, request, models, models.length === 0);
          const selections = selectors
            .flatMap((page) => page.resources)
            .map(toWorkspaceSelectorModel);
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
      resource: WorkspaceResourceModel | null,
      name: string,
      spec: ResourceSpecInput,
    ) => {
      if (!resource)
        return mutate(() =>
          workspaceResourcesApi.createMutable({ kind, name, parentId, spec }),
        );
      const authoritativeResource = authoritative(resource);
      const body = { kind, name, spec };
      const credential = body.spec.credentialBinding;
      const repository = body.spec.repositoryWorkspace;
      const integration = body.spec.integration;
      const safeBody: UpdateResource = {
        ...body,
        spec: {
          ...body.spec,
          ...(credential && authoritativeResource.spec.credentialBinding
            ? {
                credentialBinding: {
                  ...credential,
                  immutableSecretRef:
                    credential.immutableSecretRef ||
                    authoritativeResource.spec.credentialBinding
                      .immutableSecretRef,
                  principalRef:
                    credential.principalRef ||
                    authoritativeResource.spec.credentialBinding.principalRef,
                },
              }
            : {}),
          ...(repository && authoritativeResource.spec.repositoryWorkspace
            ? {
                repositoryWorkspace: {
                  ...repository,
                  repositoryRef:
                    repository.repositoryRef ||
                    authoritativeResource.spec.repositoryWorkspace
                      .repositoryRef,
                },
              }
            : {}),
          ...(integration && authoritativeResource.spec.integration
            ? {
                integration: {
                  ...integration,
                  endpointRef:
                    integration.endpointRef ||
                    authoritativeResource.spec.integration.endpointRef,
                },
              }
            : {}),
        },
      };
      return mutate(() =>
        workspaceResourcesApi.updateMutable(authoritativeResource, safeBody),
      );
    };
    const transitionMutable = (
      resource: WorkspaceResourceModel,
      targetState: "ACTIVE" | "PAUSED" | "ARCHIVED",
    ) =>
      mutate(() =>
        workspaceResourcesApi.transitionMutable(authoritative(resource), {
          targetState,
          reasonCode: "OWNER_REQUEST",
        }),
      );
    const deleteWorkspaceResource = (resource: WorkspaceResourceModel) =>
      mutate(() =>
        workspaceResourcesApi.deleteMutable(authoritative(resource)),
      );
    const saveAccess = (
      kind: AccessResourceKind,
      resource: WorkspaceResourceModel | null,
      name: string,
      spec: AccessSpecInput,
    ) =>
      mutate(() =>
        workspaceResourcesApi.manageAccess(
          {
            kind,
            action: resource ? "UPDATE" : "CREATE",
            ...(resource ? { resourceId: resource.id } : {}),
            name,
            spec,
          },
          resource ? authoritative(resource).version : undefined,
        ),
      );
    const executeAccessAction = (
      resource: WorkspaceResourceModel,
      action: "ACTIVATE" | "PAUSE" | "ARCHIVE" | "DELETE",
    ) => {
      if (!isAccessKind(resource.kind)) return Promise.resolve(null);
      const kind = resource.kind;
      return mutate(() =>
        workspaceResourcesApi.manageAccess(
          { kind, action, resourceId: resource.id },
          authoritative(resource).version,
        ),
      );
    };
    const detach = (resource: WorkspaceResourceModel) => {
      if (!isAccessKind(resource.kind)) return Promise.resolve(null);
      const body: DetachAccessResource = { kind: resource.kind };
      return mutate(() =>
        workspaceResourcesApi.detachAccess(authoritative(resource), body),
      );
    };
    const copy = (resource: WorkspaceResourceModel, name: string) => {
      if (!isAccessKind(resource.kind)) return Promise.resolve(null);
      const body: CopyAccessResource = { kind: resource.kind, name };
      return mutate(() =>
        workspaceResourcesApi.copyAccess(authoritative(resource), body),
      );
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
