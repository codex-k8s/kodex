<script setup lang="ts">
import { useServerMessage } from "@/shared/ui/server-message";
import { runListSummary } from "@/shared/ui/run-summary";
import { ownerRequestSignal } from "@/shared/api/owner-lifetime";
import VoiceTextarea from "@/shared/ui/VoiceTextarea.vue";
import {
  Activity,
  Bot,
  ChevronDown,
  ListChecks,
  PanelRightOpen,
} from "@lucide/vue";
import {
  type ComponentPublicInstance,
  computed,
  nextTick,
  onBeforeUnmount,
  onMounted,
  ref,
  watch,
} from "vue";
import { useI18n } from "vue-i18n";
import { useRoute, useRouter } from "vue-router";
import {
  authoritativeRunRefreshKey,
  createRunRefreshScheduler,
} from "@/features/platform/run-refresh";
import { usePlatformStore } from "@/features/platform/store";
import { useRealtimeStore } from "@/features/realtime/store";
import { requestAssistantRunDebug } from "@/features/assistant/events";
import { isTerminalRun } from "@/features/workboard/model";
import { hasValidGateScope } from "@/features/workboard/gate-scope";
import RunActivityDrawer from "@/features/runs/RunActivityDrawer.vue";
import RunGraphCanvas from "@/features/runs/RunGraphCanvas.vue";
import RunNodeInspector from "@/features/runs/RunNodeInspector.vue";
import RunSessionDetailsDialog from "@/features/runs/RunSessionDetailsDialog.vue";
import RunTokenUsage from "@/features/runs/RunTokenUsage.vue";
import {
  hydrateRunArtifacts,
  runArtifactReferences,
} from "@/features/runs/run-artifacts";
import type { PresentedRunEvent } from "@/features/runs/run-activity";
import { runNodeExecutionLabels } from "@/features/runs/run-owner";
import {
  presentRuntimeText,
  runtimeProgressKey,
} from "@/features/runs/runtime-text";
import {
  indexRunSessionOwnership,
  projectRunSessionGraph,
  resolveRunSessionSelection,
} from "@/features/runs/run-session-graph";
import type {
  Artifact,
  OwnerGate,
  Run,
  RunEvent,
  RunGraph,
  RunNode,
} from "@/shared/api/generated/openapi/types.gen";
import { AppProblem, asProblem } from "@/shared/api/problem";
import { runPath } from "@/shared/routes";
import AsyncState from "@/shared/ui/AsyncState.vue";
import AttachmentComposer from "@/shared/ui/AttachmentComposer.vue";
import type {
  AttachmentComposerHandle,
  AttachmentComposerState,
} from "@/shared/ui/attachment-composer";
import PageFrame from "@/shared/ui/PageFrame.vue";
import ModalDialog from "@/shared/ui/ModalDialog.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import SafeMarkdown from "@/shared/ui/SafeMarkdown.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";
const platform = usePlatformStore();
const realtime = useRealtimeStore();
const route = useRoute();
const router = useRouter();
const translator = useI18n();
const serverMessage = useServerMessage();
const runRef = computed(() => String(route.params.runRef));
const requestedNodeRef = computed(() =>
  typeof route.query.nodeRef === "string" ? route.query.nodeRef : undefined,
);
const routeProjectRef = computed(() =>
  typeof route.params.projectRef === "string"
    ? route.params.projectRef
    : undefined,
);
const run = computed(() => platform.runs[runRef.value]);
const graph = computed(
  () =>
    platform.graphs[run.value?.rootRunRef ?? runRef.value] ??
    platform.graphs[runRef.value],
);
const streamState = computed(
  () => realtime.state[graph.value?.runRef ?? runRef.value],
);
function safeRuntimeText(
  value?: string,
  messageKind?: RunEvent["messageKind"],
): string | undefined {
  const progressKey = runtimeProgressKey(value);
  if (progressKey) return translator.t(progressKey);
  return presentRuntimeText(value, serverMessage, messageKind);
}

const runSubtitle = computed(
  () =>
    safeRuntimeText(run.value?.currentActivity) ??
    run.value?.target.displayName,
);
const runInputSummary = computed(() =>
  safeRuntimeText(run.value?.inputSummary),
);

const presentedGraph = computed(() =>
  graph.value
    ? {
        ...graph.value,
        nodes: graph.value.nodes.map((node) => ({
          ...node,
          role:
            safeRuntimeText(node.role) ??
            translator.t(`runs.nodeTypes.${node.type}`),
          inputSummary: safeRuntimeText(node.inputSummary),
          progressSummary: safeRuntimeText(node.progressSummary),
        })),
        edges: graph.value.edges.map((edge) => ({
          ...edge,
          label: safeRuntimeText(edge.label) ?? "",
        })),
      }
    : undefined,
);
const sessionGraph = computed(() =>
  presentedGraph.value
    ? projectRunSessionGraph(presentedGraph.value)
    : undefined,
);
const allRunNodes = computed(() => presentedGraph.value?.nodes ?? []);
const nodeExecutionLabels = computed(() =>
  runNodeExecutionLabels(
    allRunNodes.value,
    platform.runs,
    platform.bootstrap?.organizationRef,
  ),
);
const sessionOwnership = computed(() =>
  indexRunSessionOwnership(allRunNodes.value),
);

function stateLabel(state?: string): string | undefined {
  return state && translator.te(`states.${state}`)
    ? translator.t(`states.${state}`)
    : undefined;
}

function eventFallback(event: RunEvent): string {
  switch (event.type) {
    case "RUN_CREATED":
    case "TURN_QUEUED":
      return translator.t("runs.queued");
    case "RUN_STATE_CHANGED":
      return stateLabel(event.runState) ?? translator.t("runs.activity");
    case "NODE_ADDED":
      return event.node?.displayName ?? translator.t("runs.context");
    case "NODE_STATE_CHANGED":
      return (
        stateLabel(event.nodeState) ??
        event.node?.displayName ??
        translator.t("runs.activity")
      );
    case "EDGE_ADDED":
      return (
        safeRuntimeText(event.edge?.label) ?? translator.t("runs.connections")
      );
    case "TURN_STARTED":
    case "TURN_PROGRESS":
      return translator.t("states.RUNNING");
    case "TURN_COMPLETED":
      return translator.t("states.SUCCEEDED");
    case "DELEGATION_CREATED":
      return translator.t("runs.source.AGENT_DELEGATION");
    case "CALLBACK_DELIVERED":
      return translator.t("runs.callback");
    case "OWNER_GATE_OPENED":
      return translator.t("states.WAITING_HUMAN");
    case "OWNER_GATE_RESOLVED":
      return stateLabel(event.gate?.state) ?? translator.t("runs.activity");
    case "ARTIFACT_AVAILABLE":
      return translator.t("runs.artifacts");
    case "INCIDENT_LINKED":
      return translator.t("runs.incidents");
    default:
      return translator.t("runs.activity");
  }
}

