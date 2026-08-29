<script setup lang="ts">
import { Activity } from "@lucide/vue";
import {
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
import type { PresentedRunEvent } from "@/features/runs/run-activity";
import type {
  Artifact,
  OwnerGate,
  RunEvent,
  RunNode,
} from "@/shared/api/generated/openapi/types.gen";
import { AppProblem, asProblem } from "@/shared/api/problem";
import { runPath } from "@/shared/routes";
import AsyncState from "@/shared/ui/AsyncState.vue";
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
const selectedNode = computed(
  () =>
    presentedGraph.value?.nodes.find((n) => n.ref === selectedRef.value) ??
    presentedGraph.value?.nodes.find((n) => n.state === "RUNNING") ??
    presentedGraph.value?.nodes[0],
);
const selectedAgent = computed(() =>
  selectedNode.value?.agentRef
    ? platform.agents[selectedNode.value.agentRef]
    : undefined,
);
const openGateNodeRefs = computed(
  () =>
    new Set(
      gateList.value
        .filter((gate) => gate.state === "OPEN")
        .map((gate) => gate.nodeRef),
    ),
);
const futureNodeRefs = computed(() =>
  (presentedGraph.value?.nodes ?? [])
    .filter(
      (node) =>
        (node.state === "QUEUED" || node.state === "WAITING") &&
        !node.startedAt &&
        !openGateNodeRefs.value.has(node.ref),
    )
    .map((node) => node.ref),
);
const activeNodeRefs = computed(() =>
  (presentedGraph.value?.nodes ?? [])
    .filter(
      (node) =>
        node.state === "RUNNING" || openGateNodeRefs.value.has(node.ref),
    )
    .map((node) => node.ref),
);
const lifecycleState = computed(() => run.value?.state);
const usageItems = computed(() => {
  const usage = run.value?.usage;
  if (!usage || (usage.totalTokens === 0 && usage.modelContextWindow === 0))
    return [];
  return [
    ["total", usage.totalTokens],
    ["input", usage.inputTokens],
    ["cached", usage.cachedInputTokens],
    ["output", usage.outputTokens],
    ["reasoning", usage.reasoningOutputTokens],
    ["contextWindow", usage.modelContextWindow],
  ] as const;
});
function formatTokenCount(value: number): string {
  return new Intl.NumberFormat(translator.locale.value).format(value);
}

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
const comment = ref("");
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
  if (!run.value?.nextActions.includes("ADD_TURN") || !turn.value.trim())
    return;
  busy.value = true;
  problem.value = undefined;
  try {
    const next = await platform.continueSession(run.value.sessionRef, {
      runRef: run.value.ref,
      nodeRef: selectedNode.value?.ref,
      task: turn.value.trim(),
    });
    turn.value = "";
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
    await platform.decide(gate, {
      decision,
      comment: comment.value.trim() || undefined,
    });
    comment.value = "";
  } catch (error) {
    problem.value = asProblem(error);
    await platform.loadGates(run.value?.projectRef, run.value?.rootRunRef);
  } finally {
    busy.value = false;
  }
}
function select(node: RunNode) {
  selectedRef.value = node.ref;
  nodeInspectorOpen.value = true;
  mobilePane.value = "graph";
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
  <PageFrame :title="run?.title ?? $t('runs.title')" :subtitle="runSubtitle"
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
      <div
        v-if="streamState?.problemTitle"
        class="offline-banner"
        role="status"
      >
        {{ streamState.problemTitle }}
      </div>
      <ProblemNotice v-if="refreshProblem" :problem="refreshProblem" compact />
      <dl
        v-if="usageItems.length"
        class="token-usage"
        :aria-label="$t('runs.usage.title')"
      >
        <div v-for="item in usageItems" :key="item[0]">
          <dt>{{ $t(`runs.usage.${item[0]}`) }}</dt>
          <dd>{{ formatTokenCount(item[1]) }}</dd>
        </div>
      </dl>
      <ProblemNotice v-if="problem" :problem="problem" compact />
      <section
        v-if="gateList.some((g) => g.state === 'OPEN')"
        class="gate-strip"
      >
        <article
          v-for="gate in gateList.filter((g) => g.state === 'OPEN')"
          :key="gate.ref"
        >
          <div>
            <h2>{{ gate.title }}</h2>
            <p>{{ gate.contextSummary }}</p>
            <strong>{{ gate.consequencesSummary }}</strong>
          </div>
          <label class="field"
            ><span>{{ $t("decisions.comment") }}</span
            ><input v-model="comment" maxlength="1000"
          /></label>
          <div class="gate-actions">
            <button
              v-if="
                gate.nextActions.includes('RESOLVE_GATE') &&
                gate.allowedDecisions.includes('APPROVE')
              "
              class="button button--primary"
              type="button"
              :disabled="busy"
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
              :disabled="busy"
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
              :disabled="busy"
              @click="decide(gate, 'REJECT')"
            >
              {{ $t("common.reject") }}
            </button>
          </div>
        </article>
      </section>
      <div
        v-if="run && presentedGraph"
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
        v-if="run && presentedGraph"
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
            :to="runPath(run.retryOfRunRef, routeProjectRef ?? run.projectRef)"
          >
            {{ $t("runs.previousAttempt") }}
          </RouterLink>
          <span
            class="live-indicator"
            :class="`live-indicator--${streamState?.state ?? 'connecting'}`"
          >
            ● {{ $t("runs.live") }} · #{{ presentedGraph.sequence }}
          </span>
        </aside>

        <section id="run-graph-panel" class="graph-panel">
          <div class="graph-panel__canvas">
            <RunGraphCanvas
              :nodes="presentedGraph.nodes"
              :edges="presentedGraph.edges"
              :selected-ref="selectedNode?.ref"
              :future-node-refs="futureNodeRefs"
              :active-node-refs="activeNodeRefs"
              @select="select"
            />
          </div>
        </section>

        <aside v-if="selectedNode && nodeInspectorOpen" class="node-panel">
          <RunNodeInspector
            :node="selectedNode"
            :nodes="presentedGraph.nodes"
            :artifacts="artifactList"
            :project-ref="routeProjectRef ?? run.projectRef"
            @close="nodeInspectorOpen = false"
            @activity="openActivity"
            @details="nodeDetailsOpen = true"
          />
        </aside>

        <RunActivityDrawer
          :open="activityOpen"
          :run="run"
          :nodes="presentedGraph.nodes"
          :events="eventList"
          :initiator-summary="runInputSummary"
          :initial-node-ref="activityNodeRef"
          @close="closeActivity"
        />
      </div>
      <RunSessionDetailsDialog
        v-if="run && presentedGraph && selectedNode && nodeDetailsOpen"
        :run="run"
        :node="selectedNode"
        :nodes="presentedGraph.nodes"
        :events="eventList"
        :agent="selectedAgent"
        @close="nodeDetailsOpen = false"
      />
      <section v-if="run" class="run-bottom">
        <article v-if="run.resultSummary" class="panel run-result">
          <div class="workspace-heading">
            <h2>{{ $t("common.result") }}</h2>
            <StatusBadge
              v-if="resultOutcomeState"
              :state="resultOutcomeState"
            />
          </div>
          <SafeMarkdown :content="run.resultSummary" />
        </article>
        <article class="panel run-incidents" aria-live="polite">
          <h2>{{ $t("runs.incidents") }}</h2>
          <div v-if="incidentList.length" class="incident-list">
            <article
              v-for="incident in incidentList"
              :key="incident.ref"
              class="incident-row"
            >
              <div>
                <strong>{{ incident.safeSummary }}</strong>
                <p>{{ incident.safeNextStep }}</p>
                <small v-if="!incident.coreAffected">
                  {{ $t("runs.coreUnaffected") }}
                </small>
              </div>
              <StatusBadge :state="incident.severity" />
            </article>
          </div>
          <p v-else>{{ $t("runs.noIncidents") }}</p>
        </article>
        <article class="panel">
          <h2>{{ $t("runs.artifacts") }}</h2>
          <div v-if="artifactList.length" class="artifact-list">
            <button
              v-for="artifact in artifactList"
              :key="artifact.ref"
              class="artifact-row"
              type="button"
              :disabled="
                downloadBusyRef === artifact.ref ||
                !artifact.nextActions.includes('DOWNLOAD')
              "
              @click="downloadArtifact(artifact)"
            >
              <span>{{ artifact.fileName }}</span
              ><StatusBadge :state="artifact.scanState" /><span
                >{{ artifact.sizeBytes }} B</span
              >
            </button>
          </div>
          <p v-else>{{ $t("common.empty") }}</p>
        </article>
        <article v-if="run.nextActions.includes('ADD_TURN')" class="panel">
          <h2>{{ $t("common.continue") }}</h2>
          <label class="field"
            ><span>{{ $t("runs.continueTask") }}</span
            ><textarea v-model="turn" maxlength="8000" /></label
          ><button
            class="button button--primary"
            type="button"
            :disabled="busy || !turn.trim()"
            @click="continueRun"
          >
            {{ $t("common.send") }}
          </button>
        </article>
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
.token-usage {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(112px, 1fr));
  gap: 1px;
  margin: 0 0 16px;
  overflow: hidden;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--border);
}
.token-usage > div {
  min-width: 0;
  padding: 9px 11px;
  background: var(--surface);
}
.token-usage dt {
  color: var(--subtle);
  font-size: 0.74rem;
}
.token-usage dd {
  margin: 2px 0 0;
  font-family: var(--font-mono);
  font-size: 0.86rem;
  font-weight: 600;
}
.incident-list {
  display: grid;
  gap: 8px;
}
.incident-row {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
  padding: 10px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--warning-soft);
}
.incident-row p {
  margin: 4px 0;
}
.incident-row small {
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
  margin-bottom: 16px;
}
.gate-strip article {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(220px, 0.5fr) auto;
  align-items: end;
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
.gate-actions {
  display: flex;
  gap: 7px;
  flex-wrap: wrap;
}
.run-workspace {
  position: relative;
  height: max(640px, calc(100dvh - 170px));
  min-height: 640px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--surface);
  overflow: hidden;
}
.run-canvas-summary {
  position: absolute;
  z-index: 14;
  top: 14px;
  right: 14px;
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
  overflow: hidden;
  border: 1px solid var(--border);
  border-radius: 8px;
  box-shadow: 0 16px 44px rgba(16, 22, 30, 0.16);
  overflow: hidden;
}
.workspace-heading {
  display: flex;
  align-items: center;
  justify-content: space-between;
  min-height: 46px;
  gap: 12px;
  padding: 8px 14px;
  border-bottom: 1px solid var(--border);
  background: var(--surface);
}
.workspace-heading h2 {
  margin: 0;
  font-size: 0.95rem;
}
.workspace-heading > span {
  color: var(--muted);
  font-family: var(--font-mono);
  font-size: 0.74rem;
}
.graph-panel__canvas {
  width: 100%;
  height: 100%;
  min-height: 0;
}
.run-mobile-tabs {
  display: none;
}
.run-bottom {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 16px;
  margin-top: 16px;
}
.run-result {
  grid-column: 1 / -1;
}
.artifact-list {
  display: grid;
}
.artifact-row {
  display: grid;
  grid-template-columns: 1fr auto auto;
  width: 100%;
  gap: 12px;
  padding: 10px 0;
  border: 0;
  border-bottom: 1px solid var(--border);
  background: transparent;
  color: inherit;
  text-align: left;
  text-decoration: none;
  cursor: pointer;
}
@media (max-width: 760px) {
  .gate-strip article,
  .run-bottom {
    grid-template-columns: 1fr;
  }
  .run-summary .live-indicator {
    margin-left: 0;
  }
  .run-mobile-tabs {
    display: grid;
    grid-template-columns: 1fr 1fr;
    margin-bottom: 8px;
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
  .run-workspace {
    height: max(600px, calc(100dvh - 180px));
    min-height: 600px;
  }
  .run-canvas-summary {
    top: 66px;
    right: 8px;
    width: min(320px, calc(100% - 16px));
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
  .artifact-row {
    grid-template-columns: 1fr auto;
  }
  .artifact-row span:last-child {
    display: none;
  }
}
</style>
