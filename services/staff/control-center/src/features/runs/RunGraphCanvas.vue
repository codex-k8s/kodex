<script setup lang="ts">
import { Background } from "@vue-flow/background";
import { Controls } from "@vue-flow/controls";
import {
  VueFlow,
  useVueFlow,
  type GraphNode,
  type NodeMouseEvent,
} from "@vue-flow/core";
import { MiniMap } from "@vue-flow/minimap";
import {
  Bot,
  ListTree,
  Network,
  PlugZap,
  UserRoundCheck,
  Workflow,
} from "@lucide/vue";
import { computed, nextTick, ref, watch } from "vue";
import type { Component } from "vue";
import { useI18n } from "vue-i18n";

import "@vue-flow/core/dist/style.css";
import "@vue-flow/core/dist/theme-default.css";
import "@vue-flow/controls/dist/style.css";
import "@vue-flow/minimap/dist/style.css";

import RunGraphEdge from "@/features/runs/RunGraphEdge.vue";
import RunGraphNode from "@/features/runs/RunGraphNode.vue";
import {
  createRunGraphFlowElements,
  runGraphFitViewOptions,
  runGraphMaximumZoom,
  runGraphMinimumZoom,
  type RunGraphNodeData,
} from "@/features/runs/run-graph-flow";
import { layoutRunGraph } from "@/features/runs/run-graph-layout";
import { runGraphViewportCommand } from "@/features/runs/run-graph-viewport";
import type {
  RunEdge,
  RunNode,
} from "@/shared/api/generated/openapi/types.gen";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

const props = withDefaults(
  defineProps<{
    nodes: RunNode[];
    edges: RunEdge[];
    selectedRef?: string;
    futureNodeRefs?: string[];
    activeNodeRefs?: string[];
  }>(),
  {
    selectedRef: undefined,
    futureNodeRefs: () => [],
    activeNodeRefs: () => [],
  },
);
const emit = defineEmits<{
  select: [node: RunNode];
  details: [node: RunNode];
}>();
const { t } = useI18n();

const flowId = "run-session-graph";
const viewMode = ref<"graph" | "outline">("graph");
const outline = ref<HTMLElement>();
const userAdjustedView = ref(false);
const programmaticViewportChange = ref(false);
const futureRefs = computed(() => new Set(props.futureNodeRefs));
const activeRefs = computed(() => new Set(props.activeNodeRefs));
const nodeByRef = computed(
  () => new Map(props.nodes.map((node) => [node.ref, node])),
);
const layout = computed(() => layoutRunGraph(props.nodes, props.edges));
const { fitView, getViewport, onInit, setViewport, zoomIn, zoomOut } =
  useVueFlow(flowId);

