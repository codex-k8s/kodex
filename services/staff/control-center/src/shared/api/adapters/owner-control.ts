import {
  bindScheduleConfiguration,
  cancelProviderAuthorization,
  compareInstructionSetVersions,
  configureIntegration,
  createMattermostTeam,
  createScheduleFromSelections,
  decideIntegrationApproval,
  deleteSchedule,
  exportAudit,
  getAgentAssignment,
  getAgentBotIdentity,
  getAgentBotIdentityOperation,
  getAgentBotIdentityProviderReadback,
  getAgent,
  getConfigurationDiff,
  getConfigurationSourceDetail,
  getHealthSeries,
  getIncident,
  getInstructionSet,
  getIntegrationApproval,
  getIntegrationConfiguration,
  getIntegrationDefinition,
  getIntegrationTestReceipt,
  getMattermostTeamBinding,
  getMattermostTeamMappingOperation,
  getMattermostTeamProviderReadback,
  getOwnerConfigurationCatalog,
  getOwnerSchedule,
  getProvider,
  getProviderAuthorization,
  getProviderConnection,
  getProviderPool,
  getRoleDefinition,
  getRunDetail,
  getRunLineage,
  getWorkspaceBackup,
  getWorkspaceRestore,
  linkMattermostTeam,
  listAgentAssignmentHistory,
  listAgentAssignments,
  listAgentBotIdentities,
  listAgentHistory,
  listAgents,
  listIncidentHistory,
  listInstructionSetHistory,
  listInstructionSets,
  listIntegrationApprovals,
  listIntegrationConfigurations,
  listIntegrationDefinitions,
  listMattermostTeams,
  listOwnerSchedules,
  listProviderConnections,
  listProviderPools,
  listProviders,
  listRoleDefinitionHistory,
  listRoleDefinitions,
  listRunArtifacts,
  listRunTimeline,
  listScheduleSelectors,
  listScheduleOccurrences,
  listWorkspaceBackups,
  listWorkspaceRestores,
  manageAgentAssignment,
  manageAgentBotIdentity,
  manageAgent,
  manageIncident,
  manageInstructionSet,
  manageProviderPool,
  manageRoleDefinition,
  manageRun,
  manageWorkspaceBackup,
  manageWorkspaceRestore,
  reauthorizeProviderConnection,
  relinkMattermostTeam,
  restartProviderAuthorization,
  resolveScheduleRecovery,
  revokeProviderConnection,
  startProviderAuthorization,
  testIntegrationConnection,
  unlinkMattermostTeam,
  runScheduleNow,
} from "@/shared/api/generated/openapi/sdk.gen";
import type {
  AgentAssignmentCommand,
  AgentBotIdentityAction,
  AgentBotIdentityCommand,
  AgentCommand,
  BindScheduleConfiguration,
  ConfigureIntegration,
  CreateMattermostTeam,
  CreateScheduleFromSelections,
  DecideIntegrationApproval,
  IncidentCommand,
  InstructionSetCommand,
  ProviderPoolCommand,
  RelinkMattermostTeam,
  ResourceKind,
  ResolveScheduleRecovery,
  RoleDefinitionCommand,
  RunCommand,
  StartProviderAuthorization,
  TestIntegrationConnection,
  WorkspaceBackupCommand,
  WorkspaceRestoreCommand,
} from "@/shared/api/generated/openapi/types.gen";
import { requestSignal } from "@/shared/api/client";
import { collectAllPages } from "@/shared/api/pagination";
import { asProblem, unwrap } from "@/shared/api/problem";
import { executeMutation } from "@/shared/lib/identity";

type BasicMutationHeaders = {
  "X-CSRF-Token": string;
  "Idempotency-Key": string;
};
type RequiredMutationHeaders = BasicMutationHeaders & { "If-Match": string };
type OptionalMutationHeaders = BasicMutationHeaders & { "If-Match"?: string };

const readOptions = () => ({ signal: requestSignal() });

