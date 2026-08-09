import {
  bindScheduleConfiguration,
  cancelProviderAuthorization,
  compareInstructionSetVersions,
  configureIntegration,
  createMattermostTeam,
  createScheduleFromSelections,
  decideIntegrationApproval,
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
  revokeProviderConnection,
  startProviderAuthorization,
  testIntegrationConnection,
  unlinkMattermostTeam,
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
  RoleDefinitionCommand,
  RunCommand,
  StartProviderAuthorization,
  TestIntegrationConnection,
  WorkspaceBackupCommand,
  WorkspaceRestoreCommand,
} from "@/shared/api/generated/openapi/types.gen";
import { requestSignal } from "@/shared/api/client";
import { asProblem, unwrap } from "@/shared/api/problem";
import { mutationHeaders } from "@/shared/lib/identity";

type BasicMutationHeaders = {
  "X-CSRF-Token": string;
  "Idempotency-Key": string;
};
type RequiredMutationHeaders = BasicMutationHeaders & { "If-Match": string };
type OptionalMutationHeaders = BasicMutationHeaders & { "If-Match"?: string };

const basicHeaders = () => mutationHeaders() as BasicMutationHeaders;
const optionalHeaders = (version?: number) =>
  mutationHeaders(version) as OptionalMutationHeaders;
const requiredHeaders = (version: number) =>
  mutationHeaders(version) as RequiredMutationHeaders;

const readOptions = () => ({ signal: requestSignal() });

export const fetchMattermostTeams = async () =>
  (
    await unwrap(
      listMattermostTeams({ query: { pageSize: 100 }, ...readOptions() }),
    )
  ).data;

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
    await unwrap(
      createMattermostTeam({ body, headers: basicHeaders(), ...readOptions() }),
    )
  ).data;

export const linkTeam = async (selector: string) =>
  (
    await unwrap(
      linkMattermostTeam({
        body: { selector },
        headers: basicHeaders(),
        ...readOptions(),
      }),
    )
  ).data;

export const relinkTeam = async (body: RelinkMattermostTeam, version: number) =>
  (
    await unwrap(
      relinkMattermostTeam({
        body,
        headers: requiredHeaders(version),
        ...readOptions(),
      }),
    )
  ).data;