const eventList = computed<PresentedRunEvent[]>(() =>
  Object.values(platform.events[graph.value?.runRef ?? runRef.value] ?? {})
    .sort((a, b) => a.sequence - b.sequence)
    .map((event) => ({
      ...event,
      displaySummary:
        safeRuntimeText(event.summary, event.messageKind) ??
        eventFallback(event),
      displayProgress: safeRuntimeText(event.progress, event.messageKind),
    })),
);
const gateList = computed(() =>
  Object.values(platform.gates).filter(
    (g) => g.runRef === runRef.value || g.runRef === run.value?.rootRunRef,
  ),
);
const openGateList = computed(() =>
  gateList.value.filter(
    (gate) =>
      gate.state === "OPEN" &&
      hasValidGateScope(gate, platform.bootstrap?.organizationRef),
  ),
);
const artifactList = computed(() =>
  Object.values(platform.artifacts).filter((artifact) =>
    artifactRefs.value.has(artifact.ref),
  ),
);
const artifactRefs = computed(
  () =>
    new Set(
      run.value && graph.value
        ? runArtifactReferences(run.value, graph.value)
        : [],
    ),
);
let artifactController: AbortController | undefined;
let artifactHydrationKey: string | undefined;
let artifactHydrationPromise: Promise<void> | undefined;
const incidentList = computed(() => run.value?.incidents ?? []);
const selectedRef = ref<string>();
const openedStreamRef = ref<string>();
const selectedNode = computed(() =>
  sessionGraph.value?.nodes.find((node) => node.ref === selectedRef.value),
);
const selectedAgent = computed(() =>
  selectedNode.value?.agentRef
    ? platform.agents[selectedNode.value.agentRef]
    : undefined,
);
const selectedRun = computed(() =>
  selectedNode.value
    ? (platform.runs[selectedNode.value.runRef] ?? run.value)
    : run.value,
);
const futureNodeRefs = computed(() =>
  (sessionGraph.value?.nodes ?? [])
    .filter(
      (node) =>
        (node.state === "QUEUED" || node.state === "WAITING") &&
        !node.startedAt,
    )
    .map((node) => node.ref),
);
const activeNodeRefs = computed(() =>
  (sessionGraph.value?.nodes ?? [])
    .filter((node) => node.state === "RUNNING")
    .map((node) => node.ref),
);
const lifecycleState = computed(() => run.value?.state);

const resultOutcomeState = computed(() => {
  if (!run.value) return undefined;
  if (run.value.state === "FAILED") return "OUTCOME_FAILED";
  if (run.value.state === "CANCELLED") return "OUTCOME_CANCELLED";
  if (
    run.value.safeErrorCode ||
    incidentList.value.some((incident) => incident.coreAffected)
  )
    return "OUTCOME_NEEDS_ATTENTION";
  return run.value.state === "SUCCEEDED" ? "OUTCOME_SUCCEEDED" : undefined;
});

const failedDiagnosticNodes = computed(() =>
  (graph.value?.nodes ?? []).filter((node) => node.state === "FAILED"),
);

function delegateDiagnostics(): void {
  if (!run.value || failedDiagnosticNodes.value.length === 0) return;
  requestAssistantRunDebug(run.value, failedDiagnosticNodes.value);
}

const turn = ref("");
const comments = ref<Record<string, string>>({});
const turnAttachmentComposer = ref<AttachmentComposerHandle>();
const turnAttachmentState = ref<AttachmentComposerState>({
  count: 0,
  uploadedCount: 0,
  totalBytes: 0,
  busy: false,
  hasErrors: false,
  overLimit: false,
  ready: true,
});
const gateAttachmentStates = ref<Record<string, AttachmentComposerState>>({});
const gateAttachmentComposers = new Map<string, AttachmentComposerHandle>();
const busy = ref(false);
let mutationGeneration = 0;
function mutationCurrent(generation: number, ref: string): boolean {
  return generation === mutationGeneration && runRef.value === ref;
}
const downloadBusyRef = ref("");
const problem = ref<AppProblem>();
const artifactProblem = ref<AppProblem>();
const activityOpen = ref(false);
const summaryExpanded = ref(false);
const activityNodeRef = ref<string>();
const activityDrawer = ref<HTMLElement>();
const nodeInspectorOpen = ref(false);
const nodeDetailsOpen = ref(false);
const gateDialogOpen = ref(false);
const activityTrigger = ref<HTMLButtonElement>();
const hasAuthoritativeSnapshot = computed(() =>
  Boolean(run.value && graph.value),
);
const fatalLoadProblem = computed(() =>
  hasAuthoritativeSnapshot.value
    ? undefined
    : platform.runProblems[runRef.value],
);
const refreshProblem = computed(() =>
  hasAuthoritativeSnapshot.value
    ? (artifactProblem.value ??
      platform.runProblems[runRef.value] ??
      platform.problems.gates ??
      platform.problems.artifacts)
    : undefined,
);
const refreshKey = computed(() => {
  const rootRef = graph.value?.runRef ?? run.value?.rootRunRef ?? runRef.value;
  return authoritativeRunRefreshKey(run.value, platform.events[rootRef] ?? {});
});

watch(
  [sessionGraph, requestedNodeRef],
  ([snapshot, nodeRef]) => {
    const requestedSessionRef = nodeRef
      ? sessionOwnership.value.get(nodeRef)
      : undefined;
    selectedRef.value = resolveRunSessionSelection(
      snapshot?.nodes ?? [],
      sessionOwnership.value,
      selectedRef.value,
      nodeRef,
    );
    if (requestedSessionRef) nodeInspectorOpen.value = true;
  },
  { immediate: true },
);