export const fetchMattermostTeams = async () => {
  const { values } = await collectAllPages(
    async (pageToken) =>
      (
        await unwrap(
          listMattermostTeams({
            query: { pageSize: 100, ...(pageToken ? { pageToken } : {}) },
            ...readOptions(),
          }),
        )
      ).data,
    (page) => page.teams,
  );
  return { teams: values };
};

export const fetchMattermostBinding = async () => {
  try {
    return (await unwrap(getMattermostTeamBinding(readOptions()))).data;
  } catch (error) {
    if (asProblem(error).status === 404) return null;
    throw error;
  }
};

export const createTeam = async (body: CreateMattermostTeam) =>
  (
    await executeMutation(
      "mattermost-team:create",
      body,
      undefined,
      (headers) =>
        createMattermostTeam({
          body,
          headers: headers as BasicMutationHeaders,
          ...readOptions(),
        }),
    )
  ).data;

export const linkTeam = async (selector: string) =>
  (
    await executeMutation(
      "mattermost-binding:bind",
      { selector },
      undefined,
      (headers) =>
        linkMattermostTeam({
          body: { selector },
          headers: headers as BasicMutationHeaders,
          ...readOptions(),
        }),
    )
  ).data;

export const relinkTeam = async (body: RelinkMattermostTeam, version: number) =>
  (
    await executeMutation(
      "mattermost-binding:relink",
      body,
      version,
      (headers) =>
        relinkMattermostTeam({
          body,
          headers: headers as RequiredMutationHeaders,
          ...readOptions(),
        }),
    )
  ).data;

export const unlinkTeam = async (version: number, generation: number) =>
  (
    await executeMutation(
      "mattermost-binding:unlink",
      { generation },
      version,
      (headers) =>
        unlinkMattermostTeam({
          query: { generation },
          headers: headers as RequiredMutationHeaders,
          ...readOptions(),
        }),
    )
  ).data;

export const fetchTeamOperation = async (
  action: "BIND" | "RELINK" | "UNLINK",
  idempotencyKey: string,
) =>
  (
    await unwrap(
      getMattermostTeamMappingOperation({
        query: { action },
        headers: { "Idempotency-Key": idempotencyKey },
        ...readOptions(),
      }),
    )
  ).data;

export const fetchTeamProviderReadback = async (selector: string) =>
  (
    await unwrap(
      getMattermostTeamProviderReadback({
        path: { selector },
        ...readOptions(),
      }),
    )
  ).data;

export const fetchRoleDefinitions = async () => {
  const { values } = await collectAllPages(
    async (pageToken) =>
      (
        await unwrap(
          listRoleDefinitions({
            query: { pageSize: 100, ...(pageToken ? { pageToken } : {}) },
            ...readOptions(),
          }),
        )
      ).data,
    (page) => page.resources,
  );
  return { resources: values };
};

export const fetchRoleDefinition = async (resourceRef: string) =>
  (await unwrap(getRoleDefinition({ path: { resourceRef }, ...readOptions() })))
    .data;

export const fetchRoleDefinitionHistory = async (resourceRef: string) => {
  const { values } = await collectAllPages(
    async (pageToken) =>
      (
        await unwrap(
          listRoleDefinitionHistory({
            path: { resourceRef },
            query: { pageSize: 100, ...(pageToken ? { pageToken } : {}) },
            ...readOptions(),
          }),
        )
      ).data,
    (page) => page.entries,
  );
  return { entries: values };
};

export const commandRoleDefinition = async (
  body: RoleDefinitionCommand,
  version?: number,
) =>
  (
    await executeMutation(
      `role-definition:${body.action}:${body.resourceRef ?? body.stableKey ?? "new"}`,
      body,
      version,
      (headers) =>
        manageRoleDefinition({
          body,
          headers: headers as OptionalMutationHeaders,
          ...readOptions(),
        }),
    )
  ).data;

export const fetchAgents = async () => {
  const { values } = await collectAllPages(
    async (pageToken) =>
      (
        await unwrap(
          listAgents({
            query: { pageSize: 100, ...(pageToken ? { pageToken } : {}) },
            ...readOptions(),
          }),
        )
      ).data,
    (page) => page.agents,
  );
  return { agents: values };
};