const flowElements = computed(() =>
  createRunGraphFlowElements(props.nodes, props.edges, {
    selectedRef: props.selectedRef,
    futureRefs: futureRefs.value,
    activeRefs: activeRefs.value,
    nodeAccessibleLabel,
    edgeAccessibleLabel,
  }),
);
const runSignature = computed(() =>
  [...new Set(props.nodes.map((node) => node.runRef))].sort().join("\u0000"),
);
const graphSignature = computed(() => {
  const nodeSignature = props.nodes
    .map((node) => node.ref)
    .sort()
    .join("\u0000");
  const edgeSignature = props.edges
    .map(
      (edge) =>
        `${edge.ref}\u0001${edge.sourceNodeRef}\u0001${edge.targetNodeRef}\u0001${edge.type}`,
    )
    .sort()
    .join("\u0000");
  return `${nodeSignature}\u0002${edgeSignature}`;
});
const outlineItems = computed(() => {
  const hierarchyEdges = props.edges.filter(
    (edge) =>
      edge.type !== "CALLBACK_TO" &&
      edge.type !== "RETRY_OF" &&
      nodeByRef.value.has(edge.sourceNodeRef) &&
      nodeByRef.value.has(edge.targetNodeRef),
  );
  const outgoing = new Map<string, RunEdge[]>();
  const indegree = new Map(props.nodes.map((node) => [node.ref, 0]));
  const depth = new Map(props.nodes.map((node) => [node.ref, 0]));

  for (const edge of hierarchyEdges) {
    outgoing.set(edge.sourceNodeRef, [
      ...(outgoing.get(edge.sourceNodeRef) ?? []),
      edge,
    ]);
    indegree.set(
      edge.targetNodeRef,
      (indegree.get(edge.targetNodeRef) ?? 0) + 1,
    );
  }

  const queue = props.nodes
    .filter((node) => indegree.get(node.ref) === 0)
    .sort(compareNodes);
  for (let index = 0; index < queue.length; index += 1) {
    const source = queue[index];
    if (!source) continue;
    for (const edge of outgoing.get(source.ref) ?? []) {
      depth.set(
        edge.targetNodeRef,
        Math.max(
          depth.get(edge.targetNodeRef) ?? 0,
          (depth.get(source.ref) ?? 0) + 1,
        ),
      );
      const remaining = (indegree.get(edge.targetNodeRef) ?? 1) - 1;
      indegree.set(edge.targetNodeRef, remaining);
      if (remaining === 0) {
        const target = nodeByRef.value.get(edge.targetNodeRef);
        if (target) queue.push(target);
      }
    }
  }

  const incoming = new Map<string, RunEdge[]>();
  for (const edge of props.edges) {
    if (
      !nodeByRef.value.has(edge.sourceNodeRef) ||
      !nodeByRef.value.has(edge.targetNodeRef)
    ) {
      continue;
    }
    incoming.set(edge.targetNodeRef, [
      ...(incoming.get(edge.targetNodeRef) ?? []),
      edge,
    ]);
  }

  return layout.value.nodes.map((item) => ({
    node: item.node,
    depth: Math.min(depth.get(item.node.ref) ?? 0, 4),
    incoming: incoming.get(item.node.ref) ?? [],
  }));
});

onInit(() => {
  void fit(false);
});
watch(runSignature, (current, previous) => {
  if (current !== previous) userAdjustedView.value = false;
});
watch(graphSignature, () => {
  if (!userAdjustedView.value) void nextTick(() => fit(false));
});

async function fit(userInitiated = true): Promise<void> {
  if (userInitiated) userAdjustedView.value = true;
  programmaticViewportChange.value = true;
  try {
    await fitView(runGraphFitViewOptions);
  } finally {
    programmaticViewportChange.value = false;
  }
}

function setViewMode(mode: "graph" | "outline"): void {
  viewMode.value = mode;
  if (mode === "graph" && !userAdjustedView.value) {
    void nextTick(() => fit(false));
  }
}

function handleNodeClick(event: NodeMouseEvent): void {
  emit("select", domainNode(event.node));
}

function handleNodeDoubleClick(event: NodeMouseEvent): void {
  const node = domainNode(event.node);
  emit("select", node);
  emit("details", node);
}

function domainNode(node: GraphNode): RunNode {
  return (node.data as RunGraphNodeData).node;
}

function markUserAdjusted(): void {
  if (!programmaticViewportChange.value) userAdjustedView.value = true;
}

async function handleViewportKeydown(event: KeyboardEvent): Promise<void> {
  if (
    (event.target as Element | null)?.closest(
      "button, a, input, textarea, select",
    )
  )
    return;
  const command = runGraphViewportCommand(event);
  if (!command) return;
  event.preventDefault();
  userAdjustedView.value = true;

  switch (command.type) {
    case "FIT":
      await fit(true);
      return;
    case "ZOOM_IN":
      await zoomIn({ duration: 120 });
      return;
    case "ZOOM_OUT":
      await zoomOut({ duration: 120 });
      return;
    case "PAN": {
      const viewport = getViewport();
      await setViewport(
        {
          x: viewport.x + command.x,
          y: viewport.y + command.y,
          zoom: viewport.zoom,
        },
        { duration: 90 },
      );
    }
  }
}

function edgeDisplayLabel(edge: RunEdge): string {
  const label = edge.label.trim();
  if (label) return label;
  switch (edge.type) {
    case "DELEGATED_TO":
      return t("runs.source.AGENT_DELEGATION");
    case "CALLBACK_TO":
      return t("runs.callback");
    case "RETRY_OF":
      return t("runs.retry");
    case "CONTINUES":
      return t("runs.continueTask");
    case "WAITING_FOR":
      return t("states.WAITING");
  }
}

