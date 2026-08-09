import { defineStore } from "pinia";
import { reactive, ref } from "vue";

import {
  cancelAuthorization,
  commandAgent,
  commandAssignment,
  commandBotIdentity,
  commandIncident,
  commandInstructionSet,
  commandProviderPool,
  commandRoleDefinition,
  commandRun,
  commandWorkspaceBackup,
  commandWorkspaceRestore,
  compareInstructionVersions,
  createOwnerSchedule,
  createTeam,
  decideApproval,
  downloadAudit,
  fetchAgentHistory,
  fetchAgents,
  fetchAssignmentHistory,
  fetchAssignments,
  fetchAuthorization,
  fetchBotIdentities,
  fetchConfigurationDiff,
  fetchConfigurationSource,
  fetchConnections,
  fetchHealthSeries,
  fetchIncident,
  fetchIncidentTimeline,
  fetchInstructionHistory,
  fetchInstructionSets,
  fetchIntegrationApprovals,
  fetchIntegrationConfigurations,
  fetchIntegrationDefinitions,
  fetchIntegrationTest,
  fetchMattermostBinding,
  fetchMattermostTeams,
  fetchOwnerCatalog,
  fetchOwnerSchedules,
  fetchProviderPools,
  fetchProviders,
  fetchRoleDefinitionHistory,
  fetchRoleDefinitions,
  fetchRunArtifacts,
  fetchRunDetail,
  fetchRunLineage,
  fetchRunTimeline,
  fetchScheduleSelectors,
  fetchWorkspaceBackups,
  fetchWorkspaceRestores,
  linkTeam,
  reauthorizeConnection,
  relinkTeam,
  restartAuthorization,
  revokeConnection,
  saveIntegrationConfiguration,
  startAuthorization,
  testIntegration,
  unlinkTeam,
  updateOwnerSchedule,
} from "@/shared/api/adapters/owner-control";
import type {
  AgentBotIdentity,
  AgentBotIdentityCommand,
  AgentCommand,
  AgentView,
  BindScheduleConfiguration,
  ConfigurationDiff,
  ConfigurationSourceDetail,
  CreateMattermostTeam,
  CreateScheduleFromSelections,
  HealthObservation,
  IncidentCommand,
  IncidentHistoryEntry,
  IncidentView,
  InstructionSetCommand,
  InstructionSetComparison,
  IntegrationApproval,
  IntegrationConfiguration,
  IntegrationDefinition,
  IntegrationTestReceipt,
  MattermostTeam,
  MattermostTeamBinding,
  MattermostMappingOperation,
  OwnerConfigurationCatalog,
  OwnerScheduleView,
  Provider,
  ProviderAuthorization,
  ProviderConnection,
  ProviderPoolCommand,
  ProviderPoolView,
  RelinkMattermostTeam,
  Resource,
  ResourceHistoryEntry,
  ResourceKind,
  RoleDefinitionCommand,
  RunArtifactView,
  RunCommand,
  RunDetail,
  RunLineage,
  RunTimelineEntry,
  ScheduleSelectorCatalog,
  TestIntegrationConnection,
  WorkspaceBackupCommand,
  WorkspaceRestoreCommand,
  WorkspaceRestoreView,
} from "@/shared/api/generated/openapi/types.gen";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import { fetchResources } from "@/shared/api/adapters/resources";
import {
  beginRequest,
  failRequest,
  finishRequest,
  invalidate,
  remoteState,
  type RemoteState,
} from "@/shared/lib/remote";

