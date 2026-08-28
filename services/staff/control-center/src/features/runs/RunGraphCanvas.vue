<script setup lang="ts">
import {
  Bot,
  ListTree,
  Maximize2,
  Minus,
  Network,
  PlugZap,
  Plus,
  UserRoundCheck,
  Workflow,
} from "@lucide/vue";
import {
  computed,
  nextTick,
  onBeforeUnmount,
  onMounted,
  ref,
  watch,
} from "vue";
import type { Component } from "vue";
import { useI18n } from "vue-i18n";

import {
  layoutRunGraph,
  runGraphNodeHeight,
  runGraphNodeWidth,
} from "@/features/runs/run-graph-layout";
import type {
  RunEdge,
  RunNode,
} from "@/shared/api/generated/openapi/types.gen";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

const props = defineProps<{
  nodes: RunNode[];
  edges: RunEdge[];
  selectedRef?: string;
}>();
const emit = defineEmits<{ select: [node: RunNode] }>();
const { t } = useI18n();

const minimumZoom = 0.85;
const maximumZoom = 1.5;
const zoom = ref(1);
const viewMode = ref<"graph" | "outline">("graph");
const viewport = ref<HTMLElement>();
const outline = ref<HTMLElement>();
const userAdjustedZoom = ref(false);
let resizeObserver: ResizeObserver | undefined;