function edgeAccessibleLabel(edge: RunEdge): string {
  const source = nodeByRef.value.get(edge.sourceNodeRef)?.displayName ?? "";
  const target = nodeByRef.value.get(edge.targetNodeRef)?.displayName ?? "";
  return [source, edgeDisplayLabel(edge), target].filter(Boolean).join(" → ");
}

function nodeIcon(type: RunNode["type"]): Component {
  switch (type) {
    case "ROOT_PROCESS":
      return Workflow;
    case "HUMAN_GATE":
      return UserRoundCheck;
    case "EXTERNAL_ACTION":
      return PlugZap;
    case "AGENT_EXECUTION":
      return Bot;
  }
}

function nodeSurface(node: RunNode): "session" | "control" {
  return node.type === "ROOT_PROCESS" || node.type === "AGENT_EXECUTION"
    ? "session"
    : "control";
}

function isFutureNode(node: RunNode): boolean {
  return (
    futureRefs.value.has(node.ref) ||
    node.planned === true ||
    node.state === "PLANNED"
  );
}

function isActiveNode(node: RunNode): boolean {
  return activeRefs.value.has(node.ref);
}

function nodeAccessibleLabel(node: RunNode): string {
  return [
    t(`runs.${nodeSurface(node)}Node`),
    node.displayName,
    node.role || t(`runs.nodeTypes.${node.type}`),
    t(`states.${node.state}`),
  ].join(" · ");
}

function moveOutlineFocus(event: KeyboardEvent): void {
  if (!["ArrowDown", "ArrowUp", "Home", "End"].includes(event.key)) return;
  const items = Array.from(
    outline.value?.querySelectorAll<HTMLButtonElement>("[role='treeitem']") ??
      [],
  );
  if (!items.length) return;
  const currentIndex = items.indexOf(event.currentTarget as HTMLButtonElement);
  const targetIndex =
    event.key === "Home"
      ? 0
      : event.key === "End"
        ? items.length - 1
        : event.key === "ArrowDown"
          ? Math.min(items.length - 1, currentIndex + 1)
          : Math.max(0, currentIndex - 1);
  event.preventDefault();
  items[targetIndex]?.focus();
}

function miniMapNodeColor(node: GraphNode): string {
  const data = node.data as RunGraphNodeData;
  if (data.selected) return "var(--accent)";
  if (data.future) return "var(--subtle)";
  switch (data.node.state) {
    case "RUNNING":
      return "var(--accent)";
    case "SUCCEEDED":
      return "var(--success)";
    case "FAILED":
    case "CANCELLED":
      return "var(--danger)";
    case "WAITING":
    case "QUEUED":
      return "var(--warning)";
    default:
      return "var(--border-strong)";
  }
}

function compareNodes(left: RunNode, right: RunNode): number {
  return (
    left.createdAt.localeCompare(right.createdAt) ||
    left.ref.localeCompare(right.ref)
  );
}
</script>