async function hydrateSnapshotArtifacts(
  snapshot: Run,
  snapshotGraph: RunGraph,
  missingOnly = false,
): Promise<void> {
  const refs = runArtifactReferences(snapshot, snapshotGraph).filter(
    (ref) => !missingOnly || !platform.artifacts[ref],
  );
  if (!refs.length) return;
  const key = `${snapshot.ref}:${refs.join(",")}`;
  if (artifactHydrationKey === key && artifactHydrationPromise)
    return artifactHydrationPromise;
  artifactController?.abort();
  const controller = new AbortController();
  artifactController = controller;
  artifactHydrationKey = key;
  artifactProblem.value = undefined;
  const reading = hydrateRunArtifacts(
    refs,
    (artifactRef) =>
      platform.readArtifact(
        artifactRef,
        controller.signal,
        snapshot.projectRef,
      ),
    controller.signal,
  );
  artifactHydrationPromise = reading;
  try {
    await reading;
  } catch (error) {
    if (runRef.value === snapshot.ref && !controller.signal.aborted)
      artifactProblem.value = asProblem(error);
    controller.abort();
    throw error;
  } finally {
    if (artifactHydrationPromise === reading) {
      artifactHydrationPromise = undefined;
      artifactHydrationKey = undefined;
    }
  }
}

async function refreshAuthoritativeState(ref: string): Promise<void> {
  if (!platform.bootstrap) await platform.loadBootstrap();
  if (runRef.value !== ref) return;
  await platform.loadRun(ref);
  if (runRef.value !== ref) return;
  if (platform.runProblems[ref]) throw platform.runProblems[ref];
  const snapshot = platform.runs[ref];
  if (!snapshot) return;
  const snapshotGraph =
    platform.graphs[snapshot.rootRunRef] ?? platform.graphs[ref];
  if (!snapshotGraph) throw new Error("Run artifact graph is unavailable");
  await Promise.all([
    platform.loadGates(snapshot.projectRef, snapshot.rootRunRef),
    hydrateSnapshotArtifacts(snapshot, snapshotGraph),
  ]);
  if (runRef.value !== ref) return;
  const relatedProblem = platform.problems.gates;
  if (relatedProblem) throw relatedProblem;
}
const refreshScheduler = createRunRefreshScheduler(refreshAuthoritativeState, {
  shouldRetry: (error) => error instanceof AppProblem && error.retryable,
});
let lastRefreshKey: string | undefined;