const layout = computed(() => layoutRunGraph(props.nodes, props.edges));
const nodeByRef = computed(
  () => new Map(props.nodes.map((node) => [node.ref, node])),
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

function changeZoom(delta: number): void {
  userAdjustedZoom.value = true;
  zoom.value = Math.min(maximumZoom, Math.max(minimumZoom, zoom.value + delta));
}

function fit(userInitiated = false): void {
  const width = viewport.value?.clientWidth ?? 0;
  const height = viewport.value?.clientHeight ?? 0;
  if (!width || !height || !layout.value.width || !layout.value.height) return;
  if (userInitiated) userAdjustedZoom.value = true;
  zoom.value = Math.min(
    1,
    Math.max(
      minimumZoom,
      Math.min(
        (width - 28) / layout.value.width,
        (height - 28) / layout.value.height,
      ),
    ),
  );
  if (viewport.value) {
    viewport.value.scrollLeft = 0;
    viewport.value.scrollTop = 0;
  }
}

function setViewMode(mode: "graph" | "outline"): void {
  viewMode.value = mode;
  if (mode === "graph" && !userAdjustedZoom.value) {
    void nextTick(() => fit());
  }
}

watch(runSignature, (current, previous) => {
  if (current !== previous) userAdjustedZoom.value = false;
});
watch(graphSignature, () => {
  if (!userAdjustedZoom.value) void nextTick(() => fit());
});
onMounted(() => {
  resizeObserver = new ResizeObserver(() => {
    if (!userAdjustedZoom.value) fit();
  });
  if (viewport.value) resizeObserver.observe(viewport.value);
  void nextTick(() => fit());
});
onBeforeUnmount(() => resizeObserver?.disconnect());

function compactDisplayName(displayName: string): string {
  const characters = Array.from(displayName);
  if (characters.length <= 44) return displayName;
  return `${characters.slice(0, 27).join("")}…${characters.slice(-14).join("")}`;
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

function compactEdgeLabel(edge: RunEdge): string {
  return compactDisplayName(edgeDisplayLabel(edge));
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
      <div v-if="viewMode === 'graph'" class="graph-zoom-controls">
        <button
          class="icon-button"
          type="button"
          :aria-label="$t('runs.zoomOut')"
          :title="$t('runs.zoomOut')"
          :disabled="zoom <= minimumZoom"
          @click="changeZoom(-0.1)"
        >
          <Minus :size="17" aria-hidden="true" />
        </button>
        <output :aria-label="$t('runs.zoom')">
          {{ Math.round(zoom * 100) }}%
        </output>
        <button
          class="icon-button"
          type="button"
          :aria-label="$t('runs.zoomIn')"
          :title="$t('runs.zoomIn')"
          :disabled="zoom >= maximumZoom"
          @click="changeZoom(0.1)"
        >
          <Plus :size="17" aria-hidden="true" />
        </button>
        <button
          class="button button--ghost graph-fit-button"
          type="button"
          :title="$t('runs.fitGraph')"
          @click="fit(true)"
        >
          <Maximize2 :size="16" aria-hidden="true" />
          <span>{{ $t("runs.fitGraph") }}</span>
        </button>
      </div>
    </div>

    <div
      ref="viewport"
      class="graph-viewport"
      role="region"
      :aria-label="$t('runs.graph')"
      tabindex="0"
    >
      <div
        class="graph-stage"
        :style="{
          width: layout.width * zoom + 'px',
          height: layout.height * zoom + 'px',
        }"
      >
        <div
          class="graph-surface"
          :style="{
            width: layout.width + 'px',
            height: layout.height + 'px',
            transform: 'scale(' + zoom + ')',
          }"
        >
          <svg
            class="graph-edges"
            :viewBox="'0 0 ' + layout.width + ' ' + layout.height"
            aria-hidden="true"
          >
            <defs>
              <marker
                id="run-graph-arrow"
                markerWidth="9"
                markerHeight="9"
                refX="8"
                refY="4.5"
                orient="auto"
              >
                <path d="M 0 0 L 9 4.5 L 0 9 z" />
              </marker>
            </defs>
            <g
              v-for="item in layout.edges"
              :key="item.edge.ref"
              :data-edge-ref="item.edge.ref"
              :data-edge-type="item.edge.type"
            >
              <title>{{ edgeAccessibleLabel(item.edge) }}</title>
              <path
                :d="item.path"
                :class="
                  'graph-edge graph-edge--' + item.edge.type.toLowerCase()
                "
                marker-end="url(#run-graph-arrow)"
              />
              <text
                class="graph-edge-label"
                :x="item.labelX"
                :y="item.labelY"
                text-anchor="middle"
              >
                {{ compactEdgeLabel(item.edge) }}
              </text>
            </g>
          </svg>
          <button
            v-for="item in layout.nodes"
            :key="item.node.ref"
            type="button"
            class="canvas-node"
            :data-node-ref="item.node.ref"
            :data-node-type="item.node.type"
            :class="[
              'canvas-node--' + item.node.state.toLowerCase(),
              { 'canvas-node--selected': item.node.ref === selectedRef },
            ]"
            :style="{
              left: item.x + 'px',
              top: item.y + 'px',
              width: runGraphNodeWidth + 'px',
              height: runGraphNodeHeight + 'px',
            }"
            :aria-pressed="item.node.ref === selectedRef"
            @click="emit('select', item.node)"
          >
            <span class="canvas-node__heading">
              <component
                :is="nodeIcon(item.node.type)"
                class="canvas-node__type"
                :size="16"
                aria-hidden="true"
              />
              <strong :title="item.node.displayName">
                {{ compactDisplayName(item.node.displayName) }}
              </strong>
              <StatusBadge :state="item.node.state" />
            </span>
            <span class="canvas-node__role" :title="item.node.role">
              {{ item.node.role || $t("runs.nodeTypes." + item.node.type) }}
            </span>
            <span
              class="canvas-node__progress"
              :title="item.node.progressSummary || item.node.inputSummary"
            >
              {{
                item.node.progressSummary ||
                item.node.inputSummary ||
                $t("runs.waitingForActivity")
              }}
            </span>
          </button>
        </div>
      </div>
    </div>

    <div
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
        }"
        @keydown="moveOutlineFocus"
        @click="emit('select', item.node)"
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
  </section>
</template>

