import { defineStore } from "pinia";
import { computed, reactive, ref } from "vue";

import { requestSignal } from "@/shared/api/client";
import {
  addAssistantTurn,
  addProjectMembership,
  addSessionTurn,
  applyAssistantPlan,
  commandAgent,
  commandAgentInstructions,
  commandIntegrationConnection,
  commandRoleImageRecipe,
  commandRun,
  commandSchedule,
  commandWorkflow,
  changeArtifactBinding,
  changeIntegrationGrant,
  changeProjectMembership,
  completeOnboarding,
  createAgent,
  createAssistantConversation,
  createInstructionDraft,
  createIntegrationConnection,
  createProject,
  createRoleImageRecipe,
  createRun,
  createSchedule,
  createWorkflow,
  downloadArtifact,
  getAdministration,
  getAgent,
  getBootstrapState,
  getIntegrationConnection,
  getOverview,
  getProject,
  getRoleImageRecipe,
  getRun,
  getRunGraph,
  getSystemAssistant,
  getWorkflow,
  listAgents,
  listArtifacts,
  listAssistantConversations,
  listAuditEvents,
  listIntegrationConnections,
  listIntegrationDefinitions,
  listOwnerGates,
  listPlatformCapabilities,
  listProjectMembershipCandidates,
  listProjectMemberships,
  listProjects,
  listRoleEnvironments,
  listRoleImageRecipes,
  listRunEvents,
  listRuns,
  listRuntimeSelections,
  listSchedules,
  listWorkflows,
  resolveOwnerGate,
  removeProjectMembership,
  updateAgent,
  updateProject,
  updateRoleImageRecipe,
  updateSchedule,
  updateSystemAssistantOwnerInstructions,
  updateWorkflowDraft,
  uploadArtifact,
} from "@/shared/api/generated/openapi/sdk.gen";
import type {
  AdministrationState,
  Agent,
  AgentCommand,
  AgentInput,
  Artifact,
  AssistantConversation,
  AuditEvent,
  BootstrapState,
  GateResolution,
  IntegrationConnection,
  IntegrationConnectionCommand,
  IntegrationConnectionInput,
  IntegrationGrantInput,
  IntegrationDefinition,
  Membership,
  MembershipInput,
  Overview,
  OwnerGate,
  PlatformCapability,
  Project,
  ProjectInput,
  RoleEnvironment,
  RoleEnvironmentSelection,
  RoleImageBuild,
  RoleImageRecipe,
  RoleImageRecipeCommand,
  RuntimeSelection,
  Run,
  RunCommand,
  RunEvent,
  RunGraph,
  RunInput,
  Schedule,
  ScheduleCommand,
  ScheduleInput,
  SystemAssistant,
  TurnInput,
  UserSummary,
  Workflow,
  WorkflowCommand,
  WorkflowInput,
} from "@/shared/api/generated/openapi/types.gen";
import { mutate, type MutationHeaders } from "@/shared/api/mutation";
import {
  asProblem,
  type AppProblem,
  normalizeProblem,
  unwrap,
} from "@/shared/api/problem";
import {
  reduceRunEvent,
  type RunEventOutcome,
} from "@/features/platform/run-reducer";
import { selectedProjectRef, selectProjectRef } from "@/shared/project-context";

type QueryKey =
  | "bootstrap"
  | "overview"
  | "projects"
  | "project"
  | "agents"
  | "agent"
  | "capabilities"
  | "roleEnvironments"
  | "roleImages"
  | "runtimes"
  | "workflows"
  | "workflow"
  | "runs"
  | "run"
  | "gates"
  | "artifacts"
  | "schedules"
  | "integrations"
  | "assistant"
  | "members"
  | "memberCandidates"
  | "administration"
  | "audit";

function mutationHeaders(headers: MutationHeaders): {
  "Idempotency-Key": string;
  "X-CSRF-Token": string;
} {
  return {
    "Idempotency-Key": headers["Idempotency-Key"],
    "X-CSRF-Token": headers["X-CSRF-Token"],
  };
}

function versionedHeaders(headers: MutationHeaders): {
  "Idempotency-Key": string;
  "X-CSRF-Token": string;
  "If-Match": string;
} {
  if (!headers["If-Match"]) throw new Error("Version header is unavailable");
  return { ...mutationHeaders(headers), "If-Match": headers["If-Match"] };
}