export const fetchAgent = async (resourceRef: string) =>
  (await unwrap(getAgent({ path: { resourceRef }, ...readOptions() }))).data;

export const fetchAgentHistory = async (resourceRef: string) => {
  const { values } = await collectAllPages(
    async (pageToken) =>
      (
        await unwrap(
          listAgentHistory({
            path: { resourceRef },
            query: { pageSize: 100, ...(pageToken ? { pageToken } : {}) },
            ...readOptions(),
          }),
        )
      ).data,
    (page) => page.entries,
  );
  return { entries: values };
};

export const fetchOwnerCatalog = async () => {
  const result = await collectAllPages(
    async (pageToken) =>
      (
        await unwrap(
          getOwnerConfigurationCatalog({
            query: {
              pageSize: 100,
              ...(pageToken ? { pageToken } : {}),
            },
            ...readOptions(),
          }),
        )
      ).data,
    (page) => page.runtimeSelections,
  );
  const first = result.pages[0];
  if (!first) throw new Error("Owner configuration catalog is empty");
  for (const page of result.pages) {
    if (
      page.scheduleDefaults.digestSha256 !==
        first.scheduleDefaults.digestSha256 ||
      JSON.stringify(page.schedulePresets) !==
        JSON.stringify(first.schedulePresets)
    )
      throw new Error("Owner configuration catalog changed during pagination");
  }
  return {
    runtimeSelections: result.values,
    schedulePresets: first.schedulePresets,
    scheduleDefaults: first.scheduleDefaults,
  };
};

export const commandAgent = async (body: AgentCommand, version?: number) =>
  (
    await executeMutation(
      `agent:${body.action}:${body.resourceRef ?? body.stableKey ?? "new"}`,
      body,
      version,
      (headers) =>
        manageAgent({
          body,
          headers: headers as OptionalMutationHeaders,
          ...readOptions(),
        }),
    )
  ).data;

export const fetchBotIdentities = async () => {
  const { values } = await collectAllPages(
    async (pageToken) =>
      (
        await unwrap(
          listAgentBotIdentities({
            query: { pageSize: 100, ...(pageToken ? { pageToken } : {}) },
            ...readOptions(),
          }),
        )
      ).data,
    (page) => page.identities,
  );
  return { identities: values };
};

export const fetchBotIdentity = async (resourceRef: string) =>
  (
    await unwrap(
      getAgentBotIdentity({ path: { resourceRef }, ...readOptions() }),
    )
  ).data;

export const commandBotIdentity = async (
  agentRef: string,
  version: number,
  body: AgentBotIdentityCommand,
) =>
  (
    await executeMutation(
      `agent-bot:${agentRef}:${body.action}`,
      body,
      version,
      (headers) =>
        manageAgentBotIdentity({
          path: { resourceRef: agentRef },
          body,
          headers: headers as RequiredMutationHeaders,
          ...readOptions(),
        }),
    )
  ).data;

export const fetchBotOperation = async (
  agentRef: string,
  action: AgentBotIdentityAction,
  idempotencyKey: string,
) =>
  (
    await unwrap(
      getAgentBotIdentityOperation({
        path: { resourceRef: agentRef },
        query: { action },
        headers: { "Idempotency-Key": idempotencyKey },
        ...readOptions(),
      }),
    )
  ).data;

export const fetchBotProviderReadback = async (
  resourceRef: string,
  identitySelector: string,
) =>
  (
    await unwrap(
      getAgentBotIdentityProviderReadback({
        path: { resourceRef },
        query: { identitySelector },
        ...readOptions(),
      }),
    )
  ).data;

export const fetchAssignments = async () => {
  const { values } = await collectAllPages(
    async (pageToken) =>
      (
        await unwrap(
          listAgentAssignments({
            query: { pageSize: 100, ...(pageToken ? { pageToken } : {}) },
            ...readOptions(),
          }),
        )
      ).data,
    (page) => page.resources,
  );
  return { resources: values };
};

