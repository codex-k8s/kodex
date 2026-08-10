import { defineStore } from "pinia";
import { reactive } from "vue";

import {
  commandAgent,
  commandAssignment,
  commandBotIdentity,
  commandRoleDefinition,
  fetchAgentHistory,
  fetchAgents,
  fetchAssignmentHistory,
  fetchAssignments,
  fetchBotIdentities,
  fetchBotOperation,
  fetchConfigurationSource,
  fetchInstructionSets,
  fetchOwnerCatalog,
  fetchProviderPools,
  fetchRoleDefinitionHistory,
  fetchRoleDefinitions,
} from "@/shared/api/adapters/owner-control";
import { fetchResources } from "@/shared/api/adapters/resources";
import type {
  AgentBotIdentityCommand,
  AgentCommand,
  RoleDefinitionCommand,
} from "@/shared/api/generated/openapi/types.gen";
import { createFeatureRuntime } from "@/shared/lib/feature-store";
import {
  completeMutationIntent,
  pendingMutationKey,
} from "@/shared/lib/identity";
import { remoteState, resetRemoteState } from "@/shared/lib/remote";
import {
  type AgentModel,
  type AssignmentModel,
  type BotIdentityModel,
  type BotOperationModel,
  type PeopleCatalogModel,
  type PeopleConfigurationSourceModel,
  type PeopleHistoryModel,
  type PeopleSelectionModel,
  type ProviderPoolSelectionModel,
  type RoleDefinitionModel,
  toAgentModel,
  toAssignmentModel,
  toBotIdentityModel,
  toBotOperationModel,
  toPeopleCatalogModel,
  toPeopleConfigurationSourceModel,
  toPeopleHistoryModel,
  toPeopleSelectionModel,
  toProviderPoolSelectionModel,
  toRoleDefinitionModel,
} from "./model";