<template>
  <section
    class="graph-canvas-shell"
    :class="{ 'graph-canvas-shell--outline': viewMode === 'outline' }"
    @keydown.capture="handleViewportKeydown"
  >
    <div
      class="graph-toolbar"
      role="toolbar"
      :aria-label="$t('runs.graphControls')"
    >
      <div class="graph-view-switch" role="group">
        <button
          class="icon-button"
          type="button"
          :aria-label="$t('runs.graph')"
          :title="$t('runs.graph')"
          :aria-pressed="viewMode === 'graph'"
          @click="setViewMode('graph')"
        >
          <Network :size="18" aria-hidden="true" />
        </button>
        <button
          class="icon-button"
          type="button"
          :aria-label="$t('runs.connections')"
          :title="$t('runs.connections')"
          :aria-pressed="viewMode === 'outline'"
          @click="setViewMode('outline')"
        >
          <ListTree :size="18" aria-hidden="true" />
        </button>
      </div>
    </div>

    <div
      v-show="viewMode === 'graph'"
      class="graph-viewport"
      role="region"
      :aria-label="$t('runs.graph')"
      tabindex="0"
    >
      <VueFlow
        :id="flowId"
        class="run-flow"
        :nodes="flowElements.nodes"
        :edges="flowElements.edges"
        :min-zoom="runGraphMinimumZoom"
        :max-zoom="runGraphMaximumZoom"
        :nodes-draggable="false"
        :nodes-connectable="false"
        :elements-selectable="false"
        :disable-keyboard-a11y="true"
        :edges-updatable="false"
        :delete-key-code="null"
        :selection-key-code="null"
        :multi-selection-key-code="null"
        :select-nodes-on-drag="false"
        :pan-on-drag="true"
        :pan-on-scroll="false"
        :zoom-on-scroll="true"
        :zoom-on-pinch="true"
        :zoom-on-double-click="false"
        :prevent-scrolling="true"
        :apply-default="false"
        fit-view-on-init
        @node-click="handleNodeClick"
        @node-double-click="handleNodeDoubleClick"
        @mini-map-node-click="handleNodeClick"
        @mini-map-node-double-click="handleNodeDoubleClick"
        @viewport-change-start="markUserAdjusted"
      >
        <template #node-runNode="nodeProps">
          <RunGraphNode v-bind="nodeProps" @select="emit('select', $event)" />
        </template>
        <template #edge-runEdge="edgeProps">
          <RunGraphEdge v-bind="edgeProps" />
        </template>
        <Background :gap="24" :size="1" pattern-color="var(--hairline)" />
        <Controls
          position="top-right"
          :show-interactive="false"
          :fit-view-params="runGraphFitViewOptions"
          @zoom-in="markUserAdjusted"
          @zoom-out="markUserAdjusted"
          @fit-view="markUserAdjusted"
        />
        <MiniMap
          position="bottom-right"
          pannable
          zoomable
          :node-color="miniMapNodeColor"
          node-stroke-color="var(--surface)"
          :node-border-radius="4"
          :aria-label="$t('runs.minimap')"
        />
      </VueFlow>
      <div v-if="!nodes.length" class="graph-empty" role="status">
        <Network :size="24" aria-hidden="true" />
        <p>{{ $t("runs.noEvents") }}</p>
      </div>
    </div>

    <div
      v-show="viewMode === 'outline'"
      ref="outline"
      class="graph-outline"
      role="tree"
      :aria-label="$t('runs.connections')"
    >
      <button
        v-for="item in outlineItems"
        :key="item.node.ref"
        type="button"
        role="treeitem"
        class="graph-outline-node"
        :style="{ '--tree-depth': item.depth }"
        :aria-level="item.depth + 1"
        :aria-selected="item.node.ref === selectedRef"
        :class="{
          'graph-outline-node--selected': item.node.ref === selectedRef,
          'graph-outline-node--future': isFutureNode(item.node),
          'graph-outline-node--active': isActiveNode(item.node),
        }"
        @keydown="moveOutlineFocus"
        @click="emit('select', item.node)"
        @dblclick="emit('details', item.node)"
      >
        <span class="graph-outline-node__body">
          <span class="graph-outline-node__heading">
            <component
              :is="nodeIcon(item.node.type)"
              :size="17"
              aria-hidden="true"
            />
            <strong>{{ item.node.displayName }}</strong>
          </span>
          <small>
            {{ item.node.role || $t("runs.nodeTypes." + item.node.type) }}
          </small>
          <small v-if="item.node.progressSummary || item.node.inputSummary">
            {{ item.node.progressSummary || item.node.inputSummary }}
          </small>
          <span
            v-for="edge in item.incoming"
            :key="edge.ref"
            class="graph-outline-node__connection"
          >
            <span aria-hidden="true">→</span>
            {{ edgeAccessibleLabel(edge) }}
          </span>
        </span>
        <StatusBadge :state="item.node.state" />
      </button>
    </div>

    <aside
      v-if="nodes.length"
      class="graph-legend"
      :aria-label="$t('runs.connections')"
    >
      <header>
        <Network :size="16" aria-hidden="true" />
        <strong>{{ $t("runs.connections") }}</strong>
      </header>
      <div class="graph-legend__edges">
        <span class="graph-legend__item">
          <i class="graph-legend__line graph-legend__line--delegated_to" />
          {{ $t("runs.source.AGENT_DELEGATION") }}
        </span>
        <span class="graph-legend__item">
          <i class="graph-legend__line graph-legend__line--callback_to" />
          {{ $t("runs.callback") }}
        </span>
        <span class="graph-legend__item">
          <i class="graph-legend__line graph-legend__line--continues" />
          {{ $t("runs.continueTask") }}
        </span>
        <span class="graph-legend__item">
          <i class="graph-legend__line graph-legend__line--retry_of" />
          {{ $t("runs.retry") }}
        </span>
        <span class="graph-legend__item">
          <i class="graph-legend__line graph-legend__line--waiting_for" />
          {{ $t("states.WAITING") }}
        </span>
      </div>
      <div class="graph-legend__states">
        <span class="graph-legend__item">
          <i class="graph-legend__node graph-legend__node--session" />
          {{ $t("runs.sessionNode") }}
        </span>
        <StatusBadge state="RUNNING" />
        <StatusBadge state="WAITING" />
        <StatusBadge state="SUCCEEDED" />
        <StatusBadge state="FAILED" />
      </div>
    </aside>
  </section>