export const fetchAssignment = async (resourceRef: string) =>
  (
    await unwrap(
      getAgentAssignment({ path: { resourceRef }, ...readOptions() }),
    )
  ).data;

export const fetchAssignmentHistory = async (resourceRef: string) => {
  const { values } = await collectAllPages(
    async (pageToken) =>
      (
        await unwrap(
          listAgentAssignmentHistory({
            path: { resourceRef },
            query: { pageSize: 100, ...(pageToken ? { pageToken } : {}) },
            ...readOptions(),
          }),
        )
      ).data,
    (page) => page.entries,
  );
  return { entries: values };
};

export const commandAssignment = async (
  body: AgentAssignmentCommand,
  version?: number,
) =>
  (
    await executeMutation(
      `agent-assignment:${body.action}:${body.resourceRef ?? body.name ?? "new"}`,
      body,
      version,
      (headers) =>
        manageAgentAssignment({
          body,
          headers: headers as OptionalMutationHeaders,
          ...readOptions(),
        }),
    )
  ).data;

export const fetchInstructionSets = async () => {
  const { values } = await collectAllPages(
    async (pageToken) =>
      (
        await unwrap(
          listInstructionSets({
            query: { pageSize: 100, ...(pageToken ? { pageToken } : {}) },
            ...readOptions(),
          }),
        )
      ).data,
    (page) => page.resources,
  );
  return { resources: values };
};

export const fetchInstructionSet = async (resourceRef: string) =>
  (await unwrap(getInstructionSet({ path: { resourceRef }, ...readOptions() })))
    .data;

export const fetchInstructionHistory = async (resourceRef: string) => {
  const { values } = await collectAllPages(
    async (pageToken) =>
      (
        await unwrap(
          listInstructionSetHistory({
            path: { resourceRef },
            query: { pageSize: 100, ...(pageToken ? { pageToken } : {}) },
            ...readOptions(),
          }),
        )
      ).data,
    (page) => page.entries,
  );
  return { entries: values };
};

export const compareInstructionVersions = async (
  resourceRef: string,
  leftVersion: number,
  rightVersion: number,
) =>
  (
    await unwrap(
      compareInstructionSetVersions({
        path: { resourceRef },
        query: { leftVersion, rightVersion },
        ...readOptions(),
      }),
    )
  ).data;

export const commandInstructionSet = async (
  body: InstructionSetCommand,
  version?: number,
) =>
  (
    await executeMutation(
      `instruction-set:${body.action}:${body.resourceRef ?? body.stableKey ?? "new"}`,
      body,
      version,
      (headers) =>
        manageInstructionSet({
          body,
          headers: headers as OptionalMutationHeaders,
          ...readOptions(),
        }),
    )
  ).data;

export const fetchProviders = async () =>
  (await unwrap(listProviders(readOptions()))).data;

export const fetchProvider = async (
  providerRef: string,
  version: number,
  digestSha256: string,
) =>
  (
    await unwrap(
      getProvider({
        path: { providerRef },
        query: { version, digestSha256 },
        ...readOptions(),
      }),
    )
  ).data;

export const startAuthorization = async (body: StartProviderAuthorization) =>
  (
    await executeMutation(
      `provider-authorization:start:${body.connectionStableKey}`,
      body,
      undefined,
      (headers) =>
        startProviderAuthorization({
          body,
          headers: headers as BasicMutationHeaders,
          ...readOptions(),
        }),
    )
  ).data;

export const fetchAuthorization = async (authorizationRef: string) =>
  (
    await unwrap(
      getProviderAuthorization({
        path: { authorizationRef },
        ...readOptions(),
      }),
    )
  ).data;

export const restartAuthorization = async (
  authorizationRef: string,
  version: number,
) =>
  (
    await executeMutation(
      `provider-authorization:new-code:${authorizationRef}`,
      {},
      version,
      (headers) =>
        restartProviderAuthorization({
          path: { authorizationRef },
          headers: headers as RequiredMutationHeaders,
          ...readOptions(),
        }),
    )
  ).data;