<style scoped>
.graph-canvas-shell {
  display: grid;
  min-width: 0;
  min-height: 0;
}
.graph-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  min-height: 48px;
  padding: 6px 10px;
  border-bottom: 1px solid var(--border);
  background: var(--panel);
}
.graph-view-switch,
.graph-zoom-controls {
  display: flex;
  align-items: center;
  gap: 5px;
}
.graph-toolbar .icon-button[aria-pressed="true"] {
  border-color: var(--accent);
  background: color-mix(in srgb, var(--accent) 12%, var(--surface));
  color: var(--accent);
}
.graph-zoom-controls output {
  min-width: 44px;
  color: var(--muted);
  font-size: 0.78rem;
  text-align: center;
}
.graph-fit-button {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}
.graph-viewport {
  min-height: 500px;
  max-height: 650px;
  overflow: auto;
  background:
    linear-gradient(var(--hairline) 1px, transparent 1px),
    linear-gradient(90deg, var(--hairline) 1px, transparent 1px), var(--canvas);
  background-size: 24px 24px;
  scrollbar-gutter: stable;
}
.graph-stage,
.graph-surface {
  position: relative;
}
.graph-surface {
  transform-origin: left top;
}
.graph-edges {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  overflow: visible;
}
.graph-edge {
  fill: none;
  stroke: var(--border-strong);
  stroke-width: 2;
}
.graph-edge--callback_to,
.graph-edge--retry_of {
  stroke-dasharray: 6 5;
}
.graph-edges marker path {
  fill: var(--border-strong);
}
.graph-edge-label {
  fill: var(--subtle);
  font-size: 13px;
  font-weight: 600;
  paint-order: stroke;
  stroke: var(--canvas);
  stroke-linejoin: round;
  stroke-width: 6px;
}
.canvas-node {
  position: absolute;
  display: grid;
  align-content: start;
  gap: 7px;
  padding: 12px;
  overflow: hidden;
  border: 1px solid var(--border-strong);
  border-left-width: 4px;
  border-radius: 8px;
  background: var(--surface);
  box-shadow: 0 3px 12px rgba(16, 22, 30, 0.07);
  color: var(--text);
  text-align: left;
  cursor: pointer;
}
.canvas-node--running {
  border-left-color: var(--accent);
}
.canvas-node--waiting {
  border-left-color: var(--warning);
}
.canvas-node--succeeded {
  border-left-color: var(--success);
}
.canvas-node--failed,
.canvas-node--cancelled {
  border-left-color: var(--danger);
}
.canvas-node--selected {
  border-color: var(--accent);
  box-shadow: 0 0 0 3px color-mix(in srgb, var(--accent) 18%, transparent);
}
.canvas-node__heading {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr) auto;
  align-items: start;
  gap: 7px;
}
.canvas-node__heading strong,
.canvas-node__role,
.canvas-node__progress {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.canvas-node__heading strong {
  display: -webkit-box;
  overflow: hidden;
  line-height: 1.25;
  white-space: normal;
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 2;
}
.canvas-node__type {
  flex: none;
  color: var(--accent);
}
.canvas-node__role,
.canvas-node__progress {
  color: var(--muted);
  font-size: 0.82rem;
}
.canvas-node__progress {
  color: var(--text-secondary);
}
.graph-outline {
  display: none;
  min-width: 0;
  gap: 8px;
  padding: 10px;
  overflow-x: clip;
  background: var(--canvas);
}
.graph-canvas-shell--outline .graph-viewport {
  display: none;
}
.graph-canvas-shell--outline .graph-outline {
  display: grid;
}
.graph-outline-node {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  align-items: center;
  gap: 10px;
  width: calc(100% - min(calc(var(--tree-depth) * 18px), 72px));
  min-width: 0;
  min-height: 72px;
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
  gap: 5px;
  align-items: baseline;
}
.graph-outline-node--selected {
  border-color: var(--accent);
  border-inline-start-color: var(--accent);
  box-shadow: 0 0 0 2px color-mix(in srgb, var(--accent) 16%, transparent);
}
@media (max-width: 760px) {
  .graph-toolbar,
  .graph-viewport {
    display: none;
  }
  .graph-outline {
    display: grid;
  }
  .graph-outline-node {
    width: calc(100% - min(calc(var(--tree-depth) * 12px), 36px));
    margin-inline-start: min(calc(var(--tree-depth) * 12px), 36px);
  }
}
</style>