async function load(ref = runRef.value): Promise<void> {
  await refreshScheduler.request(ref);
}
async function command(action: "CANCEL" | "RETRY") {
  const current = run.value;
  if (busy.value || !current?.nextActions.includes(action)) return;
  const generation = mutationGeneration;
  const project = routeProjectRef.value ?? current.projectRef;
  busy.value = true;
  problem.value = undefined;
  try {
    const next = await platform.changeRun(current, { action });
    if (!mutationCurrent(generation, current.ref)) return;
    if (action === "RETRY" && next.ref !== runRef.value)
      await router.replace(runPath(next.ref, project));
  } catch (error) {
    if (mutationCurrent(generation, current.ref))
      problem.value = asProblem(error);
  } finally {
    if (mutationCurrent(generation, current.ref)) busy.value = false;
  }
}
async function continueRun() {
  const current = run.value;
  if (
    busy.value ||
    !current?.nextActions.includes("ADD_TURN") ||
    !turn.value.trim() ||
    !turnAttachmentState.value.ready
  )
    return;
  const generation = mutationGeneration;
  const project = routeProjectRef.value ?? current.projectRef;
  const composer = turnAttachmentComposer.value;
  const sessionRef = current.sessionRef;
  const input = {
    runRef: current.ref,
    nodeRef: selectedNode.value?.ref,
    task: turn.value.trim(),
  };
  busy.value = true;
  problem.value = undefined;
  try {
    const attachmentSetRef = await composer?.finalize();
    if (!mutationCurrent(generation, current.ref)) return;
    const next = await platform.continueSession(sessionRef, {
      ...input,
      ...(attachmentSetRef ? { attachmentSetRef } : {}),
    });
    if (!mutationCurrent(generation, current.ref)) return;
    turn.value = "";
    composer?.clear();
    if (next.ref !== runRef.value)
      await router.replace(runPath(next.ref, project));
  } catch (error) {
    if (mutationCurrent(generation, current.ref))
      problem.value = asProblem(error);
  } finally {
    if (mutationCurrent(generation, current.ref)) busy.value = false;
  }
}
async function decide(
  gate: OwnerGate,
  decision: "APPROVE" | "REJECT" | "REQUEST_CHANGES" | "CANCEL",
) {
  if (
    busy.value ||
    !gate.nextActions.includes("RESOLVE_GATE") ||
    !gate.allowedDecisions.includes(decision)
  )
    return;
  const generation = mutationGeneration;
  const project = run.value?.projectRef;
  const scopeRef = runRef.value;
  const rootRun = run.value?.rootRunRef;
  const comment = comments.value[gate.ref]?.trim() || undefined;
  const composer = gateAttachmentComposers.get(gate.ref);
  const currentGate = { ...gate };
  busy.value = true;
  problem.value = undefined;
  try {
    const attachmentSetRef = await composer?.finalize();
    if (!mutationCurrent(generation, scopeRef)) return;
    await platform.decide(currentGate, {
      decision,
      comment,
      ...(attachmentSetRef ? { attachmentSetRef } : {}),
    });
    if (!mutationCurrent(generation, scopeRef)) return;
    Reflect.deleteProperty(comments.value, gate.ref);
    Reflect.deleteProperty(gateAttachmentStates.value, gate.ref);
  } catch (error) {
    if (!mutationCurrent(generation, scopeRef)) return;
    problem.value = asProblem(error);
    await platform.loadGates(project, rootRun);
  } finally {
    if (mutationCurrent(generation, scopeRef)) busy.value = false;
  }
}
function setGateAttachmentComposer(
  gateRef: string,
  component: Element | ComponentPublicInstance | null,
): void {
  const handle = component as AttachmentComposerHandle | null;
  if (handle && typeof handle.finalize === "function")
    gateAttachmentComposers.set(gateRef, handle);
  else gateAttachmentComposers.delete(gateRef);
}
function gateAttachmentsReady(gateRef: string): boolean {
  return gateAttachmentStates.value[gateRef]?.ready ?? true;
}
function gateNodeName(gate: OwnerGate): string {
  return (
    presentedGraph.value?.nodes.find((node) => node.ref === gate.nodeRef)
      ?.displayName ?? translator.t("decisions.openNode")
  );
}
function gateIntegrationRisk(gate: OwnerGate): string {
  const risk = gate.integrationIntent?.effectPreview.risk;
  return risk === "READ" ||
    risk === "WRITE" ||
    risk === "SENSITIVE" ||
    risk === "DESTRUCTIVE"
    ? risk
    : "UNKNOWN";
}
function gateDisplayTitle(gate: OwnerGate): string {
  if (!gate.integrationIntent) return serverMessage(gate.title);
  const risk = gateIntegrationRisk(gate);
  const key =
    risk === "READ"
      ? "decisions.integrationReadTitle"
      : risk === "WRITE"
        ? "decisions.integrationWriteTitle"
        : "decisions.integrationActionTitle";
  return translator.t(key, {
    connection: gate.integrationIntent.connectionName,
  });
}
function gateDisplayQuestion(gate: OwnerGate): string {
  if (!gate.integrationIntent) return serverMessage(gate.contextSummary);
  return translator.t("decisions.integrationQuestion", {
    connection: gate.integrationIntent.connectionName,
    risk: translator.t(
      `decisions.integrationRisk.${gateIntegrationRisk(gate)}`,
    ),
  });
}
function gateScopeFields(
  gate: OwnerGate,
): Array<{ path: string; value: unknown }> {
  const scope = gate.integrationIntent?.effectPreview.approvalScope;
  if (!scope || typeof scope !== "object" || Array.isArray(scope)) return [];
  const selected = (scope as Record<string, unknown>).selected;
  if (!Array.isArray(selected)) return [];
  const entries: unknown[] = selected;
  return entries.filter(
    (field): field is { path: string; value: unknown } =>
      field !== null &&
      typeof field === "object" &&
      !Array.isArray(field) &&
      typeof (field as Record<string, unknown>).path === "string" &&
      "value" in field,
  );
}
function gateScopeValue(value: unknown): string {
  return typeof value === "string" ? value : JSON.stringify(value);
}
function gateScopePath(path: string): string {
  return path
    .split("/")
    .filter(Boolean)
    .map((part) => part.replaceAll("~1", "/").replaceAll("~0", "~"))
    .join(".");
}
function inspectGateNode(gate: OwnerGate): void {
  const sessionRef = sessionOwnership.value.get(gate.nodeRef);
  const node = sessionGraph.value?.nodes.find(
    (candidate) => candidate.ref === sessionRef,
  );
  if (!node) return;
  select(node);
  gateDialogOpen.value = false;
  nodeInspectorOpen.value = true;
}
function select(node: RunNode) {
  selectedRef.value = node.ref;
}
function openNodeDetails(node: RunNode): void {
  select(node);
  nodeDetailsOpen.value = true;
}
function openActivity(nodeRef?: string): void {
  activityNodeRef.value = nodeRef;
  activityOpen.value = true;
  void nextTick(() => activityDrawer.value?.focus());
  // Terminal WS delta может прийти раньше авторитетного Run readback с
  // вычисленными nextActions. Drawer всегда освежает eligibility продолжения.
  void refreshScheduler.request(runRef.value);
}
function closeActivity(): void {
  activityOpen.value = false;
  void nextTick(() => activityTrigger.value?.focus());
}
function openNodeActivity(nodeRef: string): void {
  nodeInspectorOpen.value = false;
  openActivity(nodeRef);
}
function openSelectedDetails(): void {
  nodeInspectorOpen.value = false;
  nodeDetailsOpen.value = true;
}
async function downloadArtifact(artifact: Artifact): Promise<void> {
  if (!artifact.nextActions.includes("DOWNLOAD")) return;
  downloadBusyRef.value = artifact.ref;
  problem.value = undefined;
  try {
    const body = await platform.downloadArtifactContent(
      artifact.ref,
      "DOWNLOAD",
    );
    const url = URL.createObjectURL(body);
    const anchor = document.createElement("a");
    anchor.href = url;
    anchor.download = artifact.fileName;
    anchor.hidden = true;
    document.body.append(anchor);
    anchor.click();
    anchor.remove();
    URL.revokeObjectURL(url);
  } catch (error) {
    problem.value = asProblem(error);
  } finally {
    downloadBusyRef.value = "";
  }
}
function openCurrentStream(): void {
  if (openedStreamRef.value) realtime.closeRun(openedStreamRef.value);
  const ref = run.value?.rootRunRef ?? runRef.value;
  realtime.openRun(ref);
  openedStreamRef.value = ref;
}
watch(
  [runRef, () => platform.bootstrap?.organizationRef, hasAuthoritativeSnapshot],
  (
    [ref, organizationRef, hasSnapshot],
    [previousRef, previousOrganizationRef, hadSnapshot],
  ) => {
    if (
      ref !== previousRef ||
      !organizationRef ||
      hasSnapshot ||
      (!hadSnapshot && organizationRef === previousOrganizationRef) ||
      platform.runLoading[runRef.value] ||
      platform.runProblems[runRef.value]
    )
      return;
    // Отсутствие RUN в общем rejoin не доказывает отказ exact route.
    // Старый граф не заменяет новое авторитетное чтение и его проверку доступа.
    refreshScheduler.cancel();
    lastRefreshKey = undefined;
    if (openedStreamRef.value) {
      realtime.closeRun(openedStreamRef.value);
      openedStreamRef.value = undefined;
    }
    const generation = mutationGeneration;
    const ownerScope = ownerRequestSignal();
    void load(ref).then(() => {
      if (
        !ownerScope.aborted &&
        mutationCurrent(generation, ref) &&
        hasAuthoritativeSnapshot.value
      )
        openCurrentStream();
    });
  },
);
watch(refreshKey, (next) => {
  if (!next || next === lastRefreshKey) return;
  lastRefreshKey = next;
  void refreshScheduler.request(runRef.value);
});
watch(
  () =>
    `${run.value?.ref ?? ""}:${[...artifactRefs.value].join(",")}:${
      [...artifactRefs.value].some((ref) => !platform.artifacts[ref])
        ? "missing"
        : "ready"
    }`,
  () => {
    if (!run.value || !graph.value) return;
    // Rejoin и новые ссылки графа читают точные артефакты, без общего каталога.
    void hydrateSnapshotArtifacts(run.value, graph.value, true).catch(() => {
      // Ошибка показана локально; следующая авторитетная ревизия может повторить чтение.
    });
  },
);
watch(
  () => openGateList.value.length,
  (count) => {
    if (count === 0) gateDialogOpen.value = false;
  },
);
watch(runRef, async (next, previous) => {
  artifactController?.abort();
  artifactProblem.value = undefined;
  mutationGeneration++;
  busy.value = false;
  problem.value = undefined;
  refreshScheduler.cancel();
  if (openedStreamRef.value) realtime.closeRun(openedStreamRef.value);
  else realtime.closeRun(previous);
  lastRefreshKey = undefined;
  activityOpen.value = false;
  activityNodeRef.value = undefined;
  selectedRef.value = undefined;
  nodeInspectorOpen.value = false;
  nodeDetailsOpen.value = false;
  gateDialogOpen.value = false;
  turn.value = "";
  turnAttachmentComposer.value?.clear();
  gateAttachmentStates.value = {};
  comments.value = {};
  await load(next);
  if (runRef.value === next) openCurrentStream();
});
onMounted(async () => {
  const initialRef = runRef.value;
  await load(initialRef);
  if (runRef.value === initialRef) openCurrentStream();
});
onBeforeUnmount(() => {
  artifactController?.abort();
  mutationGeneration++;
  refreshScheduler.dispose();
  if (openedStreamRef.value) realtime.closeRun(openedStreamRef.value);
});
</script>
<template>
  <PageFrame
    class="run-page"
    :title="run?.title ?? $t('runs.title')"
    :subtitle="runSubtitle"
    ><template #actions
      ><div v-if="run" class="run-statuses">
        <span>
          <small>{{ $t("common.status") }}</small>
          <StatusBadge v-if="lifecycleState" :state="lifecycleState" />
        </span>
        <span v-if="resultOutcomeState">
          <small>{{ $t("common.result") }}</small>
          <StatusBadge :state="resultOutcomeState" />
        </span>
      </div>
      <button
        v-if="run?.nextActions.includes('CANCEL')"
        class="button button--danger"
        type="button"
        :disabled="busy"
        @click="command('CANCEL')"
      >
        {{ $t("runs.cancel") }}</button
      ><button
        v-if="
          run?.state === 'FAILED' &&
          run.target.type !== 'SYSTEM_ASSISTANT' &&
          failedDiagnosticNodes.length > 0
        "
        class="button"
        type="button"
        :disabled="busy"
        @click="delegateDiagnostics"
      >
        <Bot :size="16" aria-hidden="true" />
        {{ $t("runs.delegateDiagnostics") }}</button
      ><button
        v-if="run?.nextActions.includes('RETRY')"
        class="button button--primary"
        type="button"
        :disabled="busy"
        @click="command('RETRY')"
      >
        {{ $t("runs.retry") }}
      </button></template
    ><AsyncState
      :loading="platform.runLoading[runRef] && !hasAuthoritativeSnapshot"
      :problem="fatalLoadProblem"
      @retry="load"
    >
      <div v-if="run && sessionGraph" class="run-page-body">
        <div class="run-notices" aria-live="polite">
          <div
            v-if="streamState?.problemTitle"
            class="offline-banner"
            role="status"
          >
            {{ streamState.problemTitle }}
          </div>
          <ProblemNotice
            v-if="refreshProblem"
            :problem="refreshProblem"
            compact
          />
          <ProblemNotice v-if="problem" :problem="problem" compact />
        </div>
        <div
          class="run-workspace"
          :class="{ 'run-workspace--activity': activityOpen }"
        >
          <nav
            class="run-workspace-toolbar"
            role="toolbar"
            :aria-label="$t('runs.workspaceTools')"
          >
            <button
              class="icon-button"
              type="button"
              :disabled="!selectedNode"
              :aria-label="$t('runs.context')"
              :title="$t('runs.context')"
              @click="nodeInspectorOpen = true"
            >
              <PanelRightOpen :size="18" aria-hidden="true" />
            </button>
            <button
              ref="activityTrigger"
              class="icon-button"
              type="button"
              :aria-label="$t('runs.activity')"
              :title="$t('runs.activity')"
              @click="openActivity()"
            >
              <Activity :size="18" aria-hidden="true" />
            </button>
            <button
              v-if="openGateList.length"
              class="icon-button run-toolbar-action--attention"
              type="button"
              :aria-label="$t('decisions.title')"
              :title="$t('decisions.title')"
              @click="gateDialogOpen = true"
            >
              <ListChecks :size="18" aria-hidden="true" />
              <span class="run-toolbar-count">{{ openGateList.length }}</span>
            </button>
          </nav>

          <aside class="run-canvas-summary">
            <div class="run-canvas-summary__heading">
              <strong :title="run.target.displayName">{{
                run.target.displayName
              }}</strong>
              <StatusBadge :state="run.state" />
              <button
                type="button"
                class="run-canvas-summary__toggle icon-button"
                :aria-expanded="summaryExpanded"
                aria-controls="run-canvas-summary-details"
                :aria-label="$t('common.details')"
                :title="$t('common.details')"
                @click="summaryExpanded = !summaryExpanded"
              >
                <ChevronDown :size="16" aria-hidden="true" />
              </button>
            </div>
            <div
              id="run-canvas-summary-details"
              class="run-canvas-summary__details"
              :class="{
                'run-canvas-summary__details--expanded': summaryExpanded,
              }"
            >
              <span>{{ $t(`runs.source.${run.source}`) }}</span>
              <span>{{ $t("runs.attempt", { attempt: run.attempt }) }}</span>
              <p
                v-if="run.safeErrorCode"
                class="run-canvas-summary__error"
                role="status"
              >
                {{
                  serverMessage(
                    run.state === "FAILED"
                      ? (runListSummary(run) ?? "")
                      : run.safeErrorMessage || run.safeErrorCode,
                  )
                }}
                <code>{{ run.safeErrorCode }}</code>
              </p>
              <RouterLink
                v-if="run.retryOfRunRef"
                :to="
                  runPath(run.retryOfRunRef, routeProjectRef ?? run.projectRef)
                "
              >
                {{ $t("runs.previousAttempt") }}
              </RouterLink>
              <span
                class="live-indicator"
                :class="`live-indicator--${streamState?.state ?? 'connecting'}`"
              >
                ●
                {{
                  $t(isTerminalRun(run) ? "runs.historyComplete" : "runs.live")
                }}
                <template v-if="sessionGraph.sequence > 0">
                  · #{{ sessionGraph.sequence }}</template
                >
              </span>
              <RunTokenUsage :usage="run.usage" compact />
            </div>
          </aside>

          <section id="run-graph-panel" class="graph-panel">
            <div class="graph-panel__canvas">
              <RunGraphCanvas
                :key="activityOpen ? 'with-activity' : 'full-width'"
                :compact="activityOpen"
                :nodes="sessionGraph.nodes"
                :edges="sessionGraph.edges"
                :selected-ref="selectedNode?.ref"
                :future-node-refs="futureNodeRefs"
                :active-node-refs="activeNodeRefs"
                :execution-labels="nodeExecutionLabels"
                @select="select"
                @details="openNodeDetails"
              />
            </div>
          </section>
        </div>
        <ModalDialog
          v-if="gateDialogOpen && openGateList.length"
          :title="$t('decisions.title')"
          size="lg"
          :busy="busy"
          @close="gateDialogOpen = false"
        >
          <div class="gate-dialog">
            <article v-for="gate in openGateList" :key="gate.ref">
              <div class="gate-question">
                <p class="eyebrow">{{ $t("decisions.question") }}</p>
                <h2>{{ gateDisplayTitle(gate) }}</h2>
                <dl>
                  <div v-if="gate.scopeKind === 'ORGANIZATION'">
                    <dt>{{ $t("decisions.scope") }}</dt>
                    <dd>{{ $t("decisions.organizationScope") }}</dd>
                  </div>
                  <div>
                    <dt>{{ $t("decisions.requestedBy") }}</dt>
                    <dd>{{ gate.requestedBy.displayName }}</dd>
                  </div>
                  <div>
                    <dt>{{ $t("decisions.process") }}</dt>
                    <dd>
                      <button
                        class="button button--ghost"
                        type="button"
                        @click="inspectGateNode(gate)"
                      >
                        {{ gateNodeName(gate) }}
                      </button>
                    </dd>
                  </div>
                </dl>
                <h3>{{ $t("decisions.fullQuestion") }}</h3>
                <p v-if="gate.integrationIntent">
                  {{ gateDisplayQuestion(gate) }}
                </p>
                <SafeMarkdown v-else :content="gate.contextSummary" />
                <template v-if="gate.integrationIntent">
                  <dl class="gate-integration-details">
                    <div>
                      <dt>{{ $t("decisions.integrationOperation") }}</dt>
                      <dd>
                        <code>{{ gate.integrationIntent.operation }}</code>
                      </dd>
                    </div>
                    <div>
                      <dt>{{ $t("decisions.integrationCapability") }}</dt>
                      <dd>
                        <code>{{ gate.integrationIntent.capabilityKey }}</code>
                      </dd>
                    </div>
                  </dl>
                  <div
                    v-if="gateScopeFields(gate).length"
                    class="gate-approval-scope"
                  >
                    <h3>{{ $t("decisions.approvalScopeTitle") }}</h3>
                    <p>{{ $t("decisions.approvalScopeExplanation") }}</p>
                    <dl>
                      <div
                        v-for="field in gateScopeFields(gate)"
                        :key="field.path"
                      >
                        <dt>
                          <code>{{ gateScopePath(field.path) }}</code>
                        </dt>
                        <dd>
                          <code>{{ gateScopeValue(field.value) }}</code>
                        </dd>
                      </div>
                    </dl>
                  </div>
                </template>
                <h3>{{ $t("decisions.consequences") }}</h3>
                <SafeMarkdown :content="gate.consequencesSummary" />
              </div>
              <div class="gate-response">
                <label class="field"
                  ><span>{{ $t("decisions.comment") }}</span
                  ><VoiceTextarea
                    v-model="comments[gate.ref]"
                    :disabled="busy"
                    maxlength="1000"
                  />
                </label>
                <AttachmentComposer
                  :ref="
                    (component) =>
                      setGateAttachmentComposer(gate.ref, component)
                  "
                  compact
                  purpose="OWNER_GATE_MESSAGE"
                  :project-ref="run?.projectRef"
                  :disabled="busy"
                  @change="gateAttachmentStates[gate.ref] = $event"
                />
                <div class="gate-actions">
                  <button
                    v-if="
                      gate.nextActions.includes('RESOLVE_GATE') &&
                      gate.allowedDecisions.includes('APPROVE')
                    "
                    class="button button--primary"
                    type="button"
                    :disabled="busy || !gateAttachmentsReady(gate.ref)"
                    @click="decide(gate, 'APPROVE')"
                  >
                    {{ $t("common.approve") }}</button
                  ><button
                    v-if="
                      gate.nextActions.includes('RESOLVE_GATE') &&
                      gate.allowedDecisions.includes('REQUEST_CHANGES')
                    "
                    class="button"
                    type="button"
                    :disabled="busy || !gateAttachmentsReady(gate.ref)"
                    @click="decide(gate, 'REQUEST_CHANGES')"
                  >
                    {{ $t("common.requestChanges") }}</button
                  ><button
                    v-if="
                      gate.nextActions.includes('RESOLVE_GATE') &&
                      gate.allowedDecisions.includes('REJECT')
                    "
                    class="button button--danger"
                    type="button"
                    :disabled="busy || !gateAttachmentsReady(gate.ref)"
                    @click="decide(gate, 'REJECT')"
                  >
                    {{ $t("common.reject") }}
                  </button>
                </div>
              </div>
            </article>
          </div>
        </ModalDialog>
        <ModalDialog
          v-if="selectedNode && nodeInspectorOpen"
          :title="$t('runs.context')"
          size="xl"
          @close="nodeInspectorOpen = false"
        >
          <RunNodeInspector
            :node="selectedNode"
            :nodes="allRunNodes"
            :artifacts="artifactList"
            :project-ref="routeProjectRef ?? run.projectRef"
            :run="selectedRun"
            :agent="selectedAgent"
            :execution-label="nodeExecutionLabels[selectedNode.ref]"
            @close="nodeInspectorOpen = false"
            @activity="openNodeActivity"
            @details="openSelectedDetails"
          />
        </ModalDialog>
        <aside
          v-if="activityOpen"
          ref="activityDrawer"
          class="run-activity-overlay"
          tabindex="-1"
          :aria-label="$t('runs.activity')"
          @keydown.esc.stop="closeActivity"
        >
          <RunActivityDrawer
            :open="true"
            :run="run"
            :nodes="allRunNodes"
            :events="eventList"
            :artifacts="artifactList"
            :initiator-summary="runInputSummary"
            :initial-node-ref="activityNodeRef"
            @close="closeActivity"
            @download="downloadArtifact"
          >
            <template v-if="run.nextActions.includes('ADD_TURN')" #composer>
              <form class="run-continuation" @submit.prevent="continueRun">
                <label class="field">
                  <span>{{ $t("runs.continueTask") }}</span>
                  <VoiceTextarea
                    v-model="turn"
                    :disabled="busy"
                    maxlength="8000"
                  />
                </label>
                <AttachmentComposer
                  ref="turnAttachmentComposer"
                  compact
                  purpose="SESSION_TURN"
                  :project-ref="run.projectRef"
                  :disabled="busy"
                  @change="turnAttachmentState = $event"
                />
                <button
                  class="button button--primary"
                  type="submit"
                  :disabled="busy || !turn.trim() || !turnAttachmentState.ready"
                >
                  {{ $t("common.send") }}
                </button>
              </form>
            </template>
          </RunActivityDrawer>
        </aside>
        <RunSessionDetailsDialog
          v-if="selectedNode && nodeDetailsOpen"
          :run="selectedRun ?? run"
          :root-run="run"
          :node="selectedNode"
          :nodes="allRunNodes"
          :events="eventList"
          :artifacts="artifactList"
          :agent="selectedAgent"
          :execution-label="nodeExecutionLabels[selectedNode.ref]"
          @close="nodeDetailsOpen = false"
          @download="downloadArtifact"
        />
      </div>
      <section v-else class="run-empty-state">
        <p>{{ $t("common.empty") }}</p>
      </section></AsyncState
    ></PageFrame
  >
