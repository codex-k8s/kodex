import { defineStore } from "pinia";
import { computed, reactive, ref } from "vue";
import {
  invalidSearchResult,
  isSearchResult,
} from "@/shared/api/search-result";

import { requestSignal } from "@/shared/api/client";
import {
  assertRuntimeResourceIdentity,
  requireRuntimeOrganizationRef,
  organizationRuntimeResourceScope,
  type RuntimeScopedResourceIdentity,
} from "@/features/runtime/resource-scope";
import { normalizeSecretPage } from "@/features/runtime-secrets/model";
import { assertGateScope } from "@/features/workboard/gate-scope";
import {
  addPlatformMembership,
  addProjectMembership,
  addSessionTurn,
  commandAgent,
  commandAgentInstructions,
  commandIntegrationConnection,
  commandRoleImageRecipe,
  commandRun,
  commandSchedule,
  commandWorkflow,
  changeArtifactBinding,
  changeIntegrationGrant,
  changePlatformMembership,
  changeProjectMembership,
  completeOnboarding,
  configureIntegrationConnectionCredential,
  createAgent,
  createInstructionDraft,
  createIntegrationConnection,
  createProject,
  createRoleImageRecipe,
  createRun,
  createSchedule,
  createWorkflow,
  deleteIntegrationConnection,
  deleteArtifact,
  downloadArtifact,
  getAdministration,
  getAgent,
  getArtifact,
  getArtifactImpact,
  getBootstrapState,
  getIntegrationConnection,
  getOverview,
  getProject,
  getRoleImageRecipe,
  getRunGraph,
  getSystemAssistant,
  getWorkflow,
  listAgents,
  listAgentInstructionVersions,
  listArtifacts,
  listAssistantConversations,
  listAuditEvents,
  listIntegrationConnections,
  listIntegrationDefinitions,
  listOwnerGates,
  listPlatformCapabilities,
  listPlatformMembershipCandidates,
  listPlatformMemberships,
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
  searchPlatform,
  removeProjectMembership,
  removePlatformMembership,
  updateAgent,
  updateIntegrationConnection,
  updateProject,
  updateRoleImageRecipe,
  updateSchedule,
  updateSystemAssistantOwnerInstructions,
  updateWorkflowDraft,
  uploadArtifact,
  uploadOrganizationArtifact,
} from "@/shared/api/generated/openapi/sdk.gen";
import type {
  AdministrationState,
  Agent,
  AgentCommand,
  AgentInput,
  Artifact,
  ArtifactImpact,
  AssistantConversation,
  AuditEvent,
  BootstrapState,
  GateResolution,
  IntegrationConnection,
  IntegrationConnectionCommand,
  IntegrationConnectionInput,
  IntegrationConnectionUpdateInput,
  IntegrationGrantInput,
  IntegrationDefinition,
  InstructionVersion,
  ManagedConfigurationSummary,
  Membership,
  NextAction,
  Overview,
  OwnerGate,
  PlatformCapability,
  PlatformMembershipChangeInput,
  PlatformMembershipCreateInput,
  Project,
  ProjectInput,
  ProjectMembershipChangeInput,
  ProjectMembershipCreateInput,
  RoleEnvironment,
  RoleEnvironmentSelection,
  RoleImageBuild,
  RoleImageRecipe,
  RoleImageRecipeCommand,
  RuntimeSecret,
  RuntimeSelection,
  Run,
  RunCommand,
  RunEvent,
  RunGraph,
  RunInput,
  Schedule,
  ScheduleCommand,
  ScheduleInput,
  SearchResult,
  SearchResultPage,
  SystemAssistant,
  TurnInput,
  UserSummary,
  Workflow,
  WorkflowCommand,
  WorkflowInput,
} from "@/shared/api/generated/openapi/types.gen";
import type { PlatformResourceKind } from "@/shared/api/generated/asyncapi/PlatformResourceKind";
import {
  csrfToken,
  etag,
  mutate,
  mutateWithRetry,
  type MutationHeaders,
} from "@/shared/api/mutation";
import {
  AppProblem,
  asProblem,
  normalizeProblem,
  unwrap,
} from "@/shared/api/problem";
import { readWithRetry } from "@/shared/api/read-retry";
import {
  assertOwnerRequest,
  ownerRequestSignal,
  resetOwnerRequests,
} from "@/shared/api/owner-lifetime";
import {
  mergeRunGraph,
  reduceRunEvent,
  type RunEventOutcome,
} from "@/features/platform/run-reducer";
import { instructionCommandInput } from "@/features/platform/instruction-command";
import { runBoundedPlatformReload } from "@/features/platform/platform-reload";
import {
  assertAssistantRetryIdentity,
  assertRunOwner,
  sameAssistantRunPin,
} from "@/features/runs/run-owner";
import { selectedProjectRef, selectProjectRef } from "@/shared/project-context";

export interface RealtimeCatalogSnapshot {
  scopeKey: string;
  total?: number;
  nextPageToken?: string;
}

const runHistorySupersededMessage = "Run history read superseded";

type QueryKey =
  | "bootstrap"
  | "overview"
  | "projects"
  | "project"
  | "agents"
  | "agent"
  | "instructionVersions"
  | "capabilities"
  | "roleEnvironments"
  | "roleImages"
  | "runtimes"
  | "search"
  | "searchMore"
  | "workflows"
  | "workflow"
  | "runs"
  | "gates"
  | "gateCount"
  | "artifacts"
  | "schedules"
  | "integrations"
  | "assistant"
  | "platformMembers"
  | "platformMemberCandidates"
  | "members"
  | "memberCandidates"
  | "administration"
  | "audit"
  | "auditMore";

