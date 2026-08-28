<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute, useRouter } from "vue-router";
import {
  authoritativeRunRefreshKey,
  createRunRefreshScheduler,
} from "@/features/platform/run-refresh";
import { usePlatformStore } from "@/features/platform/store";
import { useRealtimeStore } from "@/features/realtime/store";
import RunGraphCanvas from "@/features/runs/RunGraphCanvas.vue";
import type {
  Artifact,
  OwnerGate,
  RunEvent,
  RunNode,
} from "@/shared/api/generated/openapi/types.gen";
import { AppProblem, asProblem } from "@/shared/api/problem";
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

type PresentedEvent = RunEvent & {
  displaySummary: string;
  displayProgress?: string;
};

const eventList = computed<PresentedEvent[]>(() =>
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
    graph.value?.nodes.find((n) => n.ref === selectedRef.value) ??
    graph.value?.nodes.find((n) => n.state === "RUNNING") ??
    graph.value?.nodes[0],
);
const selectedNodeEvents = computed(() =>
  eventList.value
    .filter((event) => event.nodeRef === selectedNode.value?.ref)
    .slice(-20),
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

const selectedNodeRole = computed(() => {
  const role = safeRuntimeText(selectedNode.value?.role);
  return (
    role ??
    (selectedNode.value
      ? translator.t(`runs.nodeTypes.${selectedNode.value.type}`)
      : undefined)
  );
});
const selectedNodeProgress = computed(() =>
  safeRuntimeText(selectedNode.value?.progressSummary),
);
const turn = ref("");
const comment = ref("");
const busy = ref(false);
const downloadBusyRef = ref("");
const problem = ref<AppProblem>();
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
      await router.replace(`/runs/${next.ref}`);
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
    if (next.ref !== runRef.value) await router.replace(`/runs/${next.ref}`);
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
      ><div v-if="run && graph" class="run-summary">
        <span>{{ run.target.displayName }}</span
        ><span>{{ $t(`runs.source.${run.source}`) }}</span
        ><span>{{ $t("runs.attempt", { attempt: run.attempt }) }}</span
        ><RouterLink
          v-if="run.retryOfRunRef"
          :to="`/runs/${run.retryOfRunRef}`"
          >{{ $t("runs.previousAttempt") }}</RouterLink
        >
        <span>{{ new Date(run.createdAt).toLocaleString() }}</span
        ><span
          class="live-indicator"
          :class="`live-indicator--${realtime.state[graph?.runRef ?? runRef]?.state ?? 'connecting'}`"
          >● {{ $t("runs.live") }}</span
        >
      </div>
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
      <div v-if="run && presentedGraph" class="run-workspace">
        <section class="graph-panel">
          <div class="workspace-heading">
            <h2>{{ $t("runs.graph") }}</h2>
            <span>{{ presentedGraph.nodes.length }}</span>
          </div>
          <RunGraphCanvas
            :nodes="presentedGraph.nodes"
            :edges="presentedGraph.edges"
            :selected-ref="selectedNode?.ref"
            @select="select"
          />
        </section>
        <section class="timeline-panel">
          <div class="workspace-heading">
            <h2>{{ $t("runs.activity") }}</h2>
          </div>
          <ol class="timeline">
            <li v-for="event in eventList" :key="event.sequence">
              <span class="timeline__marker" aria-hidden="true" />
              <div>
                <SafeMarkdown :content="event.displaySummary" />
                <SafeMarkdown
                  v-if="event.displayProgress"
                  :content="event.displayProgress"
                />
                <small>{{
                  new Date(event.occurredAt).toLocaleTimeString()
                }}</small>
              </div>
            </li>
          </ol>
          <p v-if="!eventList.length" class="empty-compact">
            {{ $t("runs.noEvents") }}
          </p>
        </section>
        <aside class="node-panel">
          <div class="workspace-heading">
            <h2>{{ $t("runs.context") }}</h2>
          </div>
          <template v-if="selectedNode"
            ><StatusBadge :state="selectedNode.state" />
            <h3>{{ selectedNode.displayName }}</h3>
            <SafeMarkdown
              v-if="selectedNode.inputSummary"
              :content="selectedNode.inputSummary"
            />
            <SafeMarkdown
              v-if="selectedNodeProgress"
              :content="selectedNodeProgress"
            />
            <ProblemNotice
              v-if="selectedNode.safeErrorCode"
              :problem="
                asProblem({
                  status: 500,
                  code: selectedNode.safeErrorCode,
                  detail: selectedNode.safeErrorMessage,
                  correlationId: '',
                })
              "
              compact
            />
            <dl class="metadata">
              <div>
                <dt>
                  {{ $t("runs.attempt", { attempt: selectedNode.attempt }) }}
                </dt>
                <dd>{{ selectedNodeRole }}</dd>
              </div>
            </dl>
            <div v-if="selectedNode.integrationNames?.length" class="chip-list">
              <span v-for="name in selectedNode.integrationNames" :key="name">{{
                name
              }}</span>
            </div>
            <dl class="node-relations">
              <div v-if="selectedNode.callbackSummary">
                <dt>{{ $t("runs.callback") }}</dt>
                <dd>
                  <SafeMarkdown :content="selectedNode.callbackSummary" />
                </dd>
              </div>
              <div v-if="selectedNode.artifactRefs.length">
                <dt>{{ $t("runs.artifacts") }}</dt>
                <dd>{{ selectedNode.artifactRefs.length }}</dd>
              </div>
              <div v-if="selectedNode.childRunRefs.length">
                <dt>{{ $t("runs.childRuns") }}</dt>
                <dd class="node-links">
                  <RouterLink
                    v-for="childRef in selectedNode.childRunRefs"
                    :key="childRef"
                    :to="`/runs/${childRef}`"
                  >
                    {{ $t("runs.openChildRun") }}
                  </RouterLink>
                </dd>
              </div>
              <div v-if="selectedNode.startedAt">
                <dt>{{ $t("runs.startedAt") }}</dt>
                <dd>{{ new Date(selectedNode.startedAt).toLocaleString() }}</dd>
              </div>
              <div v-if="selectedNode.finishedAt">
                <dt>{{ $t("runs.finishedAt") }}</dt>
                <dd>
                  {{ new Date(selectedNode.finishedAt).toLocaleString() }}
                </dd>
              </div>
            </dl>
            <section class="node-conversation" aria-live="polite">
              <h3>{{ $t("runs.nodeConversation") }}</h3>
              <ol v-if="selectedNodeEvents.length">
                <li v-for="event in selectedNodeEvents" :key="event.sequence">
                  <SafeMarkdown :content="event.displaySummary" />
                  <SafeMarkdown
                    v-if="event.displayProgress"
                    :content="event.displayProgress"
                  />
                  <time :datetime="event.occurredAt">
                    {{ new Date(event.occurredAt).toLocaleTimeString() }}
                  </time>
                </li>
              </ol>
              <p v-else>{{ $t("runs.noNodeActivity") }}</p>
            </section></template
          >
        </aside>
      </div>
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
.run-summary {
  display: flex;
  gap: 18px;
  align-items: center;
  flex-wrap: wrap;
  margin: -10px 0 16px;
  color: var(--muted);
  font-size: 0.86rem;
}
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
  display: grid;
  grid-template-columns: minmax(360px, 1.25fr) minmax(300px, 0.8fr) minmax(
      260px,
      0.65fr
    );
  min-height: 590px;
  border: 1px solid var(--border);
  border-radius: 11px;
  background: var(--surface);
  overflow: hidden;
}
.graph-panel,
.timeline-panel,
.node-panel {
  padding: 15px;
  overflow: auto;
}
.timeline-panel,
.node-panel {
  border-left: 1px solid var(--border);
}
.workspace-heading {
  display: flex;
  align-items: center;
  justify-content: space-between;
}
.workspace-heading h2 {
  margin: 0 0 12px;
}
.timeline {
  display: grid;
  gap: 0;
  margin: 0;
  padding: 0;
  list-style: none;
}
.timeline li {
  position: relative;
  display: grid;
  grid-template-columns: 18px 1fr;
  gap: 8px;
  padding: 0 0 18px;
}
.timeline li::before {
  position: absolute;
  left: 5px;
  top: 8px;
  bottom: -2px;
  width: 1px;
  background: var(--border);
  content: "";
}
.timeline li:last-child::before {
  display: none;
}
.timeline__marker {
  z-index: 1;
  width: 11px;
  height: 11px;
  margin-top: 5px;
  border-radius: 50%;
  background: var(--accent);
}
.timeline p {
  margin: 4px 0;
}
.timeline :deep(.safe-markdown > p),
.node-conversation :deep(.safe-markdown > p) {
  margin: 0;
}
.timeline small {
  color: var(--subtle);
}
.metadata dt {
  color: var(--subtle);
  font-size: 0.8rem;
}
.metadata dd {
  margin: 4px 0;
}
.node-relations {
  display: grid;
  gap: 10px;
  margin-top: 16px;
}
.node-relations dt {
  color: var(--subtle);
  font-size: 0.78rem;
}
.node-relations dd {
  margin: 3px 0 0;
}
.node-links {
  display: grid;
  gap: 4px;
}
.node-conversation {
  margin-top: 18px;
  padding-top: 14px;
  border-top: 1px solid var(--border);
}
.node-conversation ol {
  display: grid;
  gap: 10px;
  padding: 0;
  list-style: none;
}
.node-conversation li {
  padding: 9px;
  border-radius: 8px;
  background: var(--panel);
}
.node-conversation p {
  margin: 4px 0;
}
.node-conversation time {
  color: var(--subtle);
  font-size: 0.75rem;
}
.chip-list {
  display: flex;
  gap: 6px;
  flex-wrap: wrap;
}
.chip-list span {
  padding: 5px 8px;
  border-radius: 999px;
  background: var(--accent-soft);
  font-size: 0.78rem;
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
.empty-compact {
  color: var(--muted);
}
@media (max-width: 1150px) {
  .run-workspace {
    grid-template-columns: 1fr 1fr;
  }
  .node-panel {
    grid-column: 1/-1;
    border-left: 0;
    border-top: 1px solid var(--border);
  }
}
@media (max-width: 760px) {
  .gate-strip article,
  .run-workspace,
  .run-bottom {
    grid-template-columns: 1fr;
  }
  .timeline-panel,
  .node-panel {
    border-left: 0;
    border-top: 1px solid var(--border);
  }
  .run-summary .live-indicator {
    margin-left: 0;
  }
  .graph-panel,
  .timeline-panel,
  .node-panel {
    max-height: none;
  }
  .artifact-row {
    grid-template-columns: 1fr auto;
  }
  .artifact-row span:last-child {
    display: none;
  }
}
</style>