export const usePlatformStore = defineStore("platform", () => {
  const bootstrap = ref<BootstrapState>();
  const overview = ref<Overview>();
  const administration = ref<AdministrationState>();
  const capabilities = ref<PlatformCapability[]>([]);
  const runtimes = reactive<Record<string, RuntimeSelection>>({});
  const projects = reactive<Record<string, Project>>({});
  const agents = reactive<Record<string, Agent>>({});
  const roleEnvironments = reactive<Record<string, RoleEnvironment>>({});
  const roleImageRecipes = reactive<Record<string, RoleImageRecipe>>({});
  const roleImageBuilds = reactive<Record<string, RoleImageBuild>>({});
  const workflows = reactive<Record<string, Workflow>>({});
  const runs = reactive<Record<string, Run>>({});
  const graphs = reactive<Record<string, RunGraph>>({});
  const events = reactive<Record<string, Record<number, RunEvent>>>({});
  const gates = reactive<Record<string, OwnerGate>>({});
  const artifacts = reactive<Record<string, Artifact>>({});
  const schedules = reactive<Record<string, Schedule>>({});
  const definitions = reactive<Record<string, IntegrationDefinition>>({});
  const connections = reactive<Record<string, IntegrationConnection>>({});
  const memberships = reactive<Record<string, Membership>>({});
  const membershipCandidates = reactive<Record<string, UserSummary>>({});
  const conversations = reactive<Record<string, AssistantConversation>>({});
  const assistant = ref<SystemAssistant>();
  const auditEvents = ref<AuditEvent[]>([]);
  const loading = reactive<Partial<Record<QueryKey, boolean>>>({});
  const problems = reactive<Partial<Record<QueryKey, AppProblem>>>({});
  const generation = new Map<QueryKey, number>();

  async function query<T>(
    key: QueryKey,
    request: () => Promise<T>,
    apply: (value: T) => void,
  ): Promise<void> {
    const current = (generation.get(key) ?? 0) + 1;
    generation.set(key, current);
    loading[key] = true;
    Reflect.deleteProperty(problems, key);
    try {
      const value = await request();
      if (generation.get(key) !== current) return;
      apply(value);
    } catch (error) {
      if (generation.get(key) === current) problems[key] = asProblem(error);
    } finally {
      if (generation.get(key) === current) loading[key] = false;
    }
  }

  function upsert<T extends { ref: string }>(
    target: Record<string, T>,
    values: T[],
  ): void {
    for (const value of values) {
      const current = target[value.ref] as
        | (T & { version?: number })
        | undefined;
      const incoming = value as T & { version?: number };
      if (
        current?.version !== undefined &&
        incoming.version !== undefined &&
        current.version > incoming.version
      )
        continue;
      target[value.ref] = value;
    }
  }

  function replace<T extends { ref: string }>(
    target: Record<string, T>,
    values: T[],
  ): void {
    for (const ref of Object.keys(target)) Reflect.deleteProperty(target, ref);
    upsert(target, values);
  }

  async function loadBootstrap(): Promise<void> {
    await query(
      "bootstrap",
      async () =>
        (await unwrap(getBootstrapState({ signal: requestSignal() }))).data,
      (value) => {
        bootstrap.value = value;
        assistant.value = value.assistant;
      },
    );
  }

  async function loadOverview(projectRef?: string): Promise<void> {
    await query(
      "overview",
      async () =>
        (
          await unwrap(
            getOverview({
              query: projectRef ? { projectRef } : {},
              signal: requestSignal(),
            }),
          )
        ).data,
      (value) => {
        overview.value = value;
        upsert(runs, value.activeRuns);
        upsert(gates, value.pendingGates);
        upsert(artifacts, value.recentArtifacts);
      },
    );
  }

  async function loadProjects(): Promise<void> {
    await query(
      "projects",
      async () =>
        (
          await unwrap(
            listProjects({ query: { pageSize: 100 }, signal: requestSignal() }),
          )
        ).data.items,
      (values) => upsert(projects, values),
    );
  }

  async function loadProject(ref: string): Promise<void> {
    await query(
      "project",
      async () =>
        (
          await unwrap(
            getProject({ path: { projectRef: ref }, signal: requestSignal() }),
          )
        ).data,
      (value) => {
        projects[value.ref] = value;
      },
    );
  }

  async function loadAgents(projectRef: string): Promise<void> {
    await query(
      "agents",
      async () =>
        (
          await unwrap(
            listAgents({
              path: { projectRef },
              query: { pageSize: 100 },
              signal: requestSignal(),
            }),
          )
        ).data.items,
      (values) => upsert(agents, values),
    );
  }

  async function loadAgent(ref: string): Promise<void> {
    await query(
      "agent",
      async () =>
        (
          await unwrap(
            getAgent({ path: { agentRef: ref }, signal: requestSignal() }),
          )
        ).data,
      (value) => {
        agents[value.ref] = value;
      },
    );
  }

  async function loadRoleEnvironments(): Promise<void> {
    await query(
      "roleEnvironments",
      async () =>
        (await unwrap(listRoleEnvironments({ signal: requestSignal() }))).data
          .items,
      (values) => {
        for (const value of values) roleEnvironments[value.key] = value;
      },
    );
  }

  async function loadRoleImageRecipes(
    projectRef: string,
    roleDefinitionRef?: string,
  ): Promise<void> {
    await query(
      "roleImages",
      async () =>
        (
          await unwrap(
            listRoleImageRecipes({
              path: { projectRef },
              query: {
                ...(roleDefinitionRef ? { roleDefinitionRef } : {}),
                pageSize: 100,
              },
              signal: requestSignal(),
            }),
          )
        ).data.items,
      (values) => upsert(roleImageRecipes, values),
    );
  }

  async function loadRoleImageRecipe(
    projectRef: string,
    recipeRef: string,
  ): Promise<void> {
    await query(
      "roleImages",
      async () =>
        (
          await unwrap(
            getRoleImageRecipe({
              path: { projectRef, recipeRef },
              signal: requestSignal(),
            }),
          )
        ).data,
      (value) => {
        upsert(roleImageRecipes, [value.recipe]);
        upsert(roleImageBuilds, value.builds);
      },
    );
  }

  async function loadWorkflows(projectRef: string): Promise<void> {
    await query(
      "workflows",
      async () =>
        (
          await unwrap(
            listWorkflows({
              path: { projectRef },
              query: { pageSize: 100 },
              signal: requestSignal(),
            }),
          )
        ).data.items,
      (values) => upsert(workflows, values),
    );
  }

  async function loadWorkflow(ref: string): Promise<void> {
    await query(
      "workflow",
      async () =>
        (
          await unwrap(
            getWorkflow({
              path: { workflowRef: ref },
              signal: requestSignal(),
            }),
          )
        ).data,
      (value) => {
        workflows[value.ref] = value;
      },
    );
  }

  async function loadRuns(projectRef?: string): Promise<void> {
    await query(
      "runs",
      async () =>
        (
          await unwrap(
            listRuns({
              query: { ...(projectRef ? { projectRef } : {}), pageSize: 100 },
              signal: requestSignal(),
            }),
          )
        ).data.items,
      (values) => upsert(runs, values),
    );
  }

  async function loadRun(ref: string): Promise<void> {
    await query(
      "run",
      async () => {
        const [runReadback, graphReadback, eventPage] = await Promise.all([
          unwrap(getRun({ path: { runRef: ref }, signal: requestSignal() })),
          unwrap(
            getRunGraph({ path: { runRef: ref }, signal: requestSignal() }),
          ),
          unwrap(
            listRunEvents({
              path: { runRef: ref },
              query: { afterSequence: 0, limit: 200 },
              signal: requestSignal(),
            }),
          ),
        ]);
        return {
          run: runReadback.data,
          workspace: graphReadback.data,
          eventPage: eventPage.data,
        };
      },
      ({ run, workspace, eventPage }) => {
        upsert(runs, [run]);
        const current = graphs[workspace.graph.runRef];
        if (!current || workspace.graph.sequence >= current.sequence)
          graphs[workspace.graph.runRef] = workspace.graph;
        const bucket = events[workspace.graph.runRef] ?? {};
        for (const event of eventPage.items) bucket[event.sequence] = event;
        events[workspace.graph.runRef] = bucket;
      },
    );
  }

  async function loadGates(
    projectRef?: string,
    runRef?: string,
  ): Promise<void> {
    await query(
      "gates",
      async () =>
        (
          await unwrap(
            listOwnerGates({
              query: {
                ...(projectRef ? { projectRef } : {}),
                ...(runRef ? { runRef } : {}),
                pageSize: 100,
              },
              signal: requestSignal(),
            }),
          )
        ).data.items,
      (values) => upsert(gates, values),
    );
  }

  async function loadArtifacts(projectRef: string): Promise<void> {
    await query(
      "artifacts",
      async () =>
        (
          await unwrap(
            listArtifacts({
              path: { projectRef },
              query: { pageSize: 100 },
              signal: requestSignal(),
            }),
          )
        ).data.items,
      (values) => upsert(artifacts, values),
    );
  }

  async function uploadProjectArtifact(
    projectRef: string,
    file: File,
  ): Promise<Artifact> {
    const result = await mutate((headers) =>
      uploadArtifact({
        path: { projectRef },
        body: file,
        headers: {
          ...mutationHeaders(headers),
          "X-File-Name": file.name,
        },
        signal: requestSignal(),
      }),
    );
    upsert(artifacts, [result.data]);
    return result.data;
  }

  async function changeArtifactAgentBinding(
    artifact: Artifact,
    agentRef: string,
    enabled: boolean,
  ): Promise<Artifact> {
    const result = await mutate(
      (headers) =>
        changeArtifactBinding({
          path: { artifactRef: artifact.ref },
          body: { agentRef, enabled },
          headers: versionedHeaders(headers),
          signal: requestSignal(),
        }),
      artifact.version,
    );
    upsert(artifacts, [result.data]);
    void loadAgent(agentRef);
    return result.data;
  }

  async function downloadArtifactContent(
    artifactRef: string,
    purpose: "DOWNLOAD" | "PREVIEW",
  ): Promise<Blob> {
    const result = await unwrap(
      downloadArtifact({
        path: { artifactRef },
        query: { purpose },
        parseAs: "blob",
        signal: requestSignal(),
      }),
    );
    if (result.data instanceof Blob) return result.data;
    throw normalizeProblem({
      status: 502,
      code: "ARTIFACT_CONTENT_UNAVAILABLE",
      retryable: true,
    });
  }

  async function loadSchedules(projectRef: string): Promise<void> {
    await query(
      "schedules",
      async () =>
        (
          await unwrap(
            listSchedules({
              path: { projectRef },
              signal: requestSignal(),
            }),
          )
        ).data.items,
      (values) => upsert(schedules, values),
    );
  }

  async function loadIntegrations(): Promise<void> {
    await query(
      "integrations",
      async () => {
        const [definitionPage, connectionPage] = await Promise.all([
          unwrap(listIntegrationDefinitions({ signal: requestSignal() })),
          unwrap(listIntegrationConnections({ signal: requestSignal() })),
        ]);
        return {
          definitions: definitionPage.data.items,
          connections: connectionPage.data.items,
        };
      },
      (value) => {
        for (const item of value.definitions) definitions[item.key] = item;
        upsert(connections, value.connections);
      },
    );
  }

  async function loadConnection(ref: string): Promise<void> {
    await query(
      "integrations",
      async () =>
        (
          await unwrap(
            getIntegrationConnection({
              path: { connectionRef: ref },
              signal: requestSignal(),
            }),
          )
        ).data,
      (value) => {
        connections[value.ref] = value;
      },
    );
  }

  async function loadAssistant(): Promise<void> {
    await query(
      "assistant",
      async () => {
        const [assistantReadback, conversationPage] = await Promise.all([
          unwrap(getSystemAssistant({ signal: requestSignal() })),
          unwrap(
            listAssistantConversations({
              signal: requestSignal(),
            }),
          ),
        ]);
        return {
          assistant: assistantReadback.data,
          conversations: conversationPage.data.items,
        };
      },
      (value) => {
        assistant.value = value.assistant;
        upsert(conversations, value.conversations);
      },
    );
  }

  async function loadMembers(projectRef: string): Promise<void> {
    await query(
      "members",
      async () =>
        (
          await unwrap(
            listProjectMemberships({
              path: { projectRef },
              signal: requestSignal(),
            }),
          )
        ).data.items,
      (values) => replace(memberships, values),
    );
  }

  async function loadMembershipCandidates(
    projectRef: string,
    search = "",
  ): Promise<void> {
    await query(
      "memberCandidates",
      async () =>
        (
          await unwrap(
            listProjectMembershipCandidates({
              path: { projectRef },
              query: { query: search, pageSize: 100 },
              signal: requestSignal(),
            }),
          )
        ).data.items,
      (values) => replace(membershipCandidates, values),
    );
  }

  async function saveMembership(
    projectRef: string,
    input: MembershipInput,
    current?: Membership,
  ): Promise<Membership> {
    const result = current
      ? await mutate(
          (headers) =>
            changeProjectMembership({
              path: { projectRef, membershipRef: current.ref },
              body: input,
              headers: versionedHeaders(headers),
              signal: requestSignal(),
            }),
          current.version,
        )
      : await mutate((headers) =>
          addProjectMembership({
            path: { projectRef },
            body: input,
            headers: mutationHeaders(headers),
            signal: requestSignal(),
          }),
        );
    memberships[result.data.ref] = result.data;
    return result.data;
  }

  async function revokeMembership(
    projectRef: string,
    membership: Membership,
  ): Promise<Membership> {
    const result = await mutate(
      (headers) =>
        removeProjectMembership({
          path: { projectRef, membershipRef: membership.ref },
          headers: versionedHeaders(headers),
          signal: requestSignal(),
        }),
      membership.version,
    );
    memberships[result.data.ref] = result.data;
    return result.data;
  }

  async function loadAdministration(): Promise<void> {
    await query(
      "administration",
      async () =>
        (await unwrap(getAdministration({ signal: requestSignal() }))).data,
      (value) => {
        administration.value = value;
        assistant.value = value.assistant;
      },
    );
  }

  async function loadAudit(projectRef?: string): Promise<void> {
    await query(
      "audit",
      async () =>
        (
          await unwrap(
            listAuditEvents({
              query: { ...(projectRef ? { projectRef } : {}), pageSize: 100 },
              signal: requestSignal(),
            }),
          )
        ).data.items,
      (values) => {
        auditEvents.value = values;
      },
    );
  }

  async function loadCapabilities(): Promise<void> {
    await query(
      "capabilities",
      async () =>
        (await unwrap(listPlatformCapabilities({ signal: requestSignal() })))
          .data.items,
      (values) => {
        capabilities.value = values;
      },
    );
  }

  async function loadRuntimes(): Promise<void> {
    await query(
      "runtimes",
      async () =>
        (await unwrap(listRuntimeSelections({ signal: requestSignal() }))).data
          .items,
      (values) => replace(runtimes, values),
    );
  }

  async function finishOnboarding(): Promise<void> {
    const result = await mutate((headers) =>
      completeOnboarding({
        headers: mutationHeaders(headers),
        signal: requestSignal(),
      }),
    );
    bootstrap.value = result.data;
    assistant.value = result.data.assistant;
  }

  async function saveProject(
    input: ProjectInput,
    current?: Project,
  ): Promise<Project> {
    const result = current
      ? await mutate(
          (headers) =>
            updateProject({
              path: { projectRef: current.ref },
              body: input,
              headers: versionedHeaders(headers),
              signal: requestSignal(),
            }),
          current.version,
        )
      : await mutate((headers) =>
          createProject({
            body: input,
            headers: mutationHeaders(headers),
            signal: requestSignal(),
          }),
        );
    projects[result.data.ref] = result.data;
    return result.data;
  }

  async function saveAgent(
    projectRef: string,
    input: AgentInput,
    current?: Agent,
  ): Promise<Agent> {
    const result = current
      ? await mutate(
          (headers) =>
            updateAgent({
              path: { agentRef: current.ref },
              body: input,
              headers: versionedHeaders(headers),
              signal: requestSignal(),
            }),
          current.version,
        )
      : await mutate((headers) =>
          createAgent({
            path: { projectRef },
            body: input,
            headers: mutationHeaders(headers),
            signal: requestSignal(),
          }),
        );
    agents[result.data.ref] = result.data;
    return result.data;
  }

  async function readAgent(agentRef: string): Promise<Agent> {
    const readback = await unwrap(
      getAgent({
        path: { agentRef },
        signal: requestSignal(),
      }),
    );
    upsert(agents, [readback.data]);
    return agents[readback.data.ref] ?? readback.data;
  }

  async function changeAgent(agent: Agent, body: AgentCommand): Promise<Agent> {
    await mutate(
      (headers) =>
        commandAgent({
          path: { agentRef: agent.ref },
          body,
          headers: versionedHeaders(headers),
          signal: requestSignal(),
        }),
      agent.version,
    );
    return readAgent(agent.ref);
  }

  async function saveRoleImageRecipe(
    projectRef: string,
    roleDefinitionRef: string,
    name: string,
    environment: RoleEnvironmentSelection,
    current?: RoleImageRecipe,
  ): Promise<RoleImageRecipe> {
    const result = current
      ? await mutate(
          (headers) =>
            updateRoleImageRecipe({
              path: { projectRef, recipeRef: current.ref },
              body: { name, environment },
              headers: versionedHeaders(headers),
              signal: requestSignal(),
            }),
          current.version,
        )
      : await mutate((headers) =>
          createRoleImageRecipe({
            path: { projectRef },
            body: { roleDefinitionRef, name, environment },
            headers: mutationHeaders(headers),
            signal: requestSignal(),
          }),
        );
    upsert(roleImageRecipes, [result.data]);
    return result.data;
  }

  async function changeRoleImageRecipe(
    projectRef: string,
    recipe: RoleImageRecipe,
    action: RoleImageRecipeCommand["action"],
  ): Promise<RoleImageRecipe> {
    const result = await mutate(
      (headers) =>
        commandRoleImageRecipe({
          path: { projectRef, recipeRef: recipe.ref },
          body: { action },
          headers: versionedHeaders(headers),
          signal: requestSignal(),
        }),
      recipe.version,
    );
    upsert(roleImageRecipes, [result.data.recipe]);
    if (result.data.imageBuild)
      upsert(roleImageBuilds, [result.data.imageBuild]);
    return result.data.recipe;
  }

  async function saveInstructions(
    agent: Agent,
    content: string,
  ): Promise<Agent> {
    await mutate(
      (headers) =>
        createInstructionDraft({
          path: { agentRef: agent.ref },
          body: { content },
          headers: {
            ...mutationHeaders(headers),
            "If-Match": headers["If-Match"],
          },
          signal: requestSignal(),
        }),
      agent.version,
    );
    return readAgent(agent.ref);
  }

  async function instructionCommand(
    agent: Agent,
    action: "VALIDATE" | "PUBLISH" | "ROLLBACK",
  ): Promise<Agent> {
    await mutate(
      (headers) =>
        commandAgentInstructions({
          path: { agentRef: agent.ref },
          body: { action },
          headers: versionedHeaders(headers),
          signal: requestSignal(),
        }),
      agent.version,
    );
    return readAgent(agent.ref);
  }

  async function saveWorkflow(
    projectRef: string,
    input: WorkflowInput,
    current?: Workflow,
  ): Promise<Workflow> {
    const result = current
      ? await mutate(
          (headers) =>
            updateWorkflowDraft({
              path: { workflowRef: current.ref },
              body: input,
              headers: versionedHeaders(headers),
              signal: requestSignal(),
            }),
          current.version,
        )
      : await mutate((headers) =>
          createWorkflow({
            path: { projectRef },
            body: input,
            headers: mutationHeaders(headers),
            signal: requestSignal(),
          }),
        );
    workflows[result.data.ref] = result.data;
    return result.data;
  }

  async function changeWorkflow(
    workflow: Workflow,
    action: WorkflowCommand["action"],
  ): Promise<Workflow> {
    const result = await mutate(
      (headers) =>
        commandWorkflow({
          path: { workflowRef: workflow.ref },
          body: { action },
          headers: versionedHeaders(headers),
          signal: requestSignal(),
        }),
      workflow.version,
    );
    workflows[result.data.ref] = result.data;
    return result.data;
  }

  async function launch(input: RunInput): Promise<Run> {
    const result = await mutate((headers) =>
      createRun({
        body: input,
        headers: mutationHeaders(headers),
        signal: requestSignal(),
      }),
    );
    runs[result.data.run.ref] = result.data.run;
    graphs[result.data.graph.runRef] = result.data.graph;
    return result.data.run;
  }

  async function changeRun(run: Run, body: RunCommand): Promise<Run> {
    const result = await mutate(
      (headers) =>
        commandRun({
          path: { runRef: run.ref },
          body,
          headers: versionedHeaders(headers),
          signal: requestSignal(),
        }),
      run.version,
    );
    runs[result.data.run.ref] = result.data.run;
    graphs[result.data.graph.runRef] = result.data.graph;
    return result.data.run;
  }

  async function continueSession(
    sessionRef: string,
    input: TurnInput,
  ): Promise<Run> {
    const result = await mutate((headers) =>
      addSessionTurn({
        path: { sessionRef },
        body: input,
        headers: mutationHeaders(headers),
        signal: requestSignal(),
      }),
    );
    runs[result.data.run.ref] = result.data.run;
    graphs[result.data.graph.runRef] = result.data.graph;
    return result.data.run;
  }

  async function decide(
    gate: OwnerGate,
    body: GateResolution,
  ): Promise<OwnerGate> {
    const result = await mutate(
      (headers) =>
        resolveOwnerGate({
          path: { gateRef: gate.ref },
          body,
          headers: versionedHeaders(headers),
          signal: requestSignal(),
        }),
      gate.version,
    );
    gates[result.data.gate.ref] = result.data.gate;
    runs[result.data.run.ref] = result.data.run;
    graphs[result.data.graph.runRef] = result.data.graph;
    return result.data.gate;
  }

  async function saveSchedule(
    projectRef: string,
    input: ScheduleInput,
    current?: Schedule,
  ): Promise<Schedule> {
    const result = current
      ? await mutate(
          (headers) =>
            updateSchedule({
              path: { scheduleRef: current.ref },
              body: input,
              headers: versionedHeaders(headers),
              signal: requestSignal(),
            }),
          current.version,
        )
      : await mutate((headers) =>
          createSchedule({
            path: { projectRef },
            body: input,
            headers: mutationHeaders(headers),
            signal: requestSignal(),
          }),
        );
    schedules[result.data.ref] = result.data;
    return result.data;
  }

  async function changeSchedule(
    schedule: Schedule,
    action: ScheduleCommand["action"],
  ): Promise<Schedule> {
    const result = await mutate(
      (headers) =>
        commandSchedule({
          path: { scheduleRef: schedule.ref },
          body: { action },
          headers: versionedHeaders(headers),
          signal: requestSignal(),
        }),
      schedule.version,
    );
    schedules[result.data.ref] = result.data;
    return result.data;
  }

  async function connectIntegration(
    input: IntegrationConnectionInput,
  ): Promise<IntegrationConnection> {
    const result = await mutate((headers) =>
      createIntegrationConnection({
        body: input,
        headers: mutationHeaders(headers),
        signal: requestSignal(),
      }),
    );
    connections[result.data.ref] = result.data;
    return result.data;
  }

  async function readConnection(
    connectionRef: string,
  ): Promise<IntegrationConnection> {
    const readback = await unwrap(
      getIntegrationConnection({
        path: { connectionRef },
        signal: requestSignal(),
      }),
    );
    connections[readback.data.ref] = readback.data;
    return readback.data;
  }

  async function changeConnection(
    connection: IntegrationConnection,
    action: IntegrationConnectionCommand["action"],
  ): Promise<IntegrationConnection> {
    await mutate(
      (headers) =>
        commandIntegrationConnection({
          path: { connectionRef: connection.ref },
          body: { action },
          headers: versionedHeaders(headers),
          signal: requestSignal(),
        }),
      connection.version,
    );
    return readConnection(connection.ref);
  }

  async function changeConnectionGrant(
    connection: IntegrationConnection,
    input: IntegrationGrantInput,
  ): Promise<IntegrationConnection> {
    await mutate(
      (headers) =>
        changeIntegrationGrant({
          path: { connectionRef: connection.ref },
          body: input,
          headers: versionedHeaders(headers),
          signal: requestSignal(),
        }),
      connection.version,
    );
    return readConnection(connection.ref);
  }

  async function newConversation(
    title: string,
    projectRef?: string,
  ): Promise<AssistantConversation> {
    const result = await mutate((headers) =>
      createAssistantConversation({
        body: { title, ...(projectRef ? { projectRef } : {}) },
        headers: mutationHeaders(headers),
        signal: requestSignal(),
      }),
    );
    conversations[result.data.ref] = result.data;
    return result.data;
  }

  async function sendAssistantTurn(
    conversationRef: string,
    content: string,
  ): Promise<AssistantConversation> {
    const result = await mutate((headers) =>
      addAssistantTurn({
        path: { conversationRef },
        body: { content },
        headers: mutationHeaders(headers),
        signal: requestSignal(),
      }),
    );
    conversations[result.data.ref] = result.data;
    return result.data;
  }

  async function applyPlan(
    planRef: string,
    version: number,
  ): Promise<AssistantConversation> {
    const result = await mutate(
      (headers) =>
        applyAssistantPlan({
          path: { planRef },
          headers: versionedHeaders(headers),
          signal: requestSignal(),
        }),
      version,
    );
    conversations[result.data.conversation.ref] = result.data.conversation;
    return result.data.conversation;
  }

  async function updateAssistantInstructions(
    value: string,
  ): Promise<SystemAssistant> {
    if (!assistant.value) throw new Error("System assistant is unavailable");
    const result = await mutate(
      (headers) =>
        updateSystemAssistantOwnerInstructions({
          body: { ownerInstructions: value },
          headers: versionedHeaders(headers),
          signal: requestSignal(),
        }),
      assistant.value.version,
    );
    assistant.value = result.data;
    return result.data;
  }

  function applyRunSnapshot(graph: RunGraph): void {
    const current = graphs[graph.runRef];
    if (current && current.sequence > graph.sequence) return;
    graphs[graph.runRef] = graph;
  }

  function applyRunEvent(event: RunEvent): RunEventOutcome {
    return reduceRunEvent({ runs, graphs, events, gates, artifacts }, event);
  }

  async function reloadPlatformKind(kind: string): Promise<void> {
    const projectRef = selectedProjectRef();
    const operations: Array<{ key: QueryKey; run: () => Promise<void> }> = [];
    const add = (key: QueryKey, run: () => Promise<void>): void => {
      if (!operations.some((operation) => operation.key === key))
        operations.push({ key, run });
    };
    switch (kind) {
      case "PROJECT":
        add("projects", loadProjects);
        add("overview", () => loadOverview(projectRef));
        if (projectRef) add("project", () => loadProject(projectRef));
        break;
      case "AGENT":
      case "INSTRUCTIONS":
        if (projectRef) add("agents", () => loadAgents(projectRef));
        add("overview", () => loadOverview(projectRef));
        break;
      case "WORKFLOW":
        if (projectRef) add("workflows", () => loadWorkflows(projectRef));
        break;
      case "ARTIFACT":
        if (projectRef) add("artifacts", () => loadArtifacts(projectRef));
        add("overview", () => loadOverview(projectRef));
        break;
      case "SCHEDULE":
        if (projectRef) add("schedules", () => loadSchedules(projectRef));
        break;
      case "INTEGRATION_CONNECTION":
      case "INTEGRATION_GRANT":
        add("integrations", loadIntegrations);
        break;
      case "MEMBERSHIP":
        add("projects", loadProjects);
        if (projectRef) add("members", () => loadMembers(projectRef));
        break;
      case "SYSTEM_ASSISTANT":
        add("bootstrap", loadBootstrap);
        add("assistant", loadAssistant);
        break;
      case "ROLE_IMAGE_RECIPE":
        if (projectRef)
          add("roleImages", () => loadRoleImageRecipes(projectRef));
        break;
      default:
        throw new Error("Unknown platform invalidation kind");
    }
    await Promise.all(operations.map((operation) => operation.run()));
    if (operations.some((operation) => problems[operation.key]))
      throw new Error("Authoritative platform reload failed");
  }

  async function reloadPlatformState(): Promise<void> {
    const projectRef = selectedProjectRef();
    const operations: Array<{ key: QueryKey; run: () => Promise<void> }> = [
      { key: "bootstrap", run: loadBootstrap },
      { key: "projects", run: loadProjects },
      { key: "overview", run: () => loadOverview(projectRef) },
      { key: "runs", run: () => loadRuns(projectRef) },
      { key: "gates", run: () => loadGates(projectRef) },
      { key: "integrations", run: loadIntegrations },
      { key: "assistant", run: loadAssistant },
    ];
    if (projectRef) {
      operations.push(
        { key: "project", run: () => loadProject(projectRef) },
        { key: "agents", run: () => loadAgents(projectRef) },
        { key: "workflows", run: () => loadWorkflows(projectRef) },
        { key: "artifacts", run: () => loadArtifacts(projectRef) },
        { key: "schedules", run: () => loadSchedules(projectRef) },
        {
          key: "roleImages",
          run: () => loadRoleImageRecipes(projectRef),
        },
      );
    }
    await Promise.all(operations.map((operation) => operation.run()));
    if (operations.some((operation) => problems[operation.key]))
      throw new Error("Authoritative platform resync failed");
  }

  function clearOwnerState(): void {
    generation.clear();
    for (const target of [
      runtimes,
      projects,
      agents,
      roleEnvironments,
      roleImageRecipes,
      roleImageBuilds,
      workflows,
      runs,
      graphs,
      events,
      gates,
      artifacts,
      schedules,
      definitions,
      connections,
      memberships,
      membershipCandidates,
      conversations,
    ]) {
      for (const key of Object.keys(target))
        Reflect.deleteProperty(target, key);
    }
    for (const key of Object.keys(loading))
      Reflect.deleteProperty(loading, key);
    for (const key of Object.keys(problems))
      Reflect.deleteProperty(problems, key);
    bootstrap.value = undefined;
    overview.value = undefined;
    administration.value = undefined;
    capabilities.value = [];
    assistant.value = undefined;
    auditEvents.value = [];
    selectProjectRef(undefined);
  }

  const projectList = computed(() => Object.values(projects));
  const runList = computed(() => Object.values(runs));
  const gateList = computed(() => Object.values(gates));

  return {
    bootstrap,
    overview,
    administration,
    capabilities,
    runtimes,
    projects,
    agents,
    roleEnvironments,
    roleImageRecipes,
    roleImageBuilds,
    workflows,
    runs,
    graphs,
    events,
    gates,
    artifacts,
    schedules,
    definitions,
    connections,
    memberships,
    membershipCandidates,
    conversations,
    assistant,
    auditEvents,
    loading,
    problems,
    projectList,
    runList,
    gateList,
    loadBootstrap,
    loadOverview,
    loadProjects,
    loadProject,
    loadAgents,
    loadAgent,
    loadRoleEnvironments,
    loadRoleImageRecipes,
    loadRoleImageRecipe,
    loadWorkflows,
    loadWorkflow,
    loadRuns,
    loadRun,
    loadGates,
    loadArtifacts,
    uploadProjectArtifact,
    changeArtifactAgentBinding,
    downloadArtifactContent,
    loadSchedules,
    loadIntegrations,
    loadConnection,
    loadAssistant,
    loadMembers,
    loadMembershipCandidates,
    saveMembership,
    revokeMembership,
    loadAdministration,
    loadAudit,
    loadCapabilities,
    loadRuntimes,
    finishOnboarding,
    saveProject,
    saveAgent,
    changeAgent,
    saveRoleImageRecipe,
    changeRoleImageRecipe,
    saveInstructions,
    instructionCommand,
    saveWorkflow,
    changeWorkflow,
    launch,
    changeRun,
    continueSession,
    decide,
    saveSchedule,
    changeSchedule,
    connectIntegration,
    changeConnection,
    changeConnectionGrant,
    newConversation,
    sendAssistantTurn,
    applyPlan,
    updateAssistantInstructions,
    applyRunSnapshot,
    applyRunEvent,
    reloadPlatformKind,
    reloadPlatformState,
    clearOwnerState,
  };
});