export const unlinkTeam = async (version: number, generation: number) =>
  (
    await unwrap(
      unlinkMattermostTeam({
        query: { generation },
        headers: requiredHeaders(version),
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

export const fetchRoleDefinitions = async () =>
  (
    await unwrap(
      listRoleDefinitions({ query: { pageSize: 100 }, ...readOptions() }),
    )
  ).data;

export const fetchRoleDefinition = async (resourceRef: string) =>
  (await unwrap(getRoleDefinition({ path: { resourceRef }, ...readOptions() })))
    .data;

export const fetchRoleDefinitionHistory = async (resourceRef: string) =>
  (
    await unwrap(
      listRoleDefinitionHistory({
        path: { resourceRef },
        query: { pageSize: 100 },
        ...readOptions(),
      }),
    )
  ).data;

export const commandRoleDefinition = async (
  body: RoleDefinitionCommand,
  version?: number,
) =>
  (
    await unwrap(
      manageRoleDefinition({
        body,
        headers: optionalHeaders(version),
        ...readOptions(),
      }),
    )
  ).data;

export const fetchAgents = async () =>
  (await unwrap(listAgents({ query: { pageSize: 100 }, ...readOptions() })))
    .data;

export const fetchAgent = async (resourceRef: string) =>
  (await unwrap(getAgent({ path: { resourceRef }, ...readOptions() }))).data;

export const fetchAgentHistory = async (resourceRef: string) =>
  (
    await unwrap(
      listAgentHistory({
        path: { resourceRef },
        query: { pageSize: 100 },
        ...readOptions(),
      }),
    )
  ).data;

export const fetchOwnerCatalog = async () =>
  (
    await unwrap(
      getOwnerConfigurationCatalog({
        query: { pageSize: 100 },
        ...readOptions(),
      }),
    )
  ).data;

export const commandAgent = async (body: AgentCommand, version?: number) =>
  (
    await unwrap(
      manageAgent({
        body,
        headers: optionalHeaders(version),
        ...readOptions(),
      }),
    )
  ).data;

export const fetchBotIdentities = async () =>
  (
    await unwrap(
      listAgentBotIdentities({ query: { pageSize: 100 }, ...readOptions() }),
    )
  ).data;

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
    await unwrap(
      manageAgentBotIdentity({
        path: { resourceRef: agentRef },
        body,
        headers: requiredHeaders(version),
        ...readOptions(),
      }),
    )
  ).data;

export const fetchBotOperation = async (
  agentRef: string,
  action: AgentBotIdentityAction,
) =>
  (
    await unwrap(
      getAgentBotIdentityOperation({
        path: { resourceRef: agentRef },
        query: { action },
        headers: { "Idempotency-Key": crypto.randomUUID() },
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

export const fetchAssignments = async () =>
  (
    await unwrap(
      listAgentAssignments({ query: { pageSize: 100 }, ...readOptions() }),
    )
  ).data;

export const fetchAssignment = async (resourceRef: string) =>
  (
    await unwrap(
      getAgentAssignment({ path: { resourceRef }, ...readOptions() }),
    )
  ).data;

export const fetchAssignmentHistory = async (resourceRef: string) =>
  (
    await unwrap(
      listAgentAssignmentHistory({
        path: { resourceRef },
        query: { pageSize: 100 },
        ...readOptions(),
      }),
    )
  ).data;

export const commandAssignment = async (
  body: AgentAssignmentCommand,
  version?: number,
) =>
  (
    await unwrap(
      manageAgentAssignment({
        body,
        headers: optionalHeaders(version),
        ...readOptions(),
      }),
    )
  ).data;

export const fetchInstructionSets = async () =>
  (
    await unwrap(
      listInstructionSets({ query: { pageSize: 100 }, ...readOptions() }),
    )
  ).data;

export const fetchInstructionSet = async (resourceRef: string) =>
  (await unwrap(getInstructionSet({ path: { resourceRef }, ...readOptions() })))
    .data;

export const fetchInstructionHistory = async (resourceRef: string) =>
  (
    await unwrap(
      listInstructionSetHistory({
        path: { resourceRef },
        query: { pageSize: 100 },
        ...readOptions(),
      }),
    )
  ).data;

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
    await unwrap(
      manageInstructionSet({
        body,
        headers: optionalHeaders(version),
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
    await unwrap(
      startProviderAuthorization({
        body,
        headers: basicHeaders(),
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
    await unwrap(
      restartProviderAuthorization({
        path: { authorizationRef },
        headers: requiredHeaders(version),
        ...readOptions(),
      }),
    )
  ).data;

export const cancelAuthorization = async (
  authorizationRef: string,
  version: number,
) =>
  (
    await unwrap(
      cancelProviderAuthorization({
        path: { authorizationRef },
        headers: requiredHeaders(version),
        ...readOptions(),
      }),
    )
  ).data;

export const fetchConnections = async () =>
  (
    await unwrap(
      listProviderConnections({ query: { pageSize: 100 }, ...readOptions() }),
    )
  ).data;

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
    await unwrap(
      revokeProviderConnection({
        path: { connectionRef },
        query: { generation },
        headers: requiredHeaders(version),
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
    await unwrap(
      reauthorizeProviderConnection({
        path: { connectionRef },
        query: { generation },
        headers: requiredHeaders(version),
        ...readOptions(),
      }),
    )
  ).data;

export const fetchProviderPools = async () =>
  (
    await unwrap(
      listProviderPools({ query: { pageSize: 100 }, ...readOptions() }),
    )
  ).data;

export const fetchProviderPool = async (poolRef: string) =>
  (await unwrap(getProviderPool({ path: { poolRef }, ...readOptions() }))).data;

export const commandProviderPool = async (
  body: ProviderPoolCommand,
  version?: number,
) =>
  (
    await unwrap(
      manageProviderPool({
        body,
        headers: optionalHeaders(version),
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

export const fetchIntegrationConfigurations = async () =>
  (
    await unwrap(
      listIntegrationConfigurations({
        query: { pageSize: 100 },
        ...readOptions(),
      }),
    )
  ).data;

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
    await unwrap(
      configureIntegration({
        body,
        headers: optionalHeaders(version),
        ...readOptions(),
      }),
    )
  ).data;

export const testIntegration = async (body: TestIntegrationConnection) =>
  (
    await unwrap(
      testIntegrationConnection({
        body,
        headers: basicHeaders(),
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

export const fetchIntegrationApprovals = async () =>
  (
    await unwrap(
      listIntegrationApprovals({
        query: { pageSize: 100 },
        ...readOptions(),
      }),
    )
  ).data;

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
    await unwrap(
      decideIntegrationApproval({
        path: { approvalRef },
        body,
        headers: requiredHeaders(version),
        ...readOptions(),
      }),
    )
  ).data;

export const fetchScheduleSelectors = async () =>
  (await unwrap(listScheduleSelectors(readOptions()))).data;

export const fetchOwnerSchedules = async () =>
  (
    await unwrap(
      listOwnerSchedules({ query: { pageSize: 100 }, ...readOptions() }),
    )
  ).data;

export const fetchOwnerSchedule = async (scheduleRef: string) =>
  (await unwrap(getOwnerSchedule({ path: { scheduleRef }, ...readOptions() })))
    .data;

export const createOwnerSchedule = async (body: CreateScheduleFromSelections) =>
  (
    await unwrap(
      createScheduleFromSelections({
        body,
        headers: basicHeaders(),
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
    await unwrap(
      bindScheduleConfiguration({
        path: { scheduleRef },
        body,
        headers: requiredHeaders(version),
        ...readOptions(),
      }),
    )
  ).data;

export const fetchRunDetail = async (runRef: string) =>
  (await unwrap(getRunDetail({ path: { runRef }, ...readOptions() }))).data;

export const fetchRunTimeline = async (runRef: string) =>
  (
    await unwrap(
      listRunTimeline({
        path: { runRef },
        query: { pageSize: 100 },
        ...readOptions(),
      }),
    )
  ).data;

export const fetchRunLineage = async (runRef: string) =>
  (
    await unwrap(
      getRunLineage({
        path: { runRef },
        query: { pageSize: 100 },
        ...readOptions(),
      }),
    )
  ).data;

export const fetchRunArtifacts = async (runRef: string) =>
  (
    await unwrap(
      listRunArtifacts({
        path: { runRef },
        query: { pageSize: 100 },
        ...readOptions(),
      }),
    )
  ).data;

export const commandRun = async (
  runRef: string,
  version: number,
  body: RunCommand,
) =>
  (
    await unwrap(
      manageRun({
        path: { runRef },
        body,
        headers: requiredHeaders(version),
        ...readOptions(),
      }),
    )
  ).data;

export const fetchIncident = async (incidentRef: string) =>
  (await unwrap(getIncident({ path: { incidentRef }, ...readOptions() }))).data;

export const fetchIncidentTimeline = async (incidentRef: string) =>
  (
    await unwrap(
      listIncidentHistory({
        path: { incidentRef },
        query: { pageSize: 100 },
        ...readOptions(),
      }),
    )
  ).data;

export const commandIncident = async (
  incidentRef: string,
  version: number,
  body: IncidentCommand,
) =>
  (
    await unwrap(
      manageIncident({
        path: { incidentRef },
        body,
        headers: requiredHeaders(version),
        ...readOptions(),
      }),
    )
  ).data;

export const fetchHealthSeries = async () =>
  (await unwrap(getHealthSeries(readOptions()))).data;

export const fetchWorkspaceBackups = async () =>
  (
    await unwrap(
      listWorkspaceBackups({ query: { pageSize: 100 }, ...readOptions() }),
    )
  ).data;

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
    await unwrap(
      manageWorkspaceBackup({
        body,
        headers: optionalHeaders(version),
        ...readOptions(),
      }),
    )
  ).data;

export const fetchWorkspaceRestores = async () =>
  (
    await unwrap(
      listWorkspaceRestores({ query: { pageSize: 100 }, ...readOptions() }),
    )
  ).data;

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
    await unwrap(
      manageWorkspaceRestore({
        body,
        headers: optionalHeaders(version),
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
) =>
  (
    await unwrap(
      getConfigurationDiff({
        query: {
          instructionSetRef,
          leftVersion,
          rightVersion,
          pageSize: 100,
        },
        ...readOptions(),
      }),
    )
  ).data;

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