</template>
<style scoped>
.run-statuses,
.run-statuses > span {
  display: flex;
  align-items: center;
  gap: 8px;
}
.run-statuses {
  flex-wrap: wrap;
}
.run-statuses small {
  color: var(--subtle);
}
.run-page.run-page {
  display: flex;
  min-width: 0;
  min-height: 0;
  max-width: 100%;
  flex: 1 1 auto;
  flex-direction: column;
  overflow: hidden;
}
.run-page :deep(.page-header) {
  flex: 0 0 auto;
}
.run-page-body {
  position: relative;
  display: flex;
  min-width: 0;
  min-height: 0;
  flex: 1 1 auto;
  margin: 0 calc(var(--page-frame-gutter) * -1);
  overflow: hidden;
}
.run-notices {
  position: absolute;
  z-index: 26;
  top: 68px;
  left: 50%;
  display: grid;
  width: min(680px, calc(100% - 32px));
  gap: 8px;
  pointer-events: none;
  transform: translateX(-50%);
}
.run-notices > * {
  pointer-events: auto;
}
.run-empty-state {
  display: grid;
  min-height: 420px;
  place-items: center;
  color: var(--muted);
}
.live-indicator {
  margin-left: auto;
  color: var(--warning);
}
.live-indicator--live {
  color: var(--success);
}
.gate-dialog {
  display: grid;
  gap: 16px;
}
.gate-dialog article {
  display: grid;
  grid-template-columns: minmax(0, 1.25fr) minmax(280px, 0.75fr);
  align-items: start;
  gap: 16px;
  padding: 16px;
  border: 1px solid #ead8ac;
  border-radius: 10px;
  background: var(--warning-soft);
}
.gate-dialog h2,
.gate-dialog p {
  margin-bottom: 4px;
}
.gate-question {
  min-width: 0;
}
.gate-question h3 {
  margin: 14px 0 5px;
  font-size: 0.82rem;
}
.gate-question :deep(p) {
  margin: 0;
}
.gate-question dl {
  display: flex;
  flex-wrap: wrap;
  gap: 8px 20px;
  margin: 10px 0 0;
}
.gate-question dl > div {
  display: grid;
  gap: 3px;
}
.gate-question dt {
  color: var(--subtle);
  font-size: 0.72rem;
}
.gate-question dd {
  margin: 0;
}
.gate-question dd .button {
  min-height: 0;
  padding: 0;
}
.gate-integration-details {
  display: grid !important;
  grid-template-columns: repeat(2, minmax(0, 1fr));
}
.gate-integration-details code,
.gate-approval-scope code {
  overflow-wrap: anywhere;
  white-space: normal;
}
.gate-approval-scope {
  margin-top: 14px;
  padding: 12px 14px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--surface-muted, #f5f7fa);
}
.gate-approval-scope h3 {
  margin-top: 0;
}
.gate-approval-scope > p {
  color: var(--subtle);
  font-size: 0.76rem;
}
.gate-approval-scope dl {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 8px 14px;
}
.gate-approval-scope dd {
  padding-top: 4px;
}
.gate-response {
  display: grid;
  gap: 10px;
}
.gate-response :deep(textarea) {
  min-height: 92px;
}
.gate-actions {
  display: flex;
  gap: 7px;
  flex-wrap: wrap;
}
.run-workspace {
  position: relative;
  width: 100%;
  min-width: 0;
  min-height: 0;
  flex: 1 1 auto;
  border-block: 1px solid var(--border);
  background: var(--surface);
  overflow: hidden;
}
.run-workspace-toolbar {
  position: absolute;
  z-index: 20;
  top: 14px;
  left: 50%;
  display: flex;
  align-items: center;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: color-mix(in srgb, var(--surface) 94%, transparent);
  box-shadow: 0 8px 24px rgba(16, 22, 30, 0.1);
  backdrop-filter: blur(8px);
  transform: translateX(-50%);
}
.run-workspace-toolbar .icon-button {
  position: relative;
  border: 0;
  border-right: 1px solid var(--border);
  border-radius: 0;
}
.run-workspace-toolbar .icon-button:first-child {
  border-radius: 7px 0 0 7px;
}
.run-workspace-toolbar .icon-button:last-child {
  border-right: 0;
  border-radius: 0 7px 7px 0;
}
.run-toolbar-count {
  position: absolute;
  top: 2px;
  right: 2px;
  display: grid;
  min-width: 15px;
  height: 15px;
  padding: 0 3px;
  border-radius: 999px;
  background: var(--warning);
  color: var(--surface);
  font-family: var(--font-mono);
  font-size: 0.62rem;
  place-items: center;
}
.run-toolbar-action--attention {
  animation: attention-outline 1.6s ease-in-out infinite;
}
@keyframes attention-outline {
  0%,
  100% {
    box-shadow:
      inset 0 0 0 1px transparent,
      0 0 0 0 transparent;
  }
  50% {
    box-shadow:
      inset 0 0 0 1px var(--warning),
      0 0 0 4px color-mix(in srgb, var(--warning) 22%, transparent);
  }
}
.run-canvas-summary {
  position: absolute;
  z-index: 14;
  top: 14px;
  left: 14px;
  display: grid;
  width: min(360px, calc(100% - 190px));
  gap: 6px 10px;
  padding: 11px 12px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: color-mix(in srgb, var(--surface) 94%, transparent);
  box-shadow: 0 8px 24px rgba(16, 22, 30, 0.1);
  backdrop-filter: blur(8px);
}
.run-canvas-summary__heading,
.run-canvas-summary__details {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  align-items: start;
  gap: 6px 10px;
  min-width: 0;
}
.run-canvas-summary__toggle {
  display: none;
}
.run-canvas-summary strong,
.run-canvas-summary span,
.run-canvas-summary a {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
}
.run-canvas-summary__details > span,
.run-canvas-summary a {
  color: var(--muted);
  font-size: 0.75rem;
}
.run-canvas-summary .live-indicator {
  grid-column: 1 / -1;
  margin-left: 0;
  white-space: normal;
}
.run-canvas-summary__error {
  grid-column: 1 / -1;
  display: grid;
  gap: 2px;
  margin: 0;
  color: var(--danger);
  font-size: 0.75rem;
  overflow-wrap: anywhere;
}
.run-canvas-summary__error code {
  color: var(--subtle);
  font-size: 0.7rem;
}
.graph-panel {
  min-width: 0;
  min-height: 0;
}
.graph-panel {
  position: absolute;
  inset: 0;
  background: var(--canvas);
}
.run-workspace--activity .graph-panel {
  right: min(720px, 54%);
}
.run-workspace--activity .run-workspace-toolbar {
  left: calc((100% - min(720px, 54%)) / 2);
}
.graph-panel__canvas {
  width: 100%;
  height: 100%;
  min-height: 0;
}
.run-activity-overlay {
  position: absolute;
  z-index: 24;
  inset-block: 0;
  right: 0;
  display: flex;
  width: min(720px, 54%);
  min-width: 520px;
  min-height: 0;
  border: 0;
  border-left: 1px solid var(--border);
  outline: 0;
  background: var(--surface);
  box-shadow: -14px 0 36px rgba(16, 22, 30, 0.14);
}
.run-continuation {
  display: grid;
  gap: 9px;
}
.run-continuation > .button {
  justify-self: end;
}
.run-page-body :deep(.modal--full .modal__body) {
  display: flex;
  overflow: hidden;
}
.run-page-body :deep(.modal--full .modal__footer:empty),
.run-page-body :deep(.modal--xl .modal__footer:empty),
.run-page-body :deep(.modal--lg .modal__footer:empty) {
  display: none;
}
@media (prefers-reduced-motion: reduce) {
  .run-toolbar-action--attention {
    animation: none;
    box-shadow: inset 0 0 0 1px var(--warning);
  }
}
@media (min-width: 761px) {
  .run-workspace--activity .run-canvas-summary {
    top: 70px;
    width: min(360px, calc((100% - min(720px, 54%)) / 2 - 86px));
  }
  .run-workspace--activity .run-workspace-toolbar {
    top: 70px;
  }
}
@media (max-width: 760px) {
  .run-page-body {
    min-height: 0;
  }
  .run-notices {
    top: 116px;
    width: calc(100% - 16px);
  }
  .run-summary .live-indicator {
    margin-left: 0;
  }
  .gate-dialog article {
    grid-template-columns: 1fr;
  }
  .run-canvas-summary {
    top: 62px;
    left: 8px;
    width: min(320px, calc(100% - 16px));
    padding: 6px 8px;
  }
  .run-canvas-summary__heading {
    grid-template-columns: minmax(0, 1fr) auto 32px;
    align-items: center;
    gap: 6px;
  }
  .run-canvas-summary__heading strong {
    display: -webkit-box;
    -webkit-box-orient: vertical;
    -webkit-line-clamp: 2;
    font-size: 0.8rem;
    line-height: 1.3;
  }
  .run-canvas-summary__toggle {
    display: inline-flex;
    width: 32px;
    height: 32px;
    min-height: 32px;
    padding: 0;
  }
  .run-canvas-summary__toggle[aria-expanded="true"] svg {
    transform: rotate(180deg);
  }
  .run-canvas-summary__details {
    display: none;
  }
  .run-canvas-summary__details--expanded {
    display: grid;
    max-height: min(260px, 40dvh);
    overflow: auto;
  }
  .run-workspace-toolbar {
    top: 8px;
    left: 8px;
    transform: none;
  }
  .run-activity-overlay {
    left: 0;
    width: 100%;
    min-width: 0;
  }
  .run-workspace--activity .graph-panel {
    right: 0;
  }
}
</style>
