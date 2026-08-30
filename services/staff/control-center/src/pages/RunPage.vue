<script setup lang="ts">
import { Activity } from "@lucide/vue";
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
import RunActivityDrawer from "@/features/runs/RunActivityDrawer.vue";
import RunGraphCanvas from "@/features/runs/RunGraphCanvas.vue";
import RunNodeInspector from "@/features/runs/RunNodeInspector.vue";
import RunSessionDetailsDialog from "@/features/runs/RunSessionDetailsDialog.vue";
import RunTokenUsage from "@/features/runs/RunTokenUsage.vue";
import type { PresentedRunEvent } from "@/features/runs/run-activity";
import {
  indexRunSessionOwnership,
  projectRunSessionGraph,
  resolveRunSessionSelection,
} from "@/features/runs/run-session-graph";
import type {
  Artifact,
  OwnerGate,
  RunEvent,
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
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import SafeMarkdown from "@/shared/ui/SafeMarkdown.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";
const platform = usePlatformStore();
const realtime = useRealtimeStore();
const route = useRoute();
const router = useRouter();
const translator = useI18n();
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
const opaqueRefPattern =
  /`?(?:agt|art|bld|cap|cnv|con|edg|evt|gat|inc|int|job|mbr|msg|nod|pln|prj|rev|rol|rti|run|sch|ses|trn|usr|wfl)_[A-Za-z0-9_-]{8,}`?/g;
const technicalTokenPattern = /`?\b[A-Z][A-Z\d]*(?:_[A-Z\d]+)+\b`?/g;

function safeRuntimeText(value?: string): string | undefined {
  const source = value?.trim();
  if (!source) return undefined;
  if (source.startsWith("{") || source.startsWith("[")) {
    try {
      JSON.parse(source);
      return undefined;
    } catch {
      // A user-facing sentence may legitimately start with a bracket.
    }
  }
  const visible = source
    .replace(/`?i18n:[A-Z\d_]+`?/g, "")
    .replace(opaqueRefPattern, "")
    .replace(technicalTokenPattern, "")
    .replace(/\s+([,.;:!?])/g, "$1")
    .replace(/([,.;:])\s*([,.;:])/g, "$1")
    .replace(/^\s*[-–—:;,]+\s*|\s*[-–—:;,]+\s*$/g, "")
    .replace(/\s{2,}/g, " ")
    .trim();
  return /[\p{L}\p{N}]/u.test(visible) ? visible : undefined;
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
      displaySummary: safeRuntimeText(event.summary) ?? eventFallback(event),
      displayProgress: safeRuntimeText(event.progress),
    })),
);
const gateList = computed(() =>
  Object.values(platform.gates).filter(
    (g) => g.runRef === runRef.value || g.runRef === run.value?.rootRunRef,
  ),
);
const artifactList = computed(() =>
  Object.values(platform.artifacts).filter(
    (a) => a.runRef === runRef.value || a.runRef === run.value?.rootRunRef,
  ),
);
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
const downloadBusyRef = ref("");
const problem = ref<AppProblem>();
const activityOpen = ref(false);
const activityNodeRef = ref<string>();
const nodeInspectorOpen = ref(true);
const nodeDetailsOpen = ref(false);
const mobilePane = ref<"graph" | "activity">("graph");
const activityTrigger = ref<HTMLButtonElement>();
const hasAuthoritativeSnapshot = computed(() =>
  Boolean(run.value && graph.value),
);
const fatalLoadProblem = computed(() =>
  hasAuthoritativeSnapshot.value ? undefined : platform.problems.run,
);
const refreshProblem = computed(() =>
  hasAuthoritativeSnapshot.value
    ? (platform.problems.run ??
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

async function refreshAuthoritativeState(ref: string): Promise<void> {
  await platform.loadRun(ref);
  if (runRef.value !== ref) return;
  if (platform.problems.run) throw platform.problems.run;
  const snapshot = platform.runs[ref];
  if (!snapshot) return;
  await Promise.all([
    platform.loadGates(snapshot.projectRef, snapshot.rootRunRef),
    platform.loadArtifacts(snapshot.projectRef),
  ]);
  if (runRef.value !== ref) return;
  const relatedProblem = platform.problems.gates ?? platform.problems.artifacts;
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
  if (!run.value?.nextActions.includes(action)) return;
  busy.value = true;
  problem.value = undefined;
  try {
    const next = await platform.changeRun(run.value, { action });
    if (action === "RETRY" && next.ref !== runRef.value)
      await router.replace(
        runPath(next.ref, routeProjectRef.value ?? next.projectRef),
      );
  } catch (error) {
    problem.value = asProblem(error);
  } finally {
    busy.value = false;
  }
}
async function continueRun() {
  if (
    !run.value?.nextActions.includes("ADD_TURN") ||
    !turn.value.trim() ||
    !turnAttachmentState.value.ready
  )
    return;
  busy.value = true;
  problem.value = undefined;
  try {
    const attachmentSetRef = await turnAttachmentComposer.value?.finalize();
    const next = await platform.continueSession(run.value.sessionRef, {
      runRef: run.value.ref,
      nodeRef: selectedNode.value?.ref,
      task: turn.value.trim(),
      ...(attachmentSetRef ? { attachmentSetRef } : {}),
    });
    turn.value = "";
    turnAttachmentComposer.value?.clear();
    if (next.ref !== runRef.value)
      await router.replace(
        runPath(next.ref, routeProjectRef.value ?? next.projectRef),
      );
  } catch (error) {
    problem.value = asProblem(error);
  } finally {
    busy.value = false;
  }
}
async function decide(
  gate: OwnerGate,
  decision: "APPROVE" | "REJECT" | "REQUEST_CHANGES" | "CANCEL",
) {
  if (
    !gate.nextActions.includes("RESOLVE_GATE") ||
    !gate.allowedDecisions.includes(decision)
  )
    return;
  busy.value = true;
  problem.value = undefined;
  try {
    const attachmentSetRef = await gateAttachmentComposers
      .get(gate.ref)
      ?.finalize();
    await platform.decide(gate, {
      decision,
      comment: comments.value[gate.ref]?.trim() || undefined,
      ...(attachmentSetRef ? { attachmentSetRef } : {}),
    });
    Reflect.deleteProperty(comments.value, gate.ref);
    Reflect.deleteProperty(gateAttachmentStates.value, gate.ref);
  } catch (error) {
    problem.value = asProblem(error);
    await platform.loadGates(run.value?.projectRef, run.value?.rootRunRef);
  } finally {
    busy.value = false;
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
function inspectGateNode(gate: OwnerGate): void {
  const sessionRef = sessionOwnership.value.get(gate.nodeRef);
  const node = sessionGraph.value?.nodes.find(
    (candidate) => candidate.ref === sessionRef,
  );
  if (node) select(node);
}
function select(node: RunNode) {
  selectedRef.value = node.ref;
  nodeInspectorOpen.value = true;
  mobilePane.value = "graph";
}
function openNodeDetails(node: RunNode): void {
  select(node);
  nodeDetailsOpen.value = true;
}
function openActivity(nodeRef?: string): void {
  activityNodeRef.value = nodeRef;
  activityOpen.value = true;
  mobilePane.value = "activity";
}
function showGraph(): void {
  activityOpen.value = false;
  mobilePane.value = "graph";
}
function closeActivity(): void {
  showGraph();
  void nextTick(() => activityTrigger.value?.focus());
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
watch(refreshKey, (next) => {
  if (!next || next === lastRefreshKey) return;
  lastRefreshKey = next;
  void refreshScheduler.request(runRef.value);
});
watch(runRef, async (next, previous) => {
  refreshScheduler.cancel();
  if (openedStreamRef.value) realtime.closeRun(openedStreamRef.value);
  else realtime.closeRun(previous);
  lastRefreshKey = undefined;
  activityOpen.value = false;
  activityNodeRef.value = undefined;
  selectedRef.value = undefined;
  nodeInspectorOpen.value = true;
  nodeDetailsOpen.value = false;
  turn.value = "";
  turnAttachmentComposer.value?.clear();
  gateAttachmentStates.value = {};
  comments.value = {};
  mobilePane.value = "graph";
  await load(next);
  if (runRef.value === next) openCurrentStream();
});
onMounted(async () => {
  const initialRef = runRef.value;
  await load(initialRef);
  if (runRef.value === initialRef) openCurrentStream();
});
onBeforeUnmount(() => {
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
        v-if="run"
        ref="activityTrigger"
        class="button"
        type="button"
        @click="openActivity()"
      >
        <Activity :size="17" aria-hidden="true" />
        {{ $t("runs.activity") }}
      </button>
      <button
        v-if="run?.nextActions.includes('CANCEL')"
        class="button button--danger"
        type="button"
        :disabled="busy"
        @click="command('CANCEL')"
      >
        {{ $t("runs.cancel") }}</button
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
      :loading="platform.loading.run && !hasAuthoritativeSnapshot"
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
        <section
          v-if="gateList.some((g) => g.state === 'OPEN')"
          class="gate-strip"
        >
          <article
            v-for="gate in gateList.filter((g) => g.state === 'OPEN')"
            :key="gate.ref"
          >
            <div class="gate-question">
              <p class="eyebrow">{{ $t("decisions.question") }}</p>
              <h2>{{ gate.title }}</h2>
              <dl>
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
              <SafeMarkdown :content="gate.contextSummary" />
              <h3>{{ $t("decisions.consequences") }}</h3>
              <SafeMarkdown :content="gate.consequencesSummary" />
            </div>
            <div class="gate-response">
              <label class="field"
                ><span>{{ $t("decisions.comment") }}</span
                ><textarea v-model="comments[gate.ref]" maxlength="1000" />
              </label>
              <AttachmentComposer
                :ref="
                  (component) => setGateAttachmentComposer(gate.ref, component)
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
        </section>
        <div
          class="run-mobile-tabs"
          role="tablist"
          :aria-label="$t('runs.activity')"
        >
          <button
            id="run-graph-tab"
            class="button"
            type="button"
            role="tab"
            aria-controls="run-graph-panel"
            :aria-selected="mobilePane === 'graph'"
            @click="showGraph"
          >
            {{ $t("runs.graph") }}
          </button>
          <button
            id="run-activity-tab"
            class="button"
            type="button"
            role="tab"
            aria-controls="run-activity-drawer"
            :aria-selected="mobilePane === 'activity'"
            @click="openActivity()"
          >
            {{ $t("runs.activity") }}
            <span>{{ eventList.length }}</span>
          </button>
        </div>
        <div
          class="run-workspace"
          :class="{ 'run-workspace--activity': activityOpen }"
        >
          <aside class="run-canvas-summary">
            <div>
              <strong>{{ run.target.displayName }}</strong>
              <span>{{ $t(`runs.source.${run.source}`) }}</span>
            </div>
            <StatusBadge :state="run.state" />
            <span>{{ $t("runs.attempt", { attempt: run.attempt }) }}</span>
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
              ● {{ $t("runs.live") }} · #{{ sessionGraph.sequence }}
            </span>
            <RunTokenUsage :usage="run.usage" compact />
          </aside>

          <section id="run-graph-panel" class="graph-panel">
            <div class="graph-panel__canvas">
              <RunGraphCanvas
                :nodes="sessionGraph.nodes"
                :edges="sessionGraph.edges"
                :selected-ref="selectedNode?.ref"
                :future-node-refs="futureNodeRefs"
                :active-node-refs="activeNodeRefs"
                @select="select"
                @details="openNodeDetails"
              />
            </div>
          </section>

          <aside v-if="selectedNode && nodeInspectorOpen" class="node-panel">
            <RunNodeInspector
              :node="selectedNode"
              :nodes="allRunNodes"
              :artifacts="artifactList"
              :project-ref="routeProjectRef ?? run.projectRef"
              :run="selectedRun"
              :agent="selectedAgent"
              @close="nodeInspectorOpen = false"
              @activity="openActivity"
              @details="nodeDetailsOpen = true"
            />
          </aside>

          <RunActivityDrawer
            :open="activityOpen"
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
                  <textarea v-model="turn" maxlength="8000" />
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
        </div>
        <RunSessionDetailsDialog
          v-if="selectedNode && nodeDetailsOpen"
          :run="selectedRun ?? run"
          :root-run="run"
          :node="selectedNode"
          :nodes="allRunNodes"
          :events="eventList"
          :artifacts="artifactList"
          :agent="selectedAgent"
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
  max-width: 100%;
  height: calc(100dvh - 148px);
  min-height: 640px;
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
  min-height: 520px;
  flex: 1 1 auto;
  margin: 0 calc(var(--page-frame-gutter) * -1) -36px;
  overflow: hidden;
}
.run-notices {
  position: absolute;
  z-index: 24;
  top: 14px;
  right: 440px;
  left: 126px;
  display: grid;
  gap: 8px;
  pointer-events: none;
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
.gate-strip {
  position: absolute;
  z-index: 22;
  top: 86px;
  bottom: 14px;
  left: 14px;
  width: min(520px, calc(100% - 422px));
  min-width: 300px;
  overflow: auto;
  overscroll-behavior: contain;
  filter: drop-shadow(0 16px 38px rgba(16, 22, 30, 0.16));
}
.gate-strip article {
  display: grid;
  grid-template-columns: 1fr;
  align-items: start;
  gap: 16px;
  padding: 16px;
  border: 1px solid #ead8ac;
  border-radius: 10px;
  background: var(--warning-soft);
}
.gate-strip h2,
.gate-strip p {
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
.gate-response {
  display: grid;
  gap: 10px;
}
.gate-response textarea {
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
.run-canvas-summary {
  position: absolute;
  z-index: 14;
  top: 14px;
  right: 64px;
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  width: min(360px, calc(100% - 190px));
  gap: 6px 10px;
  padding: 11px 12px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: color-mix(in srgb, var(--surface) 94%, transparent);
  box-shadow: 0 8px 24px rgba(16, 22, 30, 0.1);
  backdrop-filter: blur(8px);
}
.run-canvas-summary > div {
  display: grid;
  min-width: 0;
}
.run-canvas-summary strong,
.run-canvas-summary span,
.run-canvas-summary a {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
}
.run-canvas-summary > div span,
.run-canvas-summary > span,
.run-canvas-summary a {
  color: var(--muted);
  font-size: 0.75rem;
}
.run-canvas-summary .live-indicator {
  grid-column: 1 / -1;
  margin-left: 0;
  white-space: normal;
}
.graph-panel,
.node-panel {
  min-width: 0;
  min-height: 0;
}
.graph-panel {
  position: absolute;
  inset: 0;
  background: var(--canvas);
}
.node-panel {
  position: absolute;
  z-index: 16;
  top: 126px;
  right: 14px;
  bottom: 14px;
  width: min(380px, calc(100% - 28px));
  border: 1px solid var(--border);
  border-radius: 8px;
  box-shadow: 0 16px 44px rgba(16, 22, 30, 0.16);
  overflow: hidden;
}
.graph-panel__canvas {
  width: 100%;
  height: 100%;
  min-height: 0;
}
.run-mobile-tabs {
  display: none;
}
.run-continuation {
  display: grid;
  gap: 9px;
}
.run-continuation > .button {
  justify-self: end;
}
@media (min-width: 761px) and (max-width: 1100px) {
  .run-notices {
    top: 126px;
    right: 14px;
    left: 14px;
  }
}
@media (max-width: 760px) {
  .run-page.run-page {
    height: calc(100dvh - 192px);
    min-height: 560px;
  }
  .run-page-body {
    min-height: 480px;
    margin: 0 calc(var(--page-frame-gutter) * -1);
  }
  .run-notices {
    top: 118px;
    right: 8px;
    left: 8px;
  }
  .run-summary .live-indicator {
    margin-left: 0;
  }
  .run-mobile-tabs {
    position: absolute;
    z-index: 25;
    top: 8px;
    left: 50%;
    display: grid;
    width: min(210px, calc(100% - 112px));
    grid-template-columns: 1fr 1fr;
    transform: translateX(-50%);
  }
  .run-mobile-tabs .button {
    min-width: 0;
    border-radius: 0;
  }
  .run-mobile-tabs .button:first-child {
    border-radius: 7px 0 0 7px;
  }
  .run-mobile-tabs .button:last-child {
    border-radius: 0 7px 7px 0;
  }
  .run-mobile-tabs .button[aria-selected="true"] {
    border-color: var(--accent);
    background: var(--accent-soft);
    color: var(--accent-strong);
  }
  .run-mobile-tabs span {
    font-family: var(--font-mono);
    font-size: 0.74rem;
  }
  .gate-strip {
    top: 118px;
    right: 8px;
    bottom: 8px;
    left: 8px;
    width: auto;
    min-width: 0;
  }
  .run-canvas-summary {
    top: 62px;
    right: 56px;
    width: min(320px, calc(100% - 64px));
  }
  .node-panel {
    z-index: 20;
    top: auto;
    right: 0;
    bottom: 0;
    left: 0;
    width: 100%;
    max-height: 56%;
    border-top: 1px solid var(--border);
    border-left: 0;
    border-radius: 8px 8px 0 0;
  }
}
</style>