export const cancelAuthorization = async (
  authorizationRef: string,
  version: number,
) =>
  (
    await executeMutation(
      `provider-authorization:cancel:${authorizationRef}`,
      {},
      version,
      (headers) =>
        cancelProviderAuthorization({
          path: { authorizationRef },
          headers: headers as RequiredMutationHeaders,
          ...readOptions(),
        }),
    )
  ).data;

export const fetchConnections = async () => {
  const { values } = await collectAllPages(
    async (pageToken) =>
      (
        await unwrap(
          listProviderConnections({
            query: { pageSize: 100, ...(pageToken ? { pageToken } : {}) },
            ...readOptions(),
          }),
        )
      ).data,
    (page) => page.connections,
  );
  return { connections: values };
};

export const fetchConnection = async (connectionRef: string) =>
  (
    await unwrap(
      getProviderConnection({ path: { connectionRef }, ...readOptions() }),
    )
  ).data;

export const revokeConnection = async (
  connectionRef: string,
  version: number,
  generation: number,
) =>
  (
    await executeMutation(
      `provider-connection:revoke:${connectionRef}`,
      { generation },
      version,
      (headers) =>
        revokeProviderConnection({
          path: { connectionRef },
          query: { generation },
          headers: headers as RequiredMutationHeaders,
          ...readOptions(),
        }),
    )
  ).data;

export const reauthorizeConnection = async (
  connectionRef: string,
  version: number,
  generation: number,
) =>
  (
    await executeMutation(
      `provider-connection:reauthorize:${connectionRef}`,
      { generation },
      version,
      (headers) =>
        reauthorizeProviderConnection({
          path: { connectionRef },
          query: { generation },
          headers: headers as RequiredMutationHeaders,
          ...readOptions(),
        }),
    )
  ).data;

export const fetchProviderPools = async () => {
  const { values } = await collectAllPages(
    async (pageToken) =>
      (
        await unwrap(
          listProviderPools({
            query: { pageSize: 100, ...(pageToken ? { pageToken } : {}) },
            ...readOptions(),
          }),
        )
      ).data,
    (page) => page.pools,
  );
  return { pools: values };
};

export const fetchProviderPool = async (poolRef: string) =>
  (await unwrap(getProviderPool({ path: { poolRef }, ...readOptions() }))).data;

export const commandProviderPool = async (
  body: ProviderPoolCommand,
  version?: number,
) =>
  (
    await executeMutation(
      `provider-pool:${body.action}:${body.poolRef ?? body.stableKey ?? "new"}`,
      body,
      version,
      (headers) =>
        manageProviderPool({
          body,
          headers: headers as OptionalMutationHeaders,
          ...readOptions(),
        }),
    )
  ).data;

export const fetchIntegrationDefinitions = async () =>
  (await unwrap(listIntegrationDefinitions(readOptions()))).data;

export const fetchIntegrationDefinition = async (
  definitionRef: string,
  version: number,
  digestSha256: string,
) =>
  (
    await unwrap(
      getIntegrationDefinition({
        path: { definitionRef },
        query: { version, digestSha256 },
        ...readOptions(),
      }),
    )
  ).data;

export const fetchIntegrationConfigurations = async () => {
  const { values } = await collectAllPages(
    async (pageToken) =>
      (
        await unwrap(
          listIntegrationConfigurations({
            query: { pageSize: 100, ...(pageToken ? { pageToken } : {}) },
            ...readOptions(),
          }),
        )
      ).data,
    (page) => page.configurations,
  );
  return { configurations: values };
};

export const fetchIntegrationConfiguration = async (configurationRef: string) =>
  (
    await unwrap(
      getIntegrationConfiguration({
        path: { configurationRef },
        ...readOptions(),
      }),
    )
  ).data;

export const saveIntegrationConfiguration = async (
  body: ConfigureIntegration,
  version?: number,
) =>
  (
    await executeMutation(
      `integration-configuration:${body.configurationRef ?? body.stableKey}`,
      body,
      version,
      (headers) =>
        configureIntegration({
          body,
          headers: headers as OptionalMutationHeaders,
          ...readOptions(),
        }),
    )
  ).data;