export const usePeopleStore = defineStore("people", () => {
  const roleDefinitions = reactive(remoteState<RoleDefinitionModel[]>([]));
  const agents = reactive(remoteState<AgentModel[]>([]));
  const botIdentities = reactive(remoteState<BotIdentityModel[]>([]));
  const botOperation = reactive(remoteState<BotOperationModel | null>(null));
  const assignments = reactive(remoteState<AssignmentModel[]>([]));
  const rooms = reactive(remoteState<PeopleSelectionModel[]>([]));
  const roleImageRecipes = reactive(remoteState<PeopleSelectionModel[]>([]));
  const instructionSets = reactive(remoteState<PeopleSelectionModel[]>([]));
  const pools = reactive(remoteState<ProviderPoolSelectionModel[]>([]));
  const history = reactive(remoteState<PeopleHistoryModel[]>([]));
  const agentHistory = reactive(remoteState<AgentModel[]>([]));
  const ownerCatalog = reactive(remoteState<PeopleCatalogModel | null>(null));
  const configurationSource = reactive(
    remoteState<PeopleConfigurationSourceModel | null>(null),
  );
  const runtime = createFeatureRuntime();

  const loadPeople = () =>
    Promise.all([
      runtime.loadInto(
        roleDefinitions,
        async () =>
          (await fetchRoleDefinitions()).resources.map(toRoleDefinitionModel),
        (items) => items.length === 0,
      ),
      runtime.loadInto(
        agents,
        async () => (await fetchAgents()).agents.map(toAgentModel),
        (items) => items.length === 0,
      ),
      runtime.loadInto(
        botIdentities,
        async () =>
          (await fetchBotIdentities()).identities.map(toBotIdentityModel),
        (items) => items.length === 0,
      ),
      runtime.loadInto(
        assignments,
        async () => (await fetchAssignments()).resources.map(toAssignmentModel),
        (items) => items.length === 0,
      ),
      runtime.loadInto(
        rooms,
        async () =>
          (await fetchResources("CHAT")).resources.map(toPeopleSelectionModel),
        (items) => items.length === 0,
      ),
      runtime.loadInto(
        roleImageRecipes,
        async () =>
          (await fetchResources("ROLE_IMAGE_RECIPE")).resources.map(
            toPeopleSelectionModel,
          ),
        (items) => items.length === 0,
      ),
      runtime.loadInto(
        instructionSets,
        async () =>
          (await fetchInstructionSets()).resources.map(toPeopleSelectionModel),
        (items) => items.length === 0,
      ),
      runtime.loadInto(
        pools,
        async () =>
          (await fetchProviderPools()).pools.map(toProviderPoolSelectionModel),
        (items) => items.length === 0,
      ),
      runtime.loadInto(
        ownerCatalog,
        async () => toPeopleCatalogModel(await fetchOwnerCatalog()),
        (value) => value === null,
      ),
    ]).then(() => undefined);

  const sendRole = (body: RoleDefinitionCommand, version?: number) =>
    runtime.mutate(
      () => commandRoleDefinition(body, version),
      loadPeople,
      roleDefinitions,
    );
  const saveRoleDraft = (
    value: RoleDefinitionModel | null,
    draft: Required<
      Pick<
        RoleDefinitionCommand,
        | "name"
        | "stableKey"
        | "description"
        | "capabilities"
        | "allowedTargetRoleDefinitionRefs"
      >
    > &
      Pick<
        RoleDefinitionCommand,
        | "roleImageRecipeRef"
        | "roleImageRecipeVersion"
        | "roleImageRecipeSha256"
      >,
  ) =>
    sendRole(
      {
        action: value ? "UPDATE" : "CREATE",
        ...(value ? { resourceRef: value.id } : {}),
        ...draft,
      },
      value?.version,
    );
  const executeRoleAction = (
    value: RoleDefinitionModel,
    action: "ARCHIVE" | "DELETE" | "PAUSE" | "RESUME",
  ) => sendRole({ action, resourceRef: value.id }, value.version);
  const sendAgent = (body: AgentCommand, version?: number) =>
    runtime.mutate(() => commandAgent(body, version), loadPeople, agents);
  const saveAgentDraft = (
    value: AgentModel | null,
    draft: Required<
      Pick<
        AgentCommand,
        | "name"
        | "stableKey"
        | "runtimeSelectionKey"
        | "instructionSetStableKey"
        | "providerPoolStableKey"
        | "capabilities"
        | "enabled"
      >
    >,
  ) =>
    sendAgent(
      {
        action: value ? "UPDATE" : "CREATE",
        ...(value ? { resourceRef: value.agentRef } : {}),
        ...draft,
      },
      value?.version,
    );
  const executeAgentAction = (
    value: AgentModel,
    action: "ARCHIVE" | "DELETE" | "PAUSE" | "RESUME" | "ENABLE" | "DISABLE",
  ) => sendAgent({ action, resourceRef: value.agentRef }, value.version);
  const saveBotIdentity = async (
    agentRef: string,
    version: number,
    body: AgentBotIdentityCommand,
  ) => {
    const ok = await runtime.mutate(
      () => commandBotIdentity(agentRef, version, body),
      loadPeople,
      agents,
    );
    if (ok || !runtime.mutationProblem.value?.retryable) return ok;
    const scope = `agent-bot:${agentRef}:${body.action}`;
    const key = await pendingMutationKey(scope, body, version);
    if (!key) return false;
    try {
      const operation = await fetchBotOperation(agentRef, body.action, key);
      botOperation.data = toBotOperationModel(operation);
      botOperation.phase = "ready";
      if (
        operation.state === "BOUND" ||
        operation.state === "REVOKED" ||
        operation.state === "REPAIR_REQUIRED"
      )
        await completeMutationIntent(scope, body, version);
      await loadPeople();
      return true;
    } catch {
      return false;
    }
  };
  const createAndBindBotIdentity = (
    agent: AgentModel,
    usernameIntent: string,
    displayName: string,
  ) =>
    saveBotIdentity(agent.agentRef, agent.version, {
      action: "CREATE_AND_BIND",
      usernameIntent,
      displayName,
    });
  const bindBotIdentity = (agent: AgentModel, identitySelector: string) => {
    const action = agent.botIdentity.status === "BOUND" ? "REBIND" : "BIND";
    return saveBotIdentity(agent.agentRef, agent.version, {
      action,
      identitySelector,
      ...(action === "REBIND"
        ? { expectedProviderGeneration: agent.botIdentity.providerGeneration }
        : {}),
    });
  };
  const revokeBotIdentity = (agent: AgentModel) =>
    saveBotIdentity(agent.agentRef, agent.version, {
      action: "REVOKE",
      expectedProviderGeneration: agent.botIdentity.providerGeneration,
    });
  const assignAgent = (
    name: string,
    agentStableKey: string,
    roomStableKey: string,
  ) =>
    runtime.mutate(
      () =>
        commandAssignment({
          action: "ASSIGN",
          name,
          agentStableKey,
          roomStableKey,
        }),
      loadPeople,
      assignments,
    );
  const unassignAgent = (value: AssignmentModel) =>
    runtime.mutate(
      () =>
        commandAssignment(
          { action: "UNASSIGN", resourceRef: value.id },
          value.version,
        ),
      loadPeople,
      assignments,
    );
  const loadRoleHistory = (resourceRef: string) =>
    runtime.loadInto(
      history,
      async () =>
        (await fetchRoleDefinitionHistory(resourceRef)).entries.map(
          toPeopleHistoryModel,
        ),
      (items) => items.length === 0,
    );
  const loadAgentHistory = (resourceRef: string) =>
    runtime.loadInto(
      agentHistory,
      async () =>
        (await fetchAgentHistory(resourceRef)).entries.map((entry) =>
          toAgentModel(entry.agent),
        ),
      (items) => items.length === 0,
    );
  const loadAssignmentHistory = (resourceRef: string) =>
    runtime.loadInto(
      history,
      async () =>
        (await fetchAssignmentHistory(resourceRef)).entries.map(
          toPeopleHistoryModel,
        ),
      (items) => items.length === 0,
    );
  const loadConfigurationSource = (
    resourceRef: string,
    kind: "ROLE_DEFINITION" | "AGENT",
  ) => {
    configurationSource.data = null;
    return runtime.loadInto(
      configurationSource,
      async () =>
        toPeopleConfigurationSourceModel(
          await fetchConfigurationSource(resourceRef, kind),
        ),
      (value) => value === null,
    );
  };

  function reset(): void {
    runtime.invalidate();
    resetRemoteState(roleDefinitions, []);
    resetRemoteState(agents, []);
    resetRemoteState(botIdentities, []);
    resetRemoteState(botOperation, null);
    resetRemoteState(assignments, []);
    resetRemoteState(rooms, []);
    resetRemoteState(roleImageRecipes, []);
    resetRemoteState(instructionSets, []);
    resetRemoteState(pools, []);
    resetRemoteState(history, []);
    resetRemoteState(agentHistory, []);
    resetRemoteState(ownerCatalog, null);
    resetRemoteState(configurationSource, null);
  }

  return {
    roleDefinitions,
    agents,
    botIdentities,
    botOperation,
    assignments,
    rooms,
    roleImageRecipes,
    instructionSets,
    pools,
    history,
    agentHistory,
    ownerCatalog,
    configurationSource,
    mutationProblem: runtime.mutationProblem,
    mutating: runtime.mutating,
    loadPeople,
    saveRoleDraft,
    executeRoleAction,
    saveAgentDraft,
    executeAgentAction,
    createAndBindBotIdentity,
    bindBotIdentity,
    revokeBotIdentity,
    assignAgent,
    unassignAgent,
    loadRoleHistory,
    loadAgentHistory,
    loadAssignmentHistory,
    loadConfigurationSource,
    reset,
  };
});