function inSelectedProject(
  projectRef: string | undefined,
  run: () => Promise<void>,
): () => Promise<void> {
  return async () => {
    if (selectedProjectRef() !== projectRef) return;
    await run();
  };
}

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
  const searchResults = ref<SearchResult[]>([]);
  const searchNextPageToken = ref<string>();
  const searchTotal = ref(0);
  const activeSearchQuery = ref("");
  const projects = reactive<Record<string, Project>>({});
  const trashedProjects = reactive<Record<string, Project>>({});
  const agents = reactive<Record<string, Agent>>({});
  const instructionVersions = reactive<Record<string, InstructionVersion[]>>(
    {},
  );
  const roleEnvironments = reactive<Record<string, RoleEnvironment>>({});
  const roleImageRecipes = reactive<Record<string, RoleImageRecipe>>({});
  const organizationRoleImageRecipes = reactive<
    Record<string, RoleImageRecipe>
  >({});
  const organizationRoleImagePage = ref<RealtimeCatalogSnapshot>();
  const organizationRoleImageRealtimeRevision = ref(0);
  const roleImageBuilds = reactive<Record<string, RoleImageBuild>>({});
  const workflows = reactive<Record<string, Workflow>>({});
  const runs = reactive<Record<string, Run>>({});
  const graphs = reactive<Record<string, RunGraph>>({});
  const events = reactive<Record<string, Record<number, RunEvent>>>({});
  const gates = reactive<Record<string, OwnerGate>>({});
  const pendingGateCount = ref<number>();
  const gateCatalogRevision = ref(0);
  const roleImageRealtimeRevision = ref(0);
  const ownerGateNextPageToken = ref<string>();
  const artifacts = reactive<Record<string, Artifact>>({});
  const schedules = reactive<Record<string, Schedule>>({});
  const runtimeSecrets = reactive<Record<string, RuntimeSecret>>({});
  const organizationRuntimeSecrets = reactive<Record<string, RuntimeSecret>>(
    {},
  );
  const organizationRuntimeSecretPage = ref<RealtimeCatalogSnapshot>();
  const organizationRuntimeSecretRealtimeRevision = ref(0);
  const managedConfigurations = reactive<
    Record<string, ManagedConfigurationSummary>
  >({});
  const managedConfigurationPages = reactive<
    Record<string, { total: number; nextPageToken?: string }>
  >({});
  const managedConfigurationRealtimeRevision = ref(0);
  const definitions = reactive<Record<string, IntegrationDefinition>>({});
  const connections = reactive<Record<string, IntegrationConnection>>({});
  const memberships = reactive<Record<string, Membership>>({});
  const membershipCandidates = reactive<Record<string, UserSummary>>({});
  const platformMemberships = reactive<Record<string, Membership>>({});
  const platformMembershipCandidates = reactive<Record<string, UserSummary>>(
    {},
  );
  const platformMembershipActions = ref<NextAction[]>([]);
  const projectMembershipActions = ref<NextAction[]>([]);
  const projectCollectionActions = ref<NextAction[]>([]);
  const projectTrashNextPageToken = ref<string>();
  const integrationDefinitionActions = ref<NextAction[]>([]);
  const integrationCoreReady = ref<boolean>();
  const integrationDefinitionNextPageToken = ref<string>();
  const integrationConnectionNextPageToken = ref<string>();
  const integrationRealtimeRevision = ref(0);
  const conversations = reactive<Record<string, AssistantConversation>>({});
  const assistantConversationNextPageToken = ref<string>();
  const assistantRealtimeScopeKey = ref<string>();
  const realtimeCatalogSnapshots = reactive<
    Record<string, RealtimeCatalogSnapshot>
  >({});
  const realtimeAvailableKinds = ref<PlatformResourceKind[]>([]);
  const assistant = ref<SystemAssistant>();
  const auditEvents = ref<AuditEvent[]>([]);
  const auditNextPageToken = ref<string>();
  const auditScopeKey = ref("");
  const loading = reactive<Partial<Record<QueryKey, boolean>>>({});
  const problems = reactive<Partial<Record<QueryKey, AppProblem>>>({});
  const generation = new Map<QueryKey, number>();
  const runLoading = reactive<Record<string, boolean>>({});
  const runProblems = reactive<Partial<Record<string, AppProblem>>>({});
  const runReadGeneration = new Map<string, number>();
  const consumedSearchPageTokens = new Set<string>();
  const consumedAuditPageTokens = new Set<string>();
  let platformReloadPromise: Promise<void> | undefined;
  let platformReloadScope: AbortSignal | undefined;

  async function query<T>(
    key: QueryKey,
    request: () => Promise<T>,
    apply: (value: T) => void,
    retryDelaysMs?: readonly number[],
  ): Promise<void> {
    const ownerScope = ownerRequestSignal();
    const current = (generation.get(key) ?? 0) + 1;
    generation.set(key, current);
    loading[key] = true;
    Reflect.deleteProperty(problems, key);
    try {
      const value = await readWithRetry(request, retryDelaysMs);
      if (ownerScope.aborted || generation.get(key) !== current) return;
      apply(value);
    } catch (error) {
      if (!ownerScope.aborted && generation.get(key) === current)
        problems[key] = asProblem(error);
    } finally {
      if (!ownerScope.aborted && generation.get(key) === current)
        loading[key] = false;
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

  function replaceScoped<T extends { ref: string }>(
    target: Record<string, T>,
    values: T[],
    belongsToScope: (value: T) => boolean,
  ): void {
    const currentRefs = new Set(values.map((value) => value.ref));
    for (const [ref, value] of Object.entries(target)) {
      if (belongsToScope(value) && !currentRefs.has(ref))
        Reflect.deleteProperty(target, ref);
    }
    upsert(target, values);
  }

  function reconcileRuns(values: Run[]): void {
    for (const value of values)
      assertRunOwner(value, bootstrap.value?.organizationRef);
    // loadRuns читает только короткую сводную страницу. Отсутствие Run в ней
    // не является авторитетным доказательством удаления: подробная страница и
    // realtime могут держать более старый или не попавший в первые строки Run.
    // Коллекцией RunsPage владеет отдельное хранилище с курсорами.
    for (const value of values) {
      const current = runs[value.ref];
      if (current && current.version <= value.version)
        Object.assign(current, value);
      else if (!current) runs[value.ref] = value;
    }
  }

  function reconcileConversations(values: AssistantConversation[]): void {
    const currentRefs = new Set(values.map((value) => value.ref));
    for (const ref of Object.keys(conversations)) {
      if (!currentRefs.has(ref)) Reflect.deleteProperty(conversations, ref);
    }
    for (const value of values) {
      const current = conversations[value.ref];
      if (current && current.version <= value.version)
        Object.assign(current, value);
      else if (!current) conversations[value.ref] = value;
    }
  }

  function replaceByKey<T>(
    target: Record<string, T>,
    values: T[],
    key: (value: T) => string,
  ): void {
    for (const current of Object.keys(target))
      Reflect.deleteProperty(target, current);
    for (const value of values) target[key(value)] = value;
  }

  function applyAuthenticatedBootstrap(value: BootstrapState): void {
    requireRuntimeOrganizationRef(value.organizationRef);
    if (
      bootstrap.value?.organizationRef !== value.organizationRef ||
      bootstrap.value.platformRole !== value.platformRole
    )
      clearOrganizationCatalogs();
    bootstrap.value = value;
    assistant.value = value.assistant;
  }

  async function loadBootstrap(): Promise<void> {
    await query(
      "bootstrap",
      async () =>
        (await unwrap(getBootstrapState({ signal: requestSignal() }))).data,
      applyAuthenticatedBootstrap,
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
        for (const run of value.activeRuns)
          assertRunOwner(run, bootstrap.value?.organizationRef);
        for (const gate of value.pendingGates)
          assertGateScope(gate, bootstrap.value?.organizationRef);
        overview.value = value;
        upsert(runs, value.activeRuns);
        upsert(gates, value.pendingGates);
        upsert(artifacts, value.recentArtifacts);
      },
    );
  }

  let searchController: AbortController | undefined;

  function cancelSearch(): void {
    searchController?.abort();
    searchController = undefined;
    generation.set("search", (generation.get("search") ?? 0) + 1);
    generation.set("searchMore", (generation.get("searchMore") ?? 0) + 1);
    loading.search = false;
    loading.searchMore = false;
    searchResults.value = [];
    searchNextPageToken.value = undefined;
    searchTotal.value = 0;
    activeSearchQuery.value = "";
    consumedSearchPageTokens.clear();
    Reflect.deleteProperty(problems, "search");
    Reflect.deleteProperty(problems, "searchMore");
  }

  function searchLimit(value?: number): number {
    return Math.min(50, Math.max(1, Math.floor(value ?? 20)));
  }

  function validateSearchPage(
    value: SearchResultPage,
    requestedCursor?: string,
  ): void {
    if (!Array.isArray(value.items) || !value.items.every(isSearchResult))
      throw invalidSearchResult();
    const identities = new Set(
      value.items.map((item) => `${item.kind}:${item.ref}`),
    );
    if (
      !Number.isSafeInteger(value.total) ||
      value.total < value.items.length ||
      identities.size !== value.items.length ||
      (value.nextPageToken !== undefined &&
        (value.nextPageToken.length === 0 ||
          value.nextPageToken.length > 512 ||
          value.nextPageToken === requestedCursor))
    )
      throw invalidSearchResult();
  }

  async function search(term: string, pageSize?: number): Promise<void> {
    cancelSearch();
    const normalized = term.trim();
    if (normalized.length < 2) {
      searchResults.value = [];
      return;
    }
    activeSearchQuery.value = normalized;
    const controller = new AbortController();
    searchController = controller;
    await query(
      "search",
      async () =>
        (
          await unwrap(
            searchPlatform({
              query: { query: normalized, limit: searchLimit(pageSize) },
              signal: requestSignal(controller.signal),
            }),
          )
        ).data,
      (value) => {
        validateSearchPage(value);
        searchResults.value = value.items;
        searchNextPageToken.value = value.nextPageToken;
        searchTotal.value = value.total;
      },
    );
  }

  async function loadMoreSearch(pageSize?: number): Promise<void> {
    const cursor = searchNextPageToken.value;
    const queryValue = activeSearchQuery.value;
    if (!cursor || !queryValue || loading.search || loading.searchMore) return;
    searchController ??= new AbortController();
    const controller = searchController;
    await query(
      "searchMore",
      async () =>
        (
          await unwrap(
            searchPlatform({
              query: {
                query: queryValue,
                limit: searchLimit(pageSize),
                pageToken: cursor,
              },
              signal: requestSignal(controller.signal),
            }),
          )
        ).data,
      (value) => {
        if (activeSearchQuery.value !== queryValue) return;
        validateSearchPage(value, cursor);
        if (
          value.total !== searchTotal.value ||
          (value.nextPageToken !== undefined &&
            consumedSearchPageTokens.has(value.nextPageToken))
        )
          throw invalidSearchResult();
        const unique = new Map(
          searchResults.value.map((item) => [`${item.kind}:${item.ref}`, item]),
        );
        for (const item of value.items) {
          const identity = `${item.kind}:${item.ref}`;
          if (unique.has(identity)) throw invalidSearchResult();
          unique.set(identity, item);
        }
        if (unique.size > value.total) throw invalidSearchResult();
        consumedSearchPageTokens.add(cursor);
        searchResults.value = [...unique.values()];
        searchNextPageToken.value = value.nextPageToken;
        searchTotal.value = value.total;
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
        ).data,
      (value) => {
        replace(projects, value.items);
        projectCollectionActions.value = value.nextActions;
      },
      [0, 200, 600, 1_500, 3_000, 5_000],
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
      (values) =>
        replaceScoped(
          agents,
          values,
          (agent) => agent.projectRef === projectRef,
        ),
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

  async function loadInstructionVersions(agentRef: string): Promise<void> {
    await query(
      "instructionVersions",
      async () => {
        const versions: InstructionVersion[] = [];
        let pageToken: string | undefined;
        do {
          const response = await unwrap(
            listAgentInstructionVersions({
              path: { agentRef },
              query: { pageSize: 100, ...(pageToken ? { pageToken } : {}) },
              signal: requestSignal(),
            }),
          );
          versions.push(...response.data.items);
          pageToken = response.data.nextPageToken;
        } while (pageToken);
        return versions;
      },
      (versions) => {
        instructionVersions[agentRef] = versions;
      },
    );
  }

  async function loadRoleEnvironments(): Promise<void> {
    await query(
      "roleEnvironments",
      async () =>
        (await unwrap(listRoleEnvironments({ signal: requestSignal() }))).data
          .items,
      (values) => replaceByKey(roleEnvironments, values, (value) => value.key),
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
      (values) =>
        replaceScoped(
          roleImageRecipes,
          values,
          (recipe) => recipe.projectRef === projectRef,
        ),
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
      (values) =>
        replaceScoped(
          workflows,
          values,
          (workflow) => workflow.projectRef === projectRef,
        ),
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
              // Сводные экраны не потребляют cursor/total этой выборки. Большая
              // страница только удлиняет repeatable-read authorization snapshot
              // на накопленном стенде и может исчерпать его серверный budget.
              query: { ...(projectRef ? { projectRef } : {}), pageSize: 6 },
              signal: requestSignal(),
            }),
          )
        ).data.items,
      (values) => {
        reconcileRuns(values);
      },
    );
  }

  async function loadRun(ref: string): Promise<void> {
    const ownerSignal = ownerRequestSignal();
    const current = (runReadGeneration.get(ref) ?? 0) + 1;
    runReadGeneration.set(ref, current);
    const active = () =>
      !ownerSignal.aborted && runReadGeneration.get(ref) === current;
    runLoading[ref] = true;
    Reflect.deleteProperty(runProblems, ref);
    try {
      const organizationRef = requireRuntimeOrganizationRef(
        bootstrap.value?.organizationRef,
      );
      const graphReadback = await unwrap(
        getRunGraph({
          path: { runRef: ref },
          signal: requestSignal(ownerSignal),
        }),
      );
      if (!active()) return;
      const workspace = graphReadback.data;
      assertOwnerRequest(ownerSignal);
      if (
        requireRuntimeOrganizationRef(bootstrap.value?.organizationRef) !==
        organizationRef
      )
        throw new Error("Run request organization changed");
      assertRunOwner(workspace.run, organizationRef);
      if (
        workspace.run.ref !== ref ||
        workspace.graph.runRef !== workspace.run.rootRunRef
      )
        throw new Error("Run workspace identity mismatch");
      const history = await loadRunEventHistory(
        ref,
        workspace.graph.sequence,
        ownerSignal,
        active,
      );
      if (!active()) return;
      assertOwnerRequest(ownerSignal);
      if (
        requireRuntimeOrganizationRef(bootstrap.value?.organizationRef) !==
        organizationRef
      )
        throw new Error("Run request organization changed");

      upsert(runs, [workspace.run]);
      graphs[workspace.graph.runRef] = mergeRunGraph(
        graphs[workspace.graph.runRef],
        workspace.graph,
      );
      const bucket = events[workspace.graph.runRef] ?? {};
      for (const event of history) bucket[event.sequence] = event;
      events[workspace.graph.runRef] = bucket;
    } catch (error) {
      if (active()) runProblems[ref] = asProblem(error);
    } finally {
      if (active()) runLoading[ref] = false;
    }
  }

  async function loadRunEventHistory(
    ref: string,
    throughSequence: number,
    ownerSignal: AbortSignal,
    active: () => boolean,
  ): Promise<RunEvent[]> {
    const result: RunEvent[] = [];
    let afterSequence = 0;
    while (afterSequence < throughSequence) {
      assertOwnerRequest(ownerSignal);
      if (!active()) throw new Error(runHistorySupersededMessage);
      const response = await unwrap(
        listRunEvents({
          path: { runRef: ref },
          query: { afterSequence, limit: 500 },
          signal: requestSignal(ownerSignal),
        }),
      );
      assertOwnerRequest(ownerSignal);
      if (!active()) throw new Error(runHistorySupersededMessage);
      for (const event of response.data.items) {
        if (event.sequence > throughSequence) break;
        if (event.sequence !== afterSequence + 1)
          throw new Error("Run event history is not contiguous");
        result.push(event);
        afterSequence = event.sequence;
      }
      if (afterSequence >= throughSequence) break;
      if (response.data.complete || response.data.items.length === 0)
        throw new Error("Run event history ended before graph snapshot");
    }
    return result;
  }

  async function loadGates(
    projectRef?: string,
    runRef?: string,
  ): Promise<void> {
    await query(
      "gates",
      async () => {
        const values: OwnerGate[] = [];
        const visited = new Set<string>();
        let pageToken: string | undefined;
        do {
          const response = await unwrap(
            listOwnerGates({
              query: {
                ...(projectRef ? { projectRef } : {}),
                pageSize: 100,
                ...(pageToken ? { pageToken } : {}),
              },
              signal: requestSignal(),
            }),
          );
          for (const gate of response.data.items) {
            assertGateScope(gate, bootstrap.value?.organizationRef);
            if (projectRef && gate.projectRef !== projectRef)
              throw new Error("Owner gate project scope changed");
          }
          values.push(...response.data.items);
          pageToken = response.data.nextPageToken || undefined;
          if (pageToken) {
            if (visited.has(pageToken))
              throw new AppProblem({
                status: 502,
                code: "OWNER_GATE_CURSOR_REPEATED",
                retryable: false,
                kind: "unavailable",
              });
            visited.add(pageToken);
          }
        } while (pageToken);
        return values;
      },
      (values) => {
        if (runRef)
          replaceScoped(gates, values, (gate) => gate.runRef === runRef);
        else if (projectRef)
          replaceScoped(
            gates,
            values,
            (gate) => gate.projectRef === projectRef,
          );
        else replace(gates, values);
      },
    );
  }

  async function loadPendingGateCount(): Promise<void> {
    await query(
      "gateCount",
      async () =>
        (
          await unwrap(
            listOwnerGates({
              query: { states: ["OPEN"], pageSize: 1 },
              signal: requestSignal(),
              cache: "no-store",
            }),
          )
        ).data,
      (page) => {
        if (
          !Number.isSafeInteger(page.total) ||
          page.total < 0 ||
          page.items.length !== Math.min(page.total, 1) ||
          page.items.some((gate) => gate.state !== "OPEN")
        )
          throw new Error("Invalid owner gate count page");
        pendingGateCount.value = page.total;
        gateCatalogRevision.value += 1;
      },
    );
    if (problems.gateCount) pendingGateCount.value = undefined;
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
      (values) =>
        replaceScoped(
          artifacts,
          values,
          (artifact) => artifact.projectRef === projectRef,
        ),
    );
  }

  async function uploadProjectArtifact(
    projectRef: string,
    file: File,
    signal?: AbortSignal,
  ): Promise<Artifact> {
    const result = await mutate((headers) =>
      uploadArtifact({
        path: { projectRef },
        body: file,
        headers: {
          ...mutationHeaders(headers),
          "X-File-Name": file.name,
        },
        signal: requestSignal(signal),
      }),
    );
    upsert(artifacts, [result.data]);
    return result.data;
  }

  async function uploadOrganizationArtifactFile(
    file: File,
    signal?: AbortSignal,
  ): Promise<Artifact> {
    const result = await mutate((headers) =>
      uploadOrganizationArtifact({
        body: file,
        headers: {
          ...mutationHeaders(headers),
          "X-File-Name": file.name,
        },
        signal: requestSignal(signal),
      }),
    );
    upsert(artifacts, [result.data]);
    return result.data;
  }

  async function uploadAttachmentArtifact(
    projectRef: string | undefined,
    file: File,
    signal?: AbortSignal,
  ): Promise<Artifact> {
    return projectRef
      ? uploadProjectArtifact(projectRef, file, signal)
      : uploadOrganizationArtifactFile(file, signal);
  }

  async function readArtifact(
    artifactRef: string,
    parentSignal?: AbortSignal,
    expectedProjectRef?: string,
  ): Promise<Artifact> {
    const ownerSignal = ownerRequestSignal();
    const organizationRef = requireRuntimeOrganizationRef(
      bootstrap.value?.organizationRef,
    );
    const signal = requestSignal(parentSignal);
    assertOwnerRequest(ownerSignal);
    const result = await unwrap(
      getArtifact({
        path: { artifactRef },
        signal,
        cache: "no-store",
      }),
    );
    assertOwnerRequest(ownerSignal);
    signal.throwIfAborted();
    if (
      requireRuntimeOrganizationRef(bootstrap.value?.organizationRef) !==
      organizationRef
    )
      throw new Error("Artifact request organization changed");
    if (
      result.data.ref !== artifactRef ||
      (expectedProjectRef && result.data.projectRef !== expectedProjectRef)
    )
      throw new Error("Artifact readback identity mismatch");
    upsert(artifacts, [result.data]);
    return result.data;
  }

  async function deleteProjectArtifact(artifact: Artifact): Promise<Artifact> {
    const impactResult = await unwrap(
      getArtifactImpact({
        path: { artifactRef: artifact.ref },
        query: { action: "DELETE" },
        signal: requestSignal(),
      }),
    );
    const impact: ArtifactImpact = impactResult.data;
    if (
      !impact.permitted ||
      impact.action !== "DELETE" ||
      impact.artifactRef !== artifact.ref ||
      impact.artifactVersion !== artifact.version
    )
      throw new Error("Artifact impact does not authorize this mutation");
    const result = await mutate(
      (headers) =>
        deleteArtifact({
          path: { artifactRef: artifact.ref },
          headers: {
            ...versionedHeaders(headers),
            "X-Impact-Digest": impact.impactDigest,
          },
          signal: requestSignal(),
        }),
      artifact.version,
    );
    upsert(artifacts, [result.data]);
    return result.data;
  }

  async function changeArtifactAgentBinding(
    artifact: Artifact,
    agentRef: string,
    enabled: boolean,
  ): Promise<Artifact> {
    const result = await mutateWithRetry(
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
    Reflect.deleteProperty(agents, agentRef);
    return result.data;
  }

  async function downloadArtifactContent(
    artifactRef: string,
    purpose: "DOWNLOAD" | "PREVIEW",
  ): Promise<Blob> {
    for (const delayMs of [0, 200, 600]) {
      if (delayMs > 0) {
        await new Promise<void>((resolve) =>
          globalThis.setTimeout(resolve, delayMs),
        );
      }
      try {
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
      } catch (error) {
        const problem = asProblem(error);
        if (!problem.retryable || delayMs === 600) throw problem;
      }
    }
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
      (values) =>
        replaceScoped(
          schedules,
          values,
          (schedule) => schedule.projectRef === projectRef,
        ),
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
          coreReady: definitionPage.data.coreReady,
          definitionActions: definitionPage.data.nextActions,
          connections: connectionPage.data.items,
        };
      },
      (value) => {
        replaceByKey(definitions, value.definitions, (item) => item.key);
        integrationCoreReady.value = value.coreReady;
        integrationDefinitionActions.value = value.definitionActions;
        replace(connections, value.connections);
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
        reconcileConversations(value.conversations);
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
        ).data,
      (value) => {
        replace(memberships, value.items);
        projectMembershipActions.value = value.nextActions;
      },
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
    input: ProjectMembershipCreateInput & { active: boolean },
    current?: Membership,
  ): Promise<Membership> {
    const result = current
      ? await mutate(
          (headers) =>
            changeProjectMembership({
              path: { projectRef, membershipRef: current.ref },
              body: {
                permissions: [...input.permissions],
                active: input.active,
              } satisfies ProjectMembershipChangeInput,
              headers: versionedHeaders(headers),
              signal: requestSignal(),
            }),
          current.version,
        )
      : await mutate((headers) =>
          addProjectMembership({
            path: { projectRef },
            body: {
              userRef: input.userRef,
              permissions: [...input.permissions],
            } satisfies ProjectMembershipCreateInput,
            headers: mutationHeaders(headers),
            signal: requestSignal(),
          }),
        );
    memberships[result.data.ref] = result.data;
    return result.data;
  }

  async function loadPlatformMembers(): Promise<void> {
    await query(
      "platformMembers",
      async () =>
        (
          await unwrap(
            listPlatformMemberships({
              query: { pageSize: 100 },
              signal: requestSignal(),
            }),
          )
        ).data,
      (value) => {
        replace(platformMemberships, value.items);
        platformMembershipActions.value = value.nextActions;
      },
    );
  }

  async function loadPlatformMembershipCandidates(search = ""): Promise<void> {
    await query(
      "platformMemberCandidates",
      async () =>
        (
          await unwrap(
            listPlatformMembershipCandidates({
              query: { query: search, pageSize: 100 },
              signal: requestSignal(),
            }),
          )
        ).data.items,
      (values) => replace(platformMembershipCandidates, values),
    );
  }

  async function savePlatformMembership(
    input: PlatformMembershipCreateInput & { active: boolean },
    current?: Membership,
  ): Promise<Membership> {
    const result = current
      ? await mutate(
          (headers) =>
            changePlatformMembership({
              path: { membershipRef: current.ref },
              body: {
                platformRole: input.platformRole,
                active: input.active,
              } satisfies PlatformMembershipChangeInput,
              headers: versionedHeaders(headers),
              signal: requestSignal(),
            }),
          current.version,
        )
      : await mutate((headers) =>
          addPlatformMembership({
            body: {
              userRef: input.userRef,
              platformRole: input.platformRole,
            } satisfies PlatformMembershipCreateInput,
            headers: mutationHeaders(headers),
            signal: requestSignal(),
          }),
        );
    platformMemberships[result.data.ref] = result.data;
    return result.data;
  }

  async function revokePlatformMembership(
    membership: Membership,
  ): Promise<Membership> {
    const result = await mutate(
      (headers) =>
        removePlatformMembership({
          path: { membershipRef: membership.ref },
          headers: versionedHeaders(headers),
          signal: requestSignal(),
        }),
      membership.version,
    );
    platformMemberships[result.data.ref] = result.data;
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

  async function loadAudit(
    projectRef?: string,
    search = "",
    pageSize = 20,
    resourceRef = "",
    includeTechnical = true,
  ): Promise<void> {
    const normalizedSearch = search.trim();
    const scopeKey = `${projectRef ?? ""}\n${normalizedSearch}\n${resourceRef}\n${String(includeTechnical)}`;
    auditScopeKey.value = scopeKey;
    auditNextPageToken.value = undefined;
    consumedAuditPageTokens.clear();
    generation.set("auditMore", (generation.get("auditMore") ?? 0) + 1);
    loading.auditMore = false;
    Reflect.deleteProperty(problems, "auditMore");
    await query(
      "audit",
      async () =>
        (
          await unwrap(
            listAuditEvents({
              query: {
                ...(projectRef ? { projectRef } : {}),
                ...(resourceRef ? { resourceRef } : {}),
                ...(normalizedSearch ? { query: normalizedSearch } : {}),
                includeTechnical,
                pageSize,
              },
              signal: requestSignal(),
            }),
          )
        ).data,
      (page) => {
        if (auditScopeKey.value !== scopeKey) return;
        auditEvents.value = page.items;
        auditNextPageToken.value = page.nextPageToken || undefined;
      },
    );
  }

  async function loadMoreAudit(
    projectRef?: string,
    search = "",
    pageSize = 20,
    resourceRef = "",
    includeTechnical = true,
  ): Promise<void> {
    const normalizedSearch = search.trim();
    const scopeKey = `${projectRef ?? ""}\n${normalizedSearch}\n${resourceRef}\n${String(includeTechnical)}`;
    const pageToken = auditNextPageToken.value;
    if (
      !pageToken ||
      loading.auditMore ||
      auditScopeKey.value !== scopeKey ||
      consumedAuditPageTokens.has(pageToken)
    )
      return;
    await query(
      "auditMore",
      async () =>
        (
          await unwrap(
            listAuditEvents({
              query: {
                ...(projectRef ? { projectRef } : {}),
                ...(resourceRef ? { resourceRef } : {}),
                ...(normalizedSearch ? { query: normalizedSearch } : {}),
                includeTechnical,
                pageSize,
                pageToken,
              },
              signal: requestSignal(),
            }),
          )
        ).data,
      (page) => {
        if (
          auditScopeKey.value !== scopeKey ||
          auditNextPageToken.value !== pageToken
        )
          return;
        consumedAuditPageTokens.add(pageToken);
        const knownRefs = new Set(auditEvents.value.map((event) => event.ref));
        auditEvents.value.push(
          ...page.items.filter((event) => !knownRefs.has(event.ref)),
        );
        const candidate = page.nextPageToken || undefined;
        auditNextPageToken.value =
          candidate &&
          candidate !== pageToken &&
          !consumedAuditPageTokens.has(candidate)
            ? candidate
            : undefined;
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
          headers: versionedHeaders(headers),
          signal: requestSignal(),
        }),
      agent.version,
    );
    return readAgent(agent.ref);
  }

  async function instructionCommand(
    agent: Agent,
    action: "VALIDATE" | "PUBLISH" | "ROLLBACK",
    publishedInstructionRef?: string,
  ): Promise<Agent> {
    await mutate(
      (headers) =>
        commandAgentInstructions({
          path: { agentRef: agent.ref },
          body: instructionCommandInput(action, publishedInstructionRef),
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
    const result = await mutateWithRetry((headers) =>
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
    const ownerSignal = ownerRequestSignal();
    const organizationRef = requireRuntimeOrganizationRef(
      bootstrap.value?.organizationRef,
    );
    assertRunOwner(run, organizationRef);
    const result = await mutateWithRetry(
      (headers) =>
        commandRun({
          path: { runRef: run.ref },
          body,
          headers: versionedHeaders(headers),
          signal: requestSignal(),
        }),
      run.version,
    );
    assertOwnerRequest(ownerSignal);
    if (
      requireRuntimeOrganizationRef(bootstrap.value?.organizationRef) !==
      organizationRef
    )
      throw new Error("Run mutation organization changed");
    assertRunOwner(result.data.run, organizationRef);
    if (result.data.graph.runRef !== result.data.run.rootRunRef)
      throw new Error("Run mutation graph identity mismatch");
    if (body.action === "RETRY")
      assertAssistantRetryIdentity(run, result.data.run);
    else if (
      result.data.run.ref !== run.ref ||
      !sameAssistantRunPin(run, result.data.run)
    )
      throw new Error("Run mutation owner identity mismatch");
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
    assertGateScope(gate, bootstrap.value?.organizationRef);
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
    assertGateScope(result.data.gate, bootstrap.value?.organizationRef);
    if (
      result.data.gate.ref !== gate.ref ||
      result.data.gate.scopeKind !== gate.scopeKind ||
      result.data.gate.organizationRef !== gate.organizationRef ||
      result.data.gate.projectRef !== gate.projectRef
    )
      throw new Error("Owner gate resolution scope changed");
    gates[result.data.gate.ref] = result.data.gate;
    runs[result.data.run.ref] = result.data.run;
    graphs[result.data.graph.runRef] = result.data.graph;
    if (gate.state === "OPEN" && result.data.gate.state !== "OPEN") {
      if (pendingGateCount.value !== undefined)
        pendingGateCount.value = Math.max(0, pendingGateCount.value - 1);
      if (overview.value)
        overview.value = {
          ...overview.value,
          pendingGateCount: Math.max(0, overview.value.pendingGateCount - 1),
          pendingGates: overview.value.pendingGates.filter(
            (current) => current.ref !== result.data.gate.ref,
          ),
        };
      const project =
        gate.scopeKind === "PROJECT" && gate.projectRef
          ? projects[gate.projectRef]
          : undefined;
      if (project)
        project.pendingGateCount = Math.max(0, project.pendingGateCount - 1);
    }
    gateCatalogRevision.value += 1;
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

  async function configureConnectionCredential(
    connection: Pick<IntegrationConnection, "ref" | "version">,
    credentialValue: string,
    requestIdempotencyKey: string,
  ): Promise<IntegrationConnection> {
    try {
      const result = await unwrap(
        configureIntegrationConnectionCredential({
          path: { connectionRef: connection.ref },
          body: { value: credentialValue },
          headers: {
            "If-Match": etag(connection.version),
            "Idempotency-Key": requestIdempotencyKey,
            "X-CSRF-Token": csrfToken(),
          },
          signal: requestSignal(),
        }),
      );
      connections[result.data.ref] = result.data;
      return result.data;
    } catch (error) {
      throw asProblem(error);
    }
  }

  async function updateConnection(
    connection: IntegrationConnection,
    input: IntegrationConnectionUpdateInput,
  ): Promise<IntegrationConnection> {
    const result = await mutate(
      (headers) =>
        updateIntegrationConnection({
          path: { connectionRef: connection.ref },
          body: input,
          headers: versionedHeaders(headers),
          signal: requestSignal(),
        }),
      connection.version,
    );
    connections[result.data.ref] = result.data;
    return result.data;
  }

  async function deleteConnection(
    connection: IntegrationConnection,
  ): Promise<IntegrationConnection> {
    const result = await mutate(
      (headers) =>
        deleteIntegrationConnection({
          path: { connectionRef: connection.ref },
          headers: versionedHeaders(headers),
          signal: requestSignal(),
        }),
      connection.version,
    );
    connections[result.data.ref] = result.data;
    return result.data;
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
    graphs[graph.runRef] = mergeRunGraph(graphs[graph.runRef], graph);
  }

  function applyRunEvent(event: RunEvent): RunEventOutcome {
    return reduceRunEvent({ runs, graphs, events, gates, artifacts }, event);
  }

  function snapshotRecord(
    value: unknown,
    field: string,
  ): Record<string, unknown> {
    if (typeof value !== "object" || value === null || Array.isArray(value))
      throw new Error(`Invalid realtime snapshot ${field}`);
    return value as Record<string, unknown>;
  }

  function snapshotArray<T extends { ref: string }>(
    value: Record<string, unknown>,
    field: string,
  ): T[] {
    const items = value[field];
    if (items === undefined) return [];
    if (
      !Array.isArray(items) ||
      items.some(
        (item) =>
          typeof item !== "object" ||
          item === null ||
          Array.isArray(item) ||
          typeof (item as { ref?: unknown }).ref !== "string",
      )
    )
      throw new Error(`Invalid realtime snapshot ${field}`);
    return items as T[];
  }

  function snapshotKeyArray<T extends { key: string }>(
    value: Record<string, unknown>,
    field: string,
  ): T[] {
    const items = value[field];
    if (items === undefined) return [];
    if (
      !Array.isArray(items) ||
      items.some(
        (item) =>
          typeof item !== "object" ||
          item === null ||
          Array.isArray(item) ||
          typeof (item as { key?: unknown }).key !== "string",
      )
    )
      throw new Error(`Invalid realtime snapshot ${field}`);
    return items as T[];
  }

  function snapshotStringArray(
    value: Record<string, unknown>,
    field: string,
  ): NextAction[] {
    const items = value[field];
    if (items === undefined) return [];
    if (!Array.isArray(items) || items.some((item) => typeof item !== "string"))
      throw new Error(`Invalid realtime snapshot ${field}`);
    return items as NextAction[];
  }

  function applyOverviewSnapshot(snapshot: Record<string, unknown>): void {
    if (!("overview" in snapshot)) return;
    const response = snapshotRecord(snapshot.overview, "overview");
    const value = snapshotRecord(response.overview, "overview.overview");
    for (const run of (value as Overview).activeRuns)
      assertRunOwner(run, bootstrap.value?.organizationRef);
    for (const gate of (value as Overview).pendingGates)
      assertGateScope(gate, bootstrap.value?.organizationRef);
    overview.value = value as Overview;
    upsert(runs, overview.value.activeRuns);
    upsert(gates, overview.value.pendingGates);
    upsert(artifacts, overview.value.recentArtifacts);
    pendingGateCount.value = overview.value.pendingGateCount;
  }

  function applyBootstrapSnapshot(snapshot: Record<string, unknown>): void {
    if (!("bootstrap" in snapshot)) return;
    const response = snapshotRecord(snapshot.bootstrap, "bootstrap");
    const value = snapshotRecord(response.state, "bootstrap.state");
    if (
      bootstrap.value?.organizationRef !== value.organizationRef ||
      bootstrap.value?.platformRole !== value.platformRole
    )
      clearOrganizationCatalogs();
    bootstrap.value = value as BootstrapState;
  }

  function clearOrganizationCatalogs(): void {
    replace(organizationRoleImageRecipes, []);
    replace(organizationRuntimeSecrets, []);
    organizationRoleImagePage.value = undefined;
    organizationRuntimeSecretPage.value = undefined;
    organizationRoleImageRealtimeRevision.value += 1;
    organizationRuntimeSecretRealtimeRevision.value += 1;
  }

  function scopedSnapshotItems<
    T extends { ref: string } & RuntimeScopedResourceIdentity,
  >(
    catalog: Record<string, unknown>,
    field: string,
    scopeProjectRef?: string,
  ): T[] {
    if (!Array.isArray(catalog[field]))
      throw new Error(`Missing scoped realtime catalog ${field}`);
    const organization = organizationRuntimeResourceScope(bootstrap.value);
    if (!organization)
      throw new Error("Realtime organization anchor is unavailable");
    const values = snapshotArray<T>(catalog, field);
    const seen = new Set<string>();
    for (const value of values) {
      const scope = field.startsWith("organization")
        ? organization
        : {
            kind: "PROJECT" as const,
            projectRef: scopeProjectRef ?? value.projectRef ?? "",
          };
      assertRuntimeResourceIdentity(scope, value, organization.organizationRef);
      if (!value.ref || seen.has(value.ref))
        throw new Error("Duplicate scoped realtime resource");
      seen.add(value.ref);
    }
    return values;
  }

  function organizationSnapshotPage(
    catalog: Record<string, unknown>,
    field: string,
  ): RealtimeCatalogSnapshot {
    const scope = organizationRuntimeResourceScope(bootstrap.value);
    if (!scope) throw new Error("Realtime organization anchor is unavailable");
    const page = snapshotRecord(catalog[field], field);
    if (
      page.nextPageToken !== undefined &&
      (typeof page.nextPageToken !== "string" ||
        page.nextPageToken.length > 4096)
    )
      throw new Error("Invalid organization realtime cursor");
    return {
      scopeKey: `ORGANIZATION:${scope.organizationRef}`,
      ...(page.nextPageToken ? { nextPageToken: page.nextPageToken } : {}),
    };
  }

  function applySpeechAvailability(
    value: BootstrapState["speechTranscription"],
  ): void {
    if (!bootstrap.value) return;
    bootstrap.value = { ...bootstrap.value, speechTranscription: value };
  }

  function applyPlatformSnapshotValue(
    kind: PlatformResourceKind,
    scopeProjectRef: string | undefined,
    snapshot: Record<string, unknown>,
  ): void {
    if (scopeProjectRef !== selectedProjectRef())
      throw new Error("Realtime snapshot project scope changed");
    const catalog =
      "catalog" in snapshot
        ? snapshotRecord(snapshot.catalog, "catalog")
        : undefined;
    switch (kind) {
      case "PROJECT":
        if (!catalog) throw new Error("Project realtime catalog is missing");
        replace(projects, snapshotArray<Project>(catalog, "projects"));
        if (Array.isArray(catalog.trashedProjects)) {
          replace(
            trashedProjects,
            snapshotArray<Project>(catalog, "trashedProjects"),
          );
          const trashPage = snapshotRecord(catalog.trashPage, "trashPage");
          projectTrashNextPageToken.value =
            typeof trashPage.nextPageToken === "string"
              ? trashPage.nextPageToken || undefined
              : undefined;
        } else {
          replace(trashedProjects, []);
          projectTrashNextPageToken.value = undefined;
        }
        if ("selectedProject" in snapshot) {
          const response = snapshotRecord(
            snapshot.selectedProject,
            "selectedProject",
          );
          const selected = snapshotRecord(
            response.project,
            "selectedProject.project",
          ) as Project;
          if (!scopeProjectRef || selected.ref !== scopeProjectRef)
            throw new Error("Selected project realtime scope changed");
          projects[selected.ref] = selected;
        }
        projectCollectionActions.value = snapshotStringArray(
          catalog,
          "nextActions",
        );
        applyOverviewSnapshot(snapshot);
        return;
      case "AGENT":
      case "INSTRUCTIONS":
        if (!catalog) throw new Error("Agent realtime catalog is missing");
        if (scopeProjectRef)
          replaceScoped(
            agents,
            snapshotArray<Agent>(catalog, "agents"),
            (agent) => agent.projectRef === scopeProjectRef,
          );
        else replace(agents, snapshotArray<Agent>(catalog, "agents"));
        applyOverviewSnapshot(snapshot);
        return;
      case "WORKFLOW":
        if (!catalog) throw new Error("Workflow realtime catalog is missing");
        if (scopeProjectRef)
          replaceScoped(
            workflows,
            snapshotArray<Workflow>(catalog, "workflows"),
            (workflow) => workflow.projectRef === scopeProjectRef,
          );
        else replace(workflows, snapshotArray<Workflow>(catalog, "workflows"));
        return;
      case "RUN":
        if (!catalog) throw new Error("Run realtime catalog is missing");
        reconcileRuns(snapshotArray<Run>(catalog, "runs"));
        {
          const gateValues = snapshotArray<OwnerGate>(catalog, "gates");
          for (const gate of gateValues) {
            assertGateScope(gate, bootstrap.value?.organizationRef);
            if (scopeProjectRef && gate.projectRef !== scopeProjectRef)
              throw new Error("Owner gate realtime project scope changed");
          }
          if (scopeProjectRef)
            replaceScoped(
              gates,
              gateValues,
              (gate) => gate.projectRef === scopeProjectRef,
            );
          else replace(gates, gateValues);
          const gatePage = snapshotRecord(catalog.gatesPage, "gatesPage");
          ownerGateNextPageToken.value =
            typeof gatePage.nextPageToken === "string"
              ? gatePage.nextPageToken || undefined
              : undefined;
          gateCatalogRevision.value += 1;
        }
        applyOverviewSnapshot(snapshot);
        return;
      case "ARTIFACT":
        if (!catalog) throw new Error("Artifact realtime catalog is missing");
        if (scopeProjectRef)
          replaceScoped(
            artifacts,
            snapshotArray<Artifact>(catalog, "artifacts"),
            (artifact) => artifact.projectRef === scopeProjectRef,
          );
        else replace(artifacts, snapshotArray<Artifact>(catalog, "artifacts"));
        applyOverviewSnapshot(snapshot);
        return;
      case "SCHEDULE":
        if (!catalog) throw new Error("Schedule realtime catalog is missing");
        if (scopeProjectRef)
          replaceScoped(
            schedules,
            snapshotArray<Schedule>(catalog, "schedules"),
            (schedule) => schedule.projectRef === scopeProjectRef,
          );
        else replace(schedules, snapshotArray<Schedule>(catalog, "schedules"));
        return;
      case "INTEGRATION_CONNECTION":
      case "INTEGRATION_GRANT": {
        const definitionResponse = snapshotRecord(
          snapshot.definitions,
          "definitions",
        );
        const connectionResponse = snapshotRecord(
          snapshot.connections,
          "connections",
        );
        const definitionValues = definitionResponse.definitions;
        if (!Array.isArray(definitionValues))
          throw new Error("Integration definitions snapshot is invalid");
        replaceByKey(
          definitions,
          definitionValues as IntegrationDefinition[],
          (item) => item.key,
        );
        replace(
          connections,
          snapshotArray<IntegrationConnection>(
            connectionResponse,
            "connections",
          ),
        );
        integrationCoreReady.value = Boolean(definitionResponse.coreReady);
        integrationDefinitionActions.value = snapshotStringArray(
          definitionResponse,
          "nextActions",
        );
        const definitionPage = snapshotRecord(
          definitionResponse.page,
          "definitions.page",
        );
        const connectionPage = snapshotRecord(
          connectionResponse.page,
          "connections.page",
        );
        integrationDefinitionNextPageToken.value =
          typeof definitionPage.nextPageToken === "string"
            ? definitionPage.nextPageToken || undefined
            : undefined;
        integrationConnectionNextPageToken.value =
          typeof connectionPage.nextPageToken === "string"
            ? connectionPage.nextPageToken || undefined
            : undefined;
        integrationRealtimeRevision.value += 1;
        return;
      }
      case "MEMBERSHIP":
        if (!catalog) throw new Error("Membership realtime catalog is missing");
        replace(memberships, snapshotArray<Membership>(catalog, "memberships"));
        projectMembershipActions.value = snapshotStringArray(
          catalog,
          "nextActions",
        );
        return;
      case "PLATFORM_MEMBERSHIP":
        if (!catalog)
          throw new Error("Platform membership realtime catalog is missing");
        replace(
          platformMemberships,
          snapshotArray<Membership>(catalog, "memberships"),
        );
        platformMembershipActions.value = snapshotStringArray(
          catalog,
          "nextActions",
        );
        return;
      case "SYSTEM_ASSISTANT": {
        const assistantResponse = snapshotRecord(
          snapshot.assistant,
          "assistant",
        );
        const value = snapshotRecord(
          assistantResponse.assistant,
          "assistant.assistant",
        );
        const conversationResponse = snapshotRecord(
          snapshot.conversations,
          "conversations",
        );
        assistant.value = value as SystemAssistant;
        reconcileConversations(
          snapshotArray<AssistantConversation>(
            conversationResponse,
            "conversations",
          ),
        );
        const page = snapshotRecord(
          conversationResponse.page,
          "conversations.page",
        );
        assistantConversationNextPageToken.value =
          typeof page.nextPageToken === "string"
            ? page.nextPageToken || undefined
            : undefined;
        assistantRealtimeScopeKey.value = scopeProjectRef ?? "";
        applyBootstrapSnapshot(snapshot);
        return;
      }
      case "ROLE_IMAGE_RECIPE": {
        if (!catalog) throw new Error("Role image realtime catalog is missing");
        const projectRecipes = scopedSnapshotItems<RoleImageRecipe>(
          catalog,
          "recipes",
          scopeProjectRef,
        );
        const organizationRecipes = scopedSnapshotItems<RoleImageRecipe>(
          catalog,
          "organizationRecipes",
        );
        const organizationPage = organizationSnapshotPage(
          catalog,
          "organizationRecipesPage",
        );
        const environmentValues = snapshotKeyArray<RoleEnvironment>(
          catalog,
          "roleEnvironments",
        );
        if (!scopeProjectRef && projectRecipes.length)
          throw new Error("Project role images require exact selected project");
        if (scopeProjectRef)
          replaceScoped(
            roleImageRecipes,
            projectRecipes,
            (recipe) => recipe.projectRef === scopeProjectRef,
          );
        else replace(roleImageRecipes, []);
        replace(organizationRoleImageRecipes, organizationRecipes);
        organizationRoleImagePage.value = organizationPage;
        organizationRoleImageRealtimeRevision.value += 1;
        replaceByKey(
          roleEnvironments,
          environmentValues,
          (environment) => environment.key,
        );
        roleImageRealtimeRevision.value += 1;
        return;
      }
      case "RUNTIME_SECRET": {
        if (!catalog)
          throw new Error("Runtime secret realtime catalog is missing");
        const projectSecrets = normalizeSecretPage({
          items: scopedSnapshotItems<RuntimeSecret>(
            catalog,
            "secrets",
            scopeProjectRef,
          ),
        }).items;
        const organizationSecrets = normalizeSecretPage({
          items: scopedSnapshotItems<RuntimeSecret>(
            catalog,
            "organizationSecrets",
          ),
        }).items;
        const organizationPage = organizationSnapshotPage(
          catalog,
          "organizationSecretsPage",
        );
        if (scopeProjectRef)
          replaceScoped(
            runtimeSecrets,
            projectSecrets,
            (secret) => secret.projectRef === scopeProjectRef,
          );
        else replace(runtimeSecrets, projectSecrets);
        replace(organizationRuntimeSecrets, organizationSecrets);
        organizationRuntimeSecretPage.value = organizationPage;
        organizationRuntimeSecretRealtimeRevision.value += 1;
        return;
      }
      case "MANAGED_CONFIGURATION": {
        if (!catalog)
          throw new Error("Managed configuration realtime catalog is missing");
        const configurations = snapshotArray<ManagedConfigurationSummary>(
          catalog,
          "managedConfigurations",
        );
        const rawPages = catalog.managedConfigurationPages;
        if (!Array.isArray(rawPages))
          throw new Error("Managed configuration realtime pages are missing");
        const pages: Record<string, { total: number; nextPageToken?: string }> =
          {};
        const allowedKinds = new Set([
          "PROMPT_TEMPLATE",
          "ROLE_IMAGE",
          "INTEGRATION_DEFINITION",
          "SYSTEM_STT",
        ]);
        for (const rawPage of rawPages) {
          const page = snapshotRecord(rawPage, "managedConfigurationPages[]");
          if (
            typeof page.kind !== "string" ||
            !allowedKinds.has(page.kind) ||
            page.kind in pages ||
            !Number.isSafeInteger(page.total) ||
            Number(page.total) < 0 ||
            (page.nextPageToken !== undefined &&
              typeof page.nextPageToken !== "string")
          )
            throw new Error("Managed configuration realtime page is invalid");
          pages[page.kind] = {
            total: Number(page.total),
            ...(page.nextPageToken
              ? { nextPageToken: page.nextPageToken }
              : {}),
          };
        }
        replace(managedConfigurations, configurations);
        for (const kind of Object.keys(managedConfigurationPages))
          Reflect.deleteProperty(managedConfigurationPages, kind);
        Object.assign(managedConfigurationPages, pages);
        managedConfigurationRealtimeRevision.value += 1;
        return;
      }
      case "RUNTIME_SELECTION":
        if (!catalog)
          throw new Error("Runtime selection realtime catalog is missing");
        replace(runtimes, snapshotArray<RuntimeSelection>(catalog, "runtimes"));
        return;
      case "RUNTIME_ENVIRONMENT":
        if (!catalog)
          throw new Error("Runtime environment realtime catalog is missing");
        replaceByKey(
          roleEnvironments,
          snapshotKeyArray<RoleEnvironment>(catalog, "roleEnvironments"),
          (environment) => environment.key,
        );
        return;
      case "PROVIDER_ACCOUNT":
        return;
    }
  }

  function realtimeCatalogKey(
    kind: PlatformResourceKind,
    scopeProjectRef?: string,
  ): string {
    return `${kind}:${scopeProjectRef ?? ""}`;
  }

  function markRealtimeSnapshot(
    kind: PlatformResourceKind,
    scopeProjectRef: string | undefined,
    response?: Record<string, unknown>,
  ): void {
    const snapshot: RealtimeCatalogSnapshot = {
      scopeKey: scopeProjectRef ?? "",
    };
    if (response) {
      const total = response.total;
      const numericTotal =
        typeof total === "number"
          ? total
          : typeof total === "string" && /^\d+$/.test(total)
            ? Number(total)
            : undefined;
      if (
        numericTotal !== undefined &&
        Number.isSafeInteger(numericTotal) &&
        numericTotal >= 0
      )
        snapshot.total = numericTotal;
      const page = response.page;
      if (page !== undefined) {
        const projectedPage = snapshotRecord(page, `${kind}.page`);
        if (typeof projectedPage.nextPageToken === "string")
          snapshot.nextPageToken = projectedPage.nextPageToken || undefined;
      }
    }
    realtimeCatalogSnapshots[realtimeCatalogKey(kind, scopeProjectRef)] =
      snapshot;
  }

  function applyPlatformSnapshot(
    kind: PlatformResourceKind,
    scopeProjectRef: string | undefined,
    snapshot: Record<string, unknown>,
  ): void {
    applyPlatformSnapshotValue(kind, scopeProjectRef, snapshot);
    let response: Record<string, unknown> | undefined;
    if ("catalog" in snapshot)
      response = snapshotRecord(snapshot.catalog, "catalog");
    else if (kind === "SYSTEM_ASSISTANT")
      response = snapshotRecord(snapshot.conversations, "conversations");
    else if (kind === "INTEGRATION_CONNECTION" || kind === "INTEGRATION_GRANT")
      response = snapshotRecord(snapshot.connections, "connections");
    markRealtimeSnapshot(kind, scopeProjectRef, response);
  }

  function realtimeSnapshot(
    kind: PlatformResourceKind,
    scopeProjectRef?: string,
  ): RealtimeCatalogSnapshot | undefined {
    return realtimeCatalogSnapshots[realtimeCatalogKey(kind, scopeProjectRef)];
  }

  function applyRealtimeAvailability(
    kinds: PlatformResourceKind[],
    scopeProjectRef: string | undefined,
  ): void {
    if (scopeProjectRef !== selectedProjectRef())
      throw new Error("Realtime availability project scope changed");
    const available = new Set(kinds);
    realtimeAvailableKinds.value = [...available];
    for (const key of Object.keys(realtimeCatalogSnapshots)) {
      const [kind, scope = ""] = key.split(":", 2);
      if (
        scope === (scopeProjectRef ?? "") &&
        !available.has(kind as PlatformResourceKind)
      )
        Reflect.deleteProperty(realtimeCatalogSnapshots, key);
    }
    const clearScoped = <T extends { projectRef?: string }>(
      target: Record<string, T>,
    ): void => {
      for (const [ref, value] of Object.entries(target))
        if (!scopeProjectRef || value.projectRef === scopeProjectRef)
          Reflect.deleteProperty(target, ref);
    };
    if (!available.has("AGENT") && !available.has("INSTRUCTIONS"))
      clearScoped(agents);
    if (!available.has("WORKFLOW")) clearScoped(workflows);
    if (!available.has("ARTIFACT")) clearScoped(artifacts);
    if (!available.has("SCHEDULE")) clearScoped(schedules);
    if (!available.has("RUN")) clearScoped(runs);
    if (!available.has("ROLE_IMAGE_RECIPE")) {
      clearScoped(roleImageRecipes);
      replace(organizationRoleImageRecipes, []);
      organizationRoleImagePage.value = undefined;
      organizationRoleImageRealtimeRevision.value += 1;
    }
    if (!available.has("RUNTIME_SECRET")) {
      clearScoped(runtimeSecrets);
      replace(organizationRuntimeSecrets, []);
      organizationRuntimeSecretPage.value = undefined;
      organizationRuntimeSecretRealtimeRevision.value += 1;
    }
    if (!available.has("MANAGED_CONFIGURATION")) {
      replace(managedConfigurations, []);
      for (const kind of Object.keys(managedConfigurationPages))
        Reflect.deleteProperty(managedConfigurationPages, kind);
    }
    if (!available.has("MEMBERSHIP")) clearScoped(memberships);
    if (!available.has("PLATFORM_MEMBERSHIP")) {
      replace(platformMemberships, []);
      platformMembershipActions.value = [];
    }
    if (
      !available.has("INTEGRATION_CONNECTION") &&
      !available.has("INTEGRATION_GRANT")
    ) {
      replaceByKey(definitions, [], () => "");
      replace(connections, []);
      integrationDefinitionActions.value = [];
      integrationCoreReady.value = undefined;
      integrationDefinitionNextPageToken.value = undefined;
      integrationConnectionNextPageToken.value = undefined;
    }
    if (!available.has("RUNTIME_SELECTION")) replace(runtimes, []);
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
        add(
          "overview",
          inSelectedProject(projectRef, () => loadOverview(projectRef)),
        );
        add("gateCount", loadPendingGateCount);
        if (projectRef)
          add(
            "project",
            inSelectedProject(projectRef, () => loadProject(projectRef)),
          );
        break;
      case "AGENT":
      case "INSTRUCTIONS":
        if (projectRef)
          add(
            "agents",
            inSelectedProject(projectRef, () => loadAgents(projectRef)),
          );
        add(
          "overview",
          inSelectedProject(projectRef, () => loadOverview(projectRef)),
        );
        break;
      case "WORKFLOW":
        if (projectRef)
          add(
            "workflows",
            inSelectedProject(projectRef, () => loadWorkflows(projectRef)),
          );
        break;
      case "ARTIFACT":
        if (projectRef)
          add(
            "artifacts",
            inSelectedProject(projectRef, () => loadArtifacts(projectRef)),
          );
        add(
          "overview",
          inSelectedProject(projectRef, () => loadOverview(projectRef)),
        );
        break;
      case "SCHEDULE":
        if (projectRef)
          add(
            "schedules",
            inSelectedProject(projectRef, () => loadSchedules(projectRef)),
          );
        break;
      case "INTEGRATION_CONNECTION":
      case "INTEGRATION_GRANT":
        add("integrations", loadIntegrations);
        break;
      case "MEMBERSHIP":
        pendingGateCount.value = undefined;
        add("projects", loadProjects);
        add("gateCount", loadPendingGateCount);
        if (projectRef)
          add(
            "members",
            inSelectedProject(projectRef, () => loadMembers(projectRef)),
          );
        break;
      case "PLATFORM_MEMBERSHIP":
        pendingGateCount.value = undefined;
        add("platformMembers", loadPlatformMembers);
        add("projects", loadProjects);
        add("gateCount", loadPendingGateCount);
        if (projectRef)
          add(
            "members",
            inSelectedProject(projectRef, () => loadMembers(projectRef)),
          );
        break;
      case "SYSTEM_ASSISTANT":
        add("bootstrap", loadBootstrap);
        add("assistant", loadAssistant);
        break;
      case "ROLE_IMAGE_RECIPE":
        if (projectRef)
          add(
            "roleImages",
            inSelectedProject(projectRef, () =>
              loadRoleImageRecipes(projectRef),
            ),
          );
        break;
      case "RUN":
        add(
          "runs",
          inSelectedProject(projectRef, () => loadRuns(projectRef)),
        );
        add("gateCount", loadPendingGateCount);
        add(
          "overview",
          inSelectedProject(projectRef, () => loadOverview(projectRef)),
        );
        break;
      default:
        throw new Error("Unknown platform invalidation kind");
    }
    await runBoundedPlatformReload(operations);
    if (operations.some((operation) => problems[operation.key]))
      throw new Error("Authoritative platform reload failed");
  }

  function reloadPlatformState(): Promise<void> {
    if (platformReloadPromise && !platformReloadScope?.aborted)
      return platformReloadPromise;
    platformReloadScope = ownerRequestSignal();
    const reload = async (): Promise<void> => {
      pendingGateCount.value = undefined;
      const projectRef = selectedProjectRef();
      const operations: Array<{ key: QueryKey; run: () => Promise<void> }> = [
        { key: "bootstrap", run: loadBootstrap },
        {
          key: "overview",
          run: inSelectedProject(projectRef, () => loadOverview(projectRef)),
        },
        {
          key: "runs",
          run: inSelectedProject(projectRef, () => loadRuns(projectRef)),
        },
        { key: "gateCount", run: loadPendingGateCount },
        { key: "integrations", run: loadIntegrations },
        { key: "assistant", run: loadAssistant },
      ];
      if (projectRef) {
        operations.push(
          {
            key: "project",
            run: inSelectedProject(projectRef, () => loadProject(projectRef)),
          },
          {
            key: "agents",
            run: inSelectedProject(projectRef, () => loadAgents(projectRef)),
          },
          {
            key: "workflows",
            run: inSelectedProject(projectRef, () => loadWorkflows(projectRef)),
          },
          {
            key: "artifacts",
            run: inSelectedProject(projectRef, () => loadArtifacts(projectRef)),
          },
          {
            key: "schedules",
            run: inSelectedProject(projectRef, () => loadSchedules(projectRef)),
          },
          {
            key: "roleImages",
            run: inSelectedProject(projectRef, () =>
              loadRoleImageRecipes(projectRef),
            ),
          },
        );
      }
      await runBoundedPlatformReload(operations);
      if (operations.some((operation) => problems[operation.key]))
        throw new Error("Authoritative platform resync failed");
    };
    const current = reload();
    const tracked = current.finally(() => {
      if (platformReloadPromise === tracked) platformReloadPromise = undefined;
    });
    platformReloadPromise = tracked;
    return tracked;
  }

  function clearOwnerState(): void {
    resetOwnerRequests();
    cancelSearch();
    platformReloadPromise = undefined;
    platformReloadScope = undefined;
    generation.clear();
    runReadGeneration.clear();
    for (const target of [
      runtimes,
      projects,
      trashedProjects,
      agents,
      instructionVersions,
      roleEnvironments,
      roleImageRecipes,
      organizationRoleImageRecipes,
      roleImageBuilds,
      workflows,
      runs,
      graphs,
      events,
      gates,
      artifacts,
      schedules,
      runtimeSecrets,
      organizationRuntimeSecrets,
      managedConfigurations,
      runtimes,
      definitions,
      connections,
      memberships,
      membershipCandidates,
      platformMemberships,
      platformMembershipCandidates,
      conversations,
      runLoading,
      runProblems,
    ]) {
      for (const key of Object.keys(target))
        Reflect.deleteProperty(target, key);
    }
    for (const key of Object.keys(loading))
      Reflect.deleteProperty(loading, key);
    for (const key of Object.keys(problems))
      Reflect.deleteProperty(problems, key);
    for (const key of Object.keys(realtimeCatalogSnapshots))
      Reflect.deleteProperty(realtimeCatalogSnapshots, key);
    for (const kind of Object.keys(managedConfigurationPages))
      Reflect.deleteProperty(managedConfigurationPages, kind);
    bootstrap.value = undefined;
    overview.value = undefined;
    administration.value = undefined;
    capabilities.value = [];
    searchResults.value = [];
    searchNextPageToken.value = undefined;
    searchTotal.value = 0;
    activeSearchQuery.value = "";
    consumedSearchPageTokens.clear();
    platformMembershipActions.value = [];
    projectMembershipActions.value = [];
    projectCollectionActions.value = [];
    projectTrashNextPageToken.value = undefined;
    integrationDefinitionActions.value = [];
    integrationCoreReady.value = undefined;
    integrationDefinitionNextPageToken.value = undefined;
    integrationConnectionNextPageToken.value = undefined;
    integrationRealtimeRevision.value = 0;
    ownerGateNextPageToken.value = undefined;
    pendingGateCount.value = undefined;
    gateCatalogRevision.value = 0;
    roleImageRealtimeRevision.value = 0;
    clearOrganizationCatalogs();
    managedConfigurationRealtimeRevision.value = 0;
    assistant.value = undefined;
    assistantConversationNextPageToken.value = undefined;
    assistantRealtimeScopeKey.value = undefined;
    auditEvents.value = [];
    auditNextPageToken.value = undefined;
    auditScopeKey.value = "";
    consumedAuditPageTokens.clear();
    selectProjectRef(undefined);
  }

  const projectList = computed(() => Object.values(projects));
  const projectTrashList = computed(() => Object.values(trashedProjects));
  const runList = computed(() => Object.values(runs));
  const gateList = computed(() => Object.values(gates));

  return {
    bootstrap,
    overview,
    administration,
    capabilities,
    runtimes,
    searchResults,
    searchNextPageToken,
    searchTotal,
    projects,
    trashedProjects,
    agents,
    instructionVersions,
    roleEnvironments,
    roleImageRecipes,
    organizationRoleImageRecipes,
    organizationRoleImagePage,
    organizationRoleImageRealtimeRevision,
    roleImageRealtimeRevision,
    roleImageBuilds,
    workflows,
    runs,
    graphs,
    events,
    gates,
    pendingGateCount,
    gateCatalogRevision,
    ownerGateNextPageToken,
    artifacts,
    schedules,
    runtimeSecrets,
    organizationRuntimeSecrets,
    organizationRuntimeSecretPage,
    organizationRuntimeSecretRealtimeRevision,
    managedConfigurations,
    managedConfigurationPages,
    managedConfigurationRealtimeRevision,
    definitions,
    connections,
    memberships,
    membershipCandidates,
    platformMemberships,
    platformMembershipCandidates,
    platformMembershipActions,
    projectMembershipActions,
    projectCollectionActions,
    projectTrashNextPageToken,
    integrationDefinitionActions,
    integrationCoreReady,
    integrationDefinitionNextPageToken,
    integrationConnectionNextPageToken,
    integrationRealtimeRevision,
    conversations,
    assistantConversationNextPageToken,
    assistantRealtimeScopeKey,
    realtimeCatalogSnapshots,
    assistant,
    auditEvents,
    auditNextPageToken,
    loading,
    problems,
    runLoading,
    runProblems,
    projectList,
    projectTrashList,
    runList,
    gateList,
    loadBootstrap,
    applyAuthenticatedBootstrap,
    loadOverview,
    search,
    loadMoreSearch,
    cancelSearch,
    loadProjects,
    loadProject,
    loadAgents,
    loadAgent,
    loadInstructionVersions,
    loadRoleEnvironments,
    loadRoleImageRecipes,
    loadRoleImageRecipe,
    loadWorkflows,
    loadWorkflow,
    loadRuns,
    loadRun,
    loadGates,
    loadPendingGateCount,
    loadArtifacts,
    uploadProjectArtifact,
    uploadAttachmentArtifact,
    readArtifact,
    deleteProjectArtifact,
    changeArtifactAgentBinding,
    downloadArtifactContent,
    loadSchedules,
    loadIntegrations,
    loadConnection,
    readConnection,
    loadAssistant,
    loadMembers,
    loadMembershipCandidates,
    saveMembership,
    revokeMembership,
    loadPlatformMembers,
    loadPlatformMembershipCandidates,
    savePlatformMembership,
    revokePlatformMembership,
    loadAdministration,
    loadAudit,
    loadMoreAudit,
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
    configureConnectionCredential,
    updateConnection,
    deleteConnection,
    changeConnection,
    changeConnectionGrant,
    updateAssistantInstructions,
    applyRunSnapshot,
    applyRunEvent,
    applyPlatformSnapshot,
    markRealtimeSnapshot,
    realtimeSnapshot,
    realtimeAvailableKinds,
    applyRealtimeAvailability,
    applySpeechAvailability,
    reloadPlatformKind,
    reloadPlatformState,
    clearOwnerState,
  };
});