export const testIntegration = async (body: TestIntegrationConnection) =>
  (
    await executeMutation(
      `integration-test:${body.configurationRef ?? body.connectionRef}`,
      body,
      undefined,
      (headers) =>
        testIntegrationConnection({
          body,
          headers: headers as BasicMutationHeaders,
          ...readOptions(),
        }),
    )
  ).data;

export const fetchIntegrationTest = async (testRef: string) =>
  (
    await unwrap(
      getIntegrationTestReceipt({ path: { testRef }, ...readOptions() }),
    )
  ).data;

export const fetchIntegrationApprovals = async () => {
  const { values } = await collectAllPages(
    async (pageToken) =>
      (
        await unwrap(
          listIntegrationApprovals({
            query: { pageSize: 100, ...(pageToken ? { pageToken } : {}) },
            ...readOptions(),
          }),
        )
      ).data,
    (page) => page.approvals,
  );
  return { approvals: values };
};

export const fetchIntegrationApproval = async (approvalRef: string) =>
  (
    await unwrap(
      getIntegrationApproval({ path: { approvalRef }, ...readOptions() }),
    )
  ).data;

export const decideApproval = async (
  approvalRef: string,
  version: number,
  body: DecideIntegrationApproval,
) =>
  (
    await executeMutation(
      `integration-approval:${approvalRef}:${body.decision}`,
      body,
      version,
      (headers) =>
        decideIntegrationApproval({
          path: { approvalRef },
          body,
          headers: headers as RequiredMutationHeaders,
          ...readOptions(),
        }),
    )
  ).data;

export const fetchScheduleSelectors = async () =>
  (await unwrap(listScheduleSelectors(readOptions()))).data;

export const fetchOwnerSchedules = async () => {
  const { values } = await collectAllPages(
    async (pageToken) =>
      (
        await unwrap(
          listOwnerSchedules({
            query: { pageSize: 100, ...(pageToken ? { pageToken } : {}) },
            ...readOptions(),
          }),
        )
      ).data,
    (page) => page.items,
  );
  return { items: values };
};

export const fetchOwnerSchedule = async (scheduleRef: string) =>
  (await unwrap(getOwnerSchedule({ path: { scheduleRef }, ...readOptions() })))
    .data;

export const createOwnerSchedule = async (body: CreateScheduleFromSelections) =>
  (
    await executeMutation(
      `schedule:create:${body.name}`,
      body,
      undefined,
      (headers) =>
        createScheduleFromSelections({
          body,
          headers: headers as BasicMutationHeaders,
          ...readOptions(),
        }),
    )
  ).data;

export const updateOwnerSchedule = async (
  scheduleRef: string,
  version: number,
  body: BindScheduleConfiguration,
) =>
  (
    await executeMutation(
      `schedule:update:${scheduleRef}`,
      body,
      version,
      (headers) =>
        bindScheduleConfiguration({
          path: { scheduleRef },
          body,
          headers: headers as RequiredMutationHeaders,
          ...readOptions(),
        }),
    )
  ).data;

export const deleteOwnerSchedule = async (
  scheduleRef: string,
  version: number,
) =>
  (
    await executeMutation(
      `schedule:delete:${scheduleRef}`,
      {},
      version,
      (headers) =>
        deleteSchedule({
          path: { scheduleId: scheduleRef },
          headers: headers as RequiredMutationHeaders,
          ...readOptions(),
        }),
    )
  ).data;

export const runOwnerScheduleNow = async (
  scheduleRef: string,
  version: number,
) =>
  (
    await executeMutation(
      `schedule:run-now:${scheduleRef}`,
      {},
      version,
      (headers) =>
        runScheduleNow({
          path: { scheduleId: scheduleRef },
          headers: headers as RequiredMutationHeaders,
          ...readOptions(),
        }),
    )
  ).data;