export const useOwnerControlStore = defineStore("owner-control", () => {
  const teams = reactive(remoteState<MattermostTeam[]>([]));
  const teamBinding = reactive(remoteState<MattermostTeamBinding | null>(null));
  const teamOperation = reactive(
    remoteState<MattermostMappingOperation | null>(null),
  );
  const roleDefinitions = reactive(remoteState<Resource[]>([]));
  const agents = reactive(remoteState<AgentView[]>([]));
  const botIdentities = reactive(remoteState<AgentBotIdentity[]>([]));
  const assignments = reactive(remoteState<Resource[]>([]));
  const rooms = reactive(remoteState<Resource[]>([]));
  const instructionSets = reactive(remoteState<Resource[]>([]));
  const history = reactive(remoteState<ResourceHistoryEntry[]>([]));
  const agentHistory = reactive(remoteState<AgentView[]>([]));
  const instructionComparison = reactive(
    remoteState<InstructionSetComparison | null>(null),
  );
  const ownerCatalog = reactive(
    remoteState<OwnerConfigurationCatalog | null>(null),
  );
  const providers = reactive(remoteState<Provider[]>([]));
  const authorization = reactive(
    remoteState<ProviderAuthorization | null>(null),
  );
  const connections = reactive(remoteState<ProviderConnection[]>([]));
  const pools = reactive(remoteState<ProviderPoolView[]>([]));
  const integrationDefinitions = reactive(
    remoteState<IntegrationDefinition[]>([]),
  );
  const integrationConfigurations = reactive(
    remoteState<IntegrationConfiguration[]>([]),
  );
  const approvals = reactive(remoteState<IntegrationApproval[]>([]));
  const integrationTest = reactive(
    remoteState<IntegrationTestReceipt | null>(null),
  );
  const scheduleSelectors = reactive(
    remoteState<ScheduleSelectorCatalog | null>(null),
  );
  const schedules = reactive(remoteState<OwnerScheduleView[]>([]));
  const artifacts = reactive(remoteState<Resource[]>([]));
  const runDetail = reactive(remoteState<RunDetail | null>(null));
  const runTimeline = reactive(remoteState<RunTimelineEntry[]>([]));
  const runLineage = reactive(remoteState<RunLineage | null>(null));
  const runArtifacts = reactive(remoteState<RunArtifactView[]>([]));
  const incident = reactive(remoteState<IncidentView | null>(null));
  const incidentHistory = reactive(remoteState<IncidentHistoryEntry[]>([]));
  const health = reactive(remoteState<HealthObservation[]>([]));
  const workspaceBackups = reactive(remoteState<Resource[]>([]));
  const workspaceRestores = reactive(remoteState<WorkspaceRestoreView[]>([]));
  const configurationDiff = reactive(
    remoteState<ConfigurationDiff | null>(null),
  );
  const configurationSource = reactive(
    remoteState<ConfigurationSourceDetail | null>(null),
  );
  const mutationProblem = ref<AppProblem | null>(null);
  const mutating = ref(false);
  let mutationGeneration = 0;

  async function loadInto<T>(
    state: RemoteState<T>,
    loader: () => Promise<T>,
    empty: (value: T) => boolean,
  ): Promise<void> {
    const request = beginRequest(state);
    try {
      const data = await loader();
      finishRequest(state, request, data, empty(data));
    } catch (error) {
      failRequest(state, request, asProblem(error));
    }
  }

  async function mutate(
    operation: () => Promise<unknown>,
    reload: () => Promise<void>,
    conflictState?: RemoteState<unknown>,
  ): Promise<boolean> {
    const generation = ++mutationGeneration;
    mutationProblem.value = null;
    mutating.value = true;
    try {
      await operation();
      if (generation !== mutationGeneration) return false;
      await reload();
      return generation === mutationGeneration;
    } catch (error) {
      if (generation !== mutationGeneration) return false;
      mutationProblem.value = asProblem(error);
      if (mutationProblem.value.kind === "conflict" && conflictState)
        conflictState.phase = "conflict";
      return false;
    } finally {
      if (generation === mutationGeneration) mutating.value = false;
    }
  }

  const loadTeams = () =>
    Promise.all([
      loadInto(
        teams,
        async () => (await fetchMattermostTeams()).teams,
        (items) => items.length === 0,
      ),
      loadInto(teamBinding, fetchMattermostBinding, (value) => value === null),
    ]).then(() => undefined);

  const addTeam = (body: CreateMattermostTeam) =>
    mutate(() => createTeam(body), loadTeams, teams);
  const bindTeam = (selector: string) =>
    mutate(
      async () => {
        teamOperation.data = (await linkTeam(selector)).operation;
        teamOperation.phase = "ready";
      },
      loadTeams,
      teamBinding,
    );
  const rebindTeam = (body: RelinkMattermostTeam, version: number) =>
    mutate(
      async () => {
        teamOperation.data = (await relinkTeam(body, version)).operation;
        teamOperation.phase = "ready";
      },
      loadTeams,
      teamBinding,
    );
  const removeTeamBinding = (version: number, generation: number) =>
    mutate(
      async () => {
        teamOperation.data = (await unlinkTeam(version, generation)).operation;
        teamOperation.phase = "ready";
      },
      loadTeams,
      teamBinding,
    );

  const loadPeople = () =>
    Promise.all([
      loadInto(
        roleDefinitions,
        async () => (await fetchRoleDefinitions()).resources,
        (items) => items.length === 0,
      ),
      loadInto(
        agents,
        async () => (await fetchAgents()).agents,
        (items) => items.length === 0,
      ),
      loadInto(
        botIdentities,
        async () => (await fetchBotIdentities()).identities,
        (items) => items.length === 0,
      ),
      loadInto(
        assignments,
        async () => (await fetchAssignments()).resources,
        (items) => items.length === 0,
      ),
      loadInto(
        rooms,
        async () => (await fetchResources("CHAT")).resources,
        (items) => items.length === 0,
      ),
      loadInto(ownerCatalog, fetchOwnerCatalog, (value) => value === null),
    ]).then(() => undefined);

  const saveRole = (body: RoleDefinitionCommand, version?: number) =>
    mutate(
      () => commandRoleDefinition(body, version),
      loadPeople,
      roleDefinitions,
    );
  const saveAgent = (body: AgentCommand, version?: number) =>
    mutate(() => commandAgent(body, version), loadPeople, agents);
  const saveBotIdentity = (
    agentRef: string,
    version: number,
    body: AgentBotIdentityCommand,
  ) =>
    mutate(
      () => commandBotIdentity(agentRef, version, body),
      loadPeople,
      agents,
    );
  const saveAssignment = (
    body: Parameters<typeof commandAssignment>[0],
    version?: number,
  ) => mutate(() => commandAssignment(body, version), loadPeople, assignments);

  const loadRoleHistory = (resourceRef: string) =>
    loadInto(
      history,
      async () => (await fetchRoleDefinitionHistory(resourceRef)).entries,
      (items) => items.length === 0,
    );
  const loadAgentHistory = (resourceRef: string) =>
    loadInto(
      agentHistory,
      async () =>
        (await fetchAgentHistory(resourceRef)).entries.map(
          (entry) => entry.agent,
        ),
      (items) => items.length === 0,
    );
  const loadAssignmentHistory = (resourceRef: string) =>
    loadInto(
      history,
      async () => (await fetchAssignmentHistory(resourceRef)).entries,
      (items) => items.length === 0,
    );

  const loadInstructions = () =>
    loadInto(
      instructionSets,
      async () => (await fetchInstructionSets()).resources,
      (items) => items.length === 0,
    );
  const saveInstruction = (body: InstructionSetCommand, version?: number) =>
    mutate(
      () => commandInstructionSet(body, version),
      loadInstructions,
      instructionSets,
    );
  const loadInstructionHistory = (resourceRef: string) =>
    loadInto(
      history,
      async () => (await fetchInstructionHistory(resourceRef)).entries,
      (items) => items.length === 0,
    );
  const compareInstructions = (
    resourceRef: string,
    leftVersion: number,
    rightVersion: number,
  ) =>
    loadInto(
      instructionComparison,
      () => compareInstructionVersions(resourceRef, leftVersion, rightVersion),
      (value) => value === null,
    );

  const loadProviders = () =>
    Promise.all([
      loadInto(
        providers,
        async () => (await fetchProviders()).providers,
        (items) => items.length === 0,
      ),
      loadInto(
        connections,
        async () => (await fetchConnections()).connections,
        (items) => items.length === 0,
      ),
      loadInto(
        pools,
        async () => (await fetchProviderPools()).pools,
        (items) => items.length === 0,
      ),
    ]).then(() => undefined);

  const beginAuthorization = async (
    body: Parameters<typeof startAuthorization>[0],
  ) => {
    const ok = await mutate(
      async () => {
        const result = await startAuthorization(body);
        authorization.data = result;
        authorization.phase = "ready";
      },
      loadProviders,
      connections,
    );
    return ok;
  };
  const refreshAuthorization = (authorizationRef: string) =>
    loadInto(
      authorization,
      () => fetchAuthorization(authorizationRef),
      (value) => value === null,
    );
  const newAuthorizationCode = (value: ProviderAuthorization) =>
    mutate(
      async () => {
        authorization.data = await restartAuthorization(
          value.authorizationRef,
          value.version,
        );
        authorization.phase = "ready";
      },
      loadProviders,
      authorization,
    );
  const stopAuthorization = (value: ProviderAuthorization) =>
    mutate(
      async () => {
        authorization.data = await cancelAuthorization(
          value.authorizationRef,
          value.version,
        );
        authorization.phase = "ready";
      },
      loadProviders,
      authorization,
    );
  const revokeProvider = (value: ProviderConnection) =>
    mutate(
      () =>
        revokeConnection(value.connectionRef, value.version, value.generation),
      loadProviders,
      connections,
    );
  const reauthorizeProvider = (value: ProviderConnection) =>
    mutate(
      async () => {
        authorization.data = await reauthorizeConnection(
          value.connectionRef,
          value.version,
          value.generation,
        );
        authorization.phase = "ready";
      },
      loadProviders,
      connections,
    );
  const savePool = (body: ProviderPoolCommand, version?: number) =>
    mutate(() => commandProviderPool(body, version), loadProviders, pools);

  const loadIntegrations = () =>
    Promise.all([
      loadInto(
        integrationDefinitions,
        async () => (await fetchIntegrationDefinitions()).definitions,
        (items) => items.length === 0,
      ),
      loadInto(
        integrationConfigurations,
        async () => (await fetchIntegrationConfigurations()).configurations,
        (items) => items.length === 0,
      ),
      loadInto(
        approvals,
        async () => (await fetchIntegrationApprovals()).approvals,
        (items) => items.length === 0,
      ),
      loadInto(
        connections,
        async () => (await fetchConnections()).connections,
        (items) => items.length === 0,
      ),
    ]).then(() => undefined);
  const saveIntegration = (
    body: Parameters<typeof saveIntegrationConfiguration>[0],
    version?: number,
  ) =>
    mutate(
      () => saveIntegrationConfiguration(body, version),
      loadIntegrations,
      integrationConfigurations,
    );
  const runIntegrationTest = async (body: TestIntegrationConnection) => {
    mutationProblem.value = null;
    mutating.value = true;
    try {
      integrationTest.data = await testIntegration(body);
      integrationTest.phase = "ready";
      return true;
    } catch (error) {
      mutationProblem.value = asProblem(error);
      return false;
    } finally {
      mutating.value = false;
    }
  };
  const refreshIntegrationTest = (testRef: string) =>
    loadInto(
      integrationTest,
      () => fetchIntegrationTest(testRef),
      (value) => value === null,
    );
  const reviewApproval = (
    value: IntegrationApproval,
    decision: "APPROVE" | "REJECT",
    reasonCode: string,
  ) =>
    mutate(
      () =>
        decideApproval(value.approvalRef, value.version, {
          expectedRequestHash: value.requestHash,
          decision,
          reasonCode,
        }),
      loadIntegrations,
      approvals,
    );

  const loadSchedules = () =>
    Promise.all([
      loadInto(
        scheduleSelectors,
        fetchScheduleSelectors,
        (value) => value === null,
      ),
      loadInto(
        schedules,
        async () => (await fetchOwnerSchedules()).items,
        (items) => items.length === 0,
      ),
      loadInto(
        artifacts,
        async () => (await fetchResources("ARTIFACT")).resources,
        (items) => items.length === 0,
      ),
    ]).then(() => undefined);
  const addSchedule = (body: CreateScheduleFromSelections) =>
    mutate(() => createOwnerSchedule(body), loadSchedules, schedules);
  const saveSchedule = (
    value: OwnerScheduleView,
    body: BindScheduleConfiguration,
  ) =>
    mutate(
      () => updateOwnerSchedule(value.scheduleRef, value.version, body),
      loadSchedules,
      schedules,
    );

  const loadRun = (runRef: string) =>
    Promise.all([
      loadInto(
        runDetail,
        () => fetchRunDetail(runRef),
        (value) => value === null,
      ),
      loadInto(
        runTimeline,
        async () => (await fetchRunTimeline(runRef)).entries,
        (items) => items.length === 0,
      ),
      loadInto(
        runLineage,
        () => fetchRunLineage(runRef),
        (value) => value === null,
      ),
      loadInto(
        runArtifacts,
        async () => (await fetchRunArtifacts(runRef)).artifacts,
        (items) => items.length === 0,
      ),
    ]).then(() => undefined);
  const executeRunAction = (body: RunCommand) => {
    const value = runDetail.data?.run;
    if (!value) return Promise.resolve(false);
    return mutate(
      () => commandRun(value.runRef, value.version, body),
      () => loadRun(value.runRef),
      runDetail,
    );
  };

  const loadIncident = (incidentRef: string) =>
    Promise.all([
      loadInto(
        incident,
        () => fetchIncident(incidentRef),
        (value) => value === null,
      ),
      loadInto(
        incidentHistory,
        async () => (await fetchIncidentTimeline(incidentRef)).entries,
        (items) => items.length === 0,
      ),
    ]).then(() => undefined);
  const executeIncidentAction = (body: IncidentCommand) => {
    const value = incident.data;
    if (!value) return Promise.resolve(false);
    return mutate(
      () => commandIncident(value.incidentRef, value.version, body),
      () => loadIncident(value.incidentRef),
      incident,
    );
  };

  const loadHealth = () =>
    loadInto(
      health,
      async () => (await fetchHealthSeries()).observations,
      (items) => items.length === 0,
    );

  const loadWorkspaceRecovery = () =>
    Promise.all([
      loadInto(
        workspaceBackups,
        async () => (await fetchWorkspaceBackups()).resources,
        (items) => items.length === 0,
      ),
      loadInto(
        workspaceRestores,
        async () => (await fetchWorkspaceRestores()).restores,
        (items) => items.length === 0,
      ),
    ]).then(() => undefined);
  const saveWorkspaceBackup = (
    body: WorkspaceBackupCommand,
    version?: number,
  ) =>
    mutate(
      () => commandWorkspaceBackup(body, version),
      loadWorkspaceRecovery,
      workspaceBackups,
    );
  const saveWorkspaceRestore = (
    body: WorkspaceRestoreCommand,
    version?: number,
  ) =>
    mutate(
      () => commandWorkspaceRestore(body, version),
      loadWorkspaceRecovery,
      workspaceRestores,
    );

  const loadConfigurationDiff = (
    instructionSetRef: string,
    leftVersion: number,
    rightVersion: number,
  ) =>
    loadInto(
      configurationDiff,
      () =>
        fetchConfigurationDiff(instructionSetRef, leftVersion, rightVersion),
      (value) => value === null,
    );
  const loadConfigurationSource = (
    resourceRef: string,
    kind: "ROLE_DEFINITION" | "AGENT" | "INSTRUCTION_SET" | "PROVIDER_POOL",
  ) => {
    configurationSource.data = null;
    return loadInto(
      configurationSource,
      () => fetchConfigurationSource(resourceRef, kind),
      (value) => value === null,
    );
  };

  const exportAuditFile = (filters?: {
    resourceKind?: ResourceKind;
    resourceRef?: string;
    action?: string;
  }) => downloadAudit(filters);

  function replaceTeams(items: MattermostTeam[]): void {
    invalidate(teams);
    teams.data = items;
    teams.phase = items.length ? "ready" : "empty";
  }
  function replaceConnections(items: ProviderConnection[]): void {
    invalidate(connections);
    connections.data = items;
    connections.phase = items.length ? "ready" : "empty";
  }
  function replaceIntegrations(items: IntegrationConfiguration[]): void {
    invalidate(integrationConfigurations);
    integrationConfigurations.data = items;
    integrationConfigurations.phase = items.length ? "ready" : "empty";
  }
  function replaceApprovals(items: IntegrationApproval[]): void {
    invalidate(approvals);
    approvals.data = items;
    approvals.phase = items.length ? "ready" : "empty";
  }
  function replaceBackups(items: Resource[]): void {
    invalidate(workspaceBackups);
    workspaceBackups.data = items.filter(
      (item) => item.kind === "WORKSPACE_BACKUP",
    );
    workspaceBackups.phase = workspaceBackups.data.length ? "ready" : "empty";
  }
  function replaceHealth(items: HealthObservation[]): void {
    invalidate(health);
    health.data = items;
    health.phase = items.length ? "ready" : "empty";
  }

  return {
    teams,
    teamBinding,
    teamOperation,
    roleDefinitions,
    agents,
    botIdentities,
    assignments,
    rooms,
    instructionSets,
    history,
    agentHistory,
    instructionComparison,
    ownerCatalog,
    providers,
    authorization,
    connections,
    pools,
    integrationDefinitions,
    integrationConfigurations,
    approvals,
    integrationTest,
    scheduleSelectors,
    schedules,
    artifacts,
    runDetail,
    runTimeline,
    runLineage,
    runArtifacts,
    incident,
    incidentHistory,
    health,
    workspaceBackups,
    workspaceRestores,
    configurationDiff,
    configurationSource,
    mutationProblem,
    mutating,
    loadTeams,
    addTeam,
    bindTeam,
    rebindTeam,
    removeTeamBinding,
    loadPeople,
    saveRole,
    saveAgent,
    saveBotIdentity,
    saveAssignment,
    loadRoleHistory,
    loadAgentHistory,
    loadAssignmentHistory,
    loadInstructions,
    saveInstruction,
    loadInstructionHistory,
    compareInstructions,
    loadProviders,
    beginAuthorization,
    refreshAuthorization,
    newAuthorizationCode,
    stopAuthorization,
    revokeProvider,
    reauthorizeProvider,
    savePool,
    loadIntegrations,
    saveIntegration,
    runIntegrationTest,
    refreshIntegrationTest,
    reviewApproval,
    loadSchedules,
    addSchedule,
    saveSchedule,
    loadRun,
    executeRunAction,
    loadIncident,
    executeIncidentAction,
    loadHealth,
    loadWorkspaceRecovery,
    saveWorkspaceBackup,
    saveWorkspaceRestore,
    loadConfigurationDiff,
    loadConfigurationSource,
    exportAuditFile,
    replaceTeams,
    replaceConnections,
    replaceIntegrations,
    replaceApprovals,
    replaceBackups,
    replaceHealth,
  };
});