</template>

<style scoped>
.graph-canvas-shell {
  position: relative;
  display: block;
  width: 100%;
  height: 100%;
  min-width: 0;
  min-height: 0;
  max-width: 100%;
  overflow: hidden;
}
.graph-toolbar {
  position: absolute;
  z-index: 12;
  top: 14px;
  left: 14px;
  display: flex;
  min-height: 42px;
  align-items: center;
  padding: 6px 10px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: color-mix(in srgb, var(--surface) 94%, transparent);
  box-shadow: 0 8px 24px rgba(16, 22, 30, 0.1);
  backdrop-filter: blur(8px);
}
.graph-view-switch {
  display: flex;
  align-items: center;
  gap: 5px;
}
.graph-toolbar .icon-button[aria-pressed="true"] {
  border-color: var(--accent);
  background: color-mix(in srgb, var(--accent) 12%, var(--surface));
  color: var(--accent);
}
.graph-viewport {
  position: absolute;
  inset: 0;
  min-width: 0;
  min-height: 0;
  overflow: hidden;
  background: var(--canvas);
  touch-action: none;
}
.run-flow {
  width: 100%;
  height: 100%;
  max-width: 100%;
  background: var(--canvas);
}
.run-flow :deep(.vue-flow__pane) {
  cursor: grab;
}
.run-flow :deep(.vue-flow__pane.dragging) {
  cursor: grabbing;
}
.run-flow :deep(.vue-flow__node) {
  border: 0;
  background: transparent;
  box-shadow: none;
  cursor: pointer;
}
.run-flow :deep(.vue-flow__node:focus-visible) {
  border-radius: 10px;
  outline: 3px solid color-mix(in srgb, var(--accent) 48%, transparent);
  outline-offset: 4px;
}
.run-flow :deep(.vue-flow__edge-path) {
  fill: none;
}
.run-flow :deep(.vue-flow__controls),
.run-flow :deep(.vue-flow__minimap) {
  overflow: hidden;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: color-mix(in srgb, var(--surface) 94%, transparent);
  box-shadow: 0 8px 24px rgba(16, 22, 30, 0.1);
  backdrop-filter: blur(8px);
}
.graph-empty {
  position: absolute;
  z-index: 10;
  inset: 0;
  display: grid;
  place-content: center;
  place-items: center;
  gap: 8px;
  padding: 24px;
  color: var(--muted);
  text-align: center;
  pointer-events: none;
}
.graph-empty p {
  margin: 0;
}
.run-flow :deep(.vue-flow__controls) {
  top: 14px;
  right: 14px;
}
.run-flow :deep(.vue-flow__controls-button) {
  width: 34px;
  height: 34px;
  border-color: var(--border);
  background: transparent;
  color: var(--text);
}
.run-flow :deep(.vue-flow__controls-button:hover) {
  background: var(--panel);
}
.run-flow :deep(.vue-flow__minimap) {
  right: 14px;
  bottom: 14px;
  width: 152px;
  height: 104px;
}
.graph-outline {
  position: absolute;
  inset: 0;
  display: grid;
  align-content: start;
  min-width: 0;
  min-height: 0;
  gap: 8px;
  padding: 72px 14px 126px;
  overflow: auto;
  background: var(--canvas);
}
.graph-outline-node {
  display: grid;
  width: calc(100% - min(calc(var(--tree-depth) * 18px), 72px));
  min-width: 0;
  min-height: 72px;
  grid-template-columns: minmax(0, 1fr) auto;
  align-items: center;
  gap: 10px;
  margin-inline-start: min(calc(var(--tree-depth) * 18px), 72px);
  padding: 10px 12px;
  overflow: hidden;
  border: 1px solid var(--border);
  border-inline-start: 4px solid var(--border-strong);
  border-radius: 8px;
  background: var(--surface);
  color: inherit;
  text-align: start;
}
.graph-outline-node--future {
  border-style: dashed;
  opacity: 0.7;
}
.graph-outline-node--active {
  border-inline-start-color: var(--accent);
}
.graph-outline-node__body,
.graph-outline-node__heading {
  display: grid;
  min-width: 0;
}
.graph-outline-node__body {
  gap: 4px;
}
.graph-outline-node__heading {
  grid-template-columns: auto minmax(0, 1fr);
  align-items: center;
  gap: 7px;
}
.graph-outline-node strong,
.graph-outline-node small,
.graph-outline-node__connection {
  min-width: 0;
  overflow-wrap: anywhere;
}
.graph-outline-node small,
.graph-outline-node__connection {
  color: var(--muted);
  font-size: 0.78rem;
}
.graph-outline-node__connection {
  display: inline-flex;
  align-items: baseline;
  gap: 5px;
}
.graph-outline-node--selected {
  border-color: var(--accent);
  border-inline-start-color: var(--accent);
  box-shadow: 0 0 0 2px color-mix(in srgb, var(--accent) 16%, transparent);
  opacity: 1;
}
.graph-legend {
  position: absolute;
  z-index: 12;
  bottom: 14px;
  left: 14px;
  display: grid;
  width: min(560px, calc(100% - 196px));
  gap: 8px;
  padding: 10px 12px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: color-mix(in srgb, var(--surface) 94%, transparent);
  box-shadow: 0 8px 24px rgba(16, 22, 30, 0.1);
  backdrop-filter: blur(8px);
}
.graph-legend > header,
.graph-legend__edges,
.graph-legend__states,
.graph-legend__item {
  display: flex;
  align-items: center;
}
.graph-legend > header {
  gap: 6px;
  color: var(--muted);
  font-size: 0.76rem;
}
.graph-legend__edges,
.graph-legend__states {
  flex-wrap: wrap;
  gap: 7px 12px;
}
.graph-legend__item {
  gap: 6px;
  color: var(--muted);
  font-size: 0.72rem;
  white-space: nowrap;
}
.graph-legend__line {
  display: block;
  width: 25px;
  border-top: 2px solid var(--border-strong);
}
.graph-legend__node {
  width: 14px;
  height: 10px;
  border: 1px solid var(--border-strong);
  border-radius: 3px;
  background: var(--surface);
}
.graph-legend__node--session {
  border-left: 3px solid var(--accent);
}
.graph-legend__line--delegated_to {
  border-color: var(--accent);
}
.graph-legend__line--callback_to {
  border-color: var(--success);
  border-top-style: dashed;
}
.graph-legend__line--retry_of {
  border-color: var(--warning);
  border-top-style: dotted;
}
.graph-legend__line--continues {
  border-color: color-mix(in srgb, var(--accent) 58%, var(--muted));
  border-top-width: 3px;
}
.graph-legend__line--waiting_for {
  border-color: var(--subtle);
  border-top-style: dashed;
}
@media (max-width: 760px) {
  .graph-toolbar {
    top: 8px;
    bottom: auto;
    left: 8px;
    min-height: 40px;
    padding: 4px;
  }
  .run-flow :deep(.vue-flow__controls) {
    top: 8px;
    right: 8px;
  }
  .run-flow :deep(.vue-flow__minimap) {
    right: 8px;
    bottom: 8px;
    width: 124px;
    height: 82px;
  }
  .graph-outline-node {
    width: calc(100% - min(calc(var(--tree-depth) * 12px), 36px));
    margin-inline-start: min(calc(var(--tree-depth) * 12px), 36px);
  }
  .graph-legend {
    right: 140px;
    bottom: 8px;
    left: 8px;
    width: auto;
    max-height: 108px;
    overflow: auto;
  }
}
</style>