export const fetchScheduleOccurrences = async (scheduleRef: string) => {
  const { values } = await collectAllPages(
    async (pageToken) =>
      (
        await unwrap(
          listScheduleOccurrences({
            path: { scheduleId: scheduleRef },
            query: { pageSize: 100, ...(pageToken ? { pageToken } : {}) },
            ...readOptions(),
          }),
        )
      ).data,
    (page) => page.occurrences,
  );
  return { occurrences: values };
};

export const recoverScheduleOccurrence = async (
  scheduleRef: string,
  occurrenceId: string,
  version: number,
  body: ResolveScheduleRecovery,
) =>
  (
    await executeMutation(
      `schedule:recovery:${scheduleRef}:${occurrenceId}:${body.action}`,
      body,
      version,
      (headers) =>
        resolveScheduleRecovery({
          path: { scheduleId: scheduleRef, occurrenceId },
          body,
          headers: headers as RequiredMutationHeaders,
          ...readOptions(),
        }),
    )
  ).data;

export const fetchRunDetail = async (runRef: string) =>
  (await unwrap(getRunDetail({ path: { runRef }, ...readOptions() }))).data;

export const fetchRunTimeline = async (runRef: string) => {
  const result = await collectAllPages(
    async (pageToken) =>
      (
        await unwrap(
          listRunTimeline({
            path: { runRef },
            query: { pageSize: 100, ...(pageToken ? { pageToken } : {}) },
            ...readOptions(),
          }),
        )
      ).data,
    (page) => page.entries,
  );
  const first = result.pages[0];
  if (!first) throw new Error("Run timeline page is missing");
  return {
    run: first.run,
    entries: result.values,
    nextActions: first.nextActions,
  };
};

export const fetchRunLineage = async (runRef: string) => {
  const result = await collectAllPages(
    async (pageToken) =>
      (
        await unwrap(
          getRunLineage({
            path: { runRef },
            query: { pageSize: 100, ...(pageToken ? { pageToken } : {}) },
            ...readOptions(),
          }),
        )
      ).data,
    (page) => page.nodes,
  );
  const first = result.pages[0];
  if (!first) throw new Error("Run lineage page is missing");
  return {
    run: first.run,
    nodes: result.values,
    nextActions: first.nextActions,
    truncated: result.pages.some((page) => page.truncated),
  };
};

export const fetchRunArtifacts = async (runRef: string) => {
  const result = await collectAllPages(
    async (pageToken) =>
      (
        await unwrap(
          listRunArtifacts({
            path: { runRef },
            query: { pageSize: 100, ...(pageToken ? { pageToken } : {}) },
            ...readOptions(),
          }),
        )
      ).data,
    (page) => page.artifacts,
  );
  return {
    artifacts: result.values,
    nextActions: result.pages[0]?.nextActions ?? [],
  };
};

export const commandRun = async (
  runRef: string,
  version: number,
  body: RunCommand,
) =>
  (
    await executeMutation(
      `run:${runRef}:${body.action}`,
      body,
      version,
      (headers) =>
        manageRun({
          path: { runRef },
          body,
          headers: headers as RequiredMutationHeaders,
          ...readOptions(),
        }),
    )
  ).data;

export const fetchIncident = async (incidentRef: string) =>
  (await unwrap(getIncident({ path: { incidentRef }, ...readOptions() }))).data;

export const fetchIncidentTimeline = async (incidentRef: string) => {
  const result = await collectAllPages(
    async (pageToken) =>
      (
        await unwrap(
          listIncidentHistory({
            path: { incidentRef },
            query: { pageSize: 100, ...(pageToken ? { pageToken } : {}) },
            ...readOptions(),
          }),
        )
      ).data,
    (page) => page.entries,
  );
  return {
    entries: result.values,
    nextActions: result.pages[0]?.nextActions ?? [],
  };
};

export const commandIncident = async (
  incidentRef: string,
  version: number,
  body: IncidentCommand,
) =>
  (
    await executeMutation(
      `incident:${incidentRef}:${body.action}`,
      body,
      version,
      (headers) =>
        manageIncident({
          path: { incidentRef },
          body,
          headers: headers as RequiredMutationHeaders,
          ...readOptions(),
        }),
    )
  ).data;

export const fetchHealthSeries = async () =>
  (await unwrap(getHealthSeries(readOptions()))).data;

export const fetchWorkspaceBackups = async () => {
  const { values } = await collectAllPages(
    async (pageToken) =>
      (
        await unwrap(
          listWorkspaceBackups({
            query: { pageSize: 100, ...(pageToken ? { pageToken } : {}) },
            ...readOptions(),
          }),
        )
      ).data,
    (page) => page.resources,
  );
  return { resources: values };
};

export const fetchWorkspaceBackup = async (resourceRef: string) =>
  (
    await unwrap(
      getWorkspaceBackup({
        path: { backupRef: resourceRef },
        ...readOptions(),
      }),
    )
  ).data;

export const commandWorkspaceBackup = async (
  body: WorkspaceBackupCommand,
  version?: number,
) =>
  (
    await executeMutation(
      `workspace-backup:${body.action}:${body.backupRef ?? body.name ?? "new"}`,
      body,
      version,
      (headers) =>
        manageWorkspaceBackup({
          body,
          headers: headers as OptionalMutationHeaders,
          ...readOptions(),
        }),
    )
  ).data;

export const fetchWorkspaceRestores = async () => {
  const { values } = await collectAllPages(
    async (pageToken) =>
      (
        await unwrap(
          listWorkspaceRestores({
            query: { pageSize: 100, ...(pageToken ? { pageToken } : {}) },
            ...readOptions(),
          }),
        )
      ).data,
    (page) => page.restores,
  );
  return { restores: values };
};

export const fetchWorkspaceRestore = async (restoreRef: string) =>
  (
    await unwrap(
      getWorkspaceRestore({ path: { restoreRef }, ...readOptions() }),
    )
  ).data;

export const commandWorkspaceRestore = async (
  body: WorkspaceRestoreCommand,
  version?: number,
) =>
  (
    await executeMutation(
      `workspace-restore:${body.action}:${body.restoreRef ?? body.name ?? "new"}`,
      body,
      version,
      (headers) =>
        manageWorkspaceRestore({
          body,
          headers: headers as OptionalMutationHeaders,
          ...readOptions(),
        }),
    )
  ).data;

export const downloadAudit = async (filters?: {
  resourceKind?: ResourceKind;
  resourceRef?: string;
  action?: string;
}) =>
  (
    await unwrap(
      exportAudit({
        query: filters ?? {},
        ...readOptions(),
      }),
    )
  ).data;

export const fetchConfigurationDiff = async (
  instructionSetRef: string,
  leftVersion: number,
  rightVersion: number,
) => {
  const result = await collectAllPages(
    async (pageToken) =>
      (
        await unwrap(
          getConfigurationDiff({
            query: {
              instructionSetRef,
              leftVersion,
              rightVersion,
              pageSize: 100,
              ...(pageToken ? { pageToken } : {}),
            },
            ...readOptions(),
          }),
        )
      ).data,
    (page) => page.changes,
  );
  const first = result.pages[0];
  if (!first) throw new Error("Configuration diff is empty");
  for (const page of result.pages) {
    if (
      page.left.snapshotSha256 !== first.left.snapshotSha256 ||
      page.right.snapshotSha256 !== first.right.snapshotSha256
    )
      throw new Error("Configuration diff changed during pagination");
  }
  return {
    left: first.left,
    right: first.right,
    changes: result.values,
    truncated: result.pages.some((page) => page.truncated),
  };
};

export const fetchConfigurationSource = async (
  resourceRef: string,
  kind: "ROLE_DEFINITION" | "AGENT" | "INSTRUCTION_SET" | "PROVIDER_POOL",
) =>
  (
    await unwrap(
      getConfigurationSourceDetail({
        path: { resourceRef },
        query: { kind },
        ...readOptions(),
      }),
    )
  ).data;
