<script setup lang="ts">
import {
  computed,
  nextTick,
  onBeforeUnmount,
  onMounted,
  ref,
  watch,
} from "vue";

import type {
  RunEdge,
  RunNode,
} from "@/shared/api/generated/openapi/types.gen";
import {
  layoutRunGraph,
  runGraphNodeHeight,
  runGraphNodeWidth,
} from "@/features/runs/run-graph-layout";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

const props = defineProps<{
  nodes: RunNode[];
  edges: RunEdge[];
  selectedRef?: string;
}>();
const emit = defineEmits<{ select: [node: RunNode] }>();

const zoom = ref(1);
const viewport = ref<HTMLElement>();
let resizeObserver: ResizeObserver | undefined;

const layout = computed(() => layoutRunGraph(props.nodes, props.edges));

function changeZoom(delta: number): void {
  zoom.value = Math.min(1.5, Math.max(0.5, zoom.value + delta));
}

function fit(): void {
  const width = viewport.value?.clientWidth ?? 0;
  if (!width || !layout.value.width) return;
  zoom.value = Math.min(1, Math.max(0.5, (width - 28) / layout.value.width));
  if (viewport.value) {
    viewport.value.scrollLeft = 0;
    viewport.value.scrollTop = 0;
  }
}

watch(
  () => [props.nodes.length, props.edges.length],
  () => void nextTick(fit),
);
onMounted(() => {
  resizeObserver = new ResizeObserver(() => fit());
  if (viewport.value) resizeObserver.observe(viewport.value);
  void nextTick(fit);
});
onBeforeUnmount(() => resizeObserver?.disconnect());
</script>

<template>
  <section class="graph-canvas-shell">
    <div class="graph-toolbar" :aria-label="$t('runs.graphControls')">
      <button
        class="icon-button"
        type="button"
        :aria-label="$t('runs.zoomOut')"
        :disabled="zoom <= 0.5"
        @click="changeZoom(-0.1)"
      >
        −
      </button>
      <output :aria-label="$t('runs.zoom')">
        {{ Math.round(zoom * 100) }}%
      </output>
      <button
        class="icon-button"
        type="button"
        :aria-label="$t('runs.zoomIn')"
        :disabled="zoom >= 1.5"
        @click="changeZoom(0.1)"
      >
        +
      </button>
      <button class="button button--ghost" type="button" @click="fit">
        {{ $t("runs.fitGraph") }}
      </button>
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
                markerWidth="8"
                markerHeight="8"
                refX="7"
                refY="4"
                orient="auto"
              >
                <path d="M 0 0 L 8 4 L 0 8 z" />
              </marker>
            </defs>
            <g v-for="item in layout.edges" :key="item.edge.ref">
              <path
                :d="item.path"
                :class="
                  'graph-edge graph-edge--' + item.edge.type.toLowerCase()
                "
                marker-end="url(#run-graph-arrow)"
              />
              <text
                v-if="item.edge.label"
                class="graph-edge-label"
                :x="item.labelX"
                :y="item.labelY"
                text-anchor="middle"
              >
                {{ item.edge.label }}
              </text>
            </g>
          </svg>
          <button
            v-for="item in layout.nodes"
            :key="item.node.ref"
            type="button"
            class="canvas-node"
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
              <span class="canvas-node__type" aria-hidden="true">{{
                item.node.type === "HUMAN_GATE"
                  ? "◇"
                  : item.node.type === "EXTERNAL_ACTION"
                    ? "□"
                    : item.node.type === "ROOT_PROCESS"
                      ? "◆"
                      : "●"
              }}</span>
              <strong>{{ item.node.displayName }}</strong>
              <StatusBadge :state="item.node.state" />
            </span>
            <span class="canvas-node__role">
              {{
                item.node.role ||
                $t("runs.nodeTypes." + item.node.type)
              }}
            </span>
            <span class="canvas-node__progress">
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

    <div class="graph-mobile-list" role="list">
      <button
        v-for="item in layout.nodes"
        :key="item.node.ref"
        type="button"
        role="listitem"
        class="graph-mobile-node"
        :class="{
          'graph-mobile-node--selected': item.node.ref === selectedRef,
        }"
        @click="emit('select', item.node)"
      >
        <span>
          <strong>{{ item.node.displayName }}</strong>
          <small>{{ item.node.progressSummary || item.node.role }}</small>
        </span>
        <StatusBadge :state="item.node.state" />
      </button>
    </div>
  </section>
</template>

<style scoped>
.graph-canvas-shell {
  position: relative;
  min-height: 0;
}
.graph-toolbar {
  position: absolute;
  z-index: 4;
  top: 9px;
  right: 9px;
  display: flex;
  align-items: center;
  gap: 5px;
  padding: 5px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: color-mix(in srgb, var(--surface) 94%, transparent);
  box-shadow: 0 3px 12px rgba(16, 22, 30, 0.08);
}
.graph-toolbar output {
  min-width: 44px;
  color: var(--muted);
  font-size: 0.78rem;
  text-align: center;
}
.graph-viewport {
  min-height: 500px;
  max-height: 650px;
  overflow: auto;
  background:
    linear-gradient(var(--hairline) 1px, transparent 1px),
    linear-gradient(90deg, var(--hairline) 1px, transparent 1px),
    var(--canvas);
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
  font-size: 11px;
  paint-order: stroke;
  stroke: var(--canvas);
  stroke-width: 5px;
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
  border-radius: 10px;
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
  align-items: center;
  gap: 7px;
}
.canvas-node__heading strong,
.canvas-node__role,
.canvas-node__progress {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.canvas-node__type {
  color: var(--accent);
}
.canvas-node__role,
.canvas-node__progress {
  color: var(--muted);
  font-size: 0.78rem;
}
.canvas-node__progress {
  color: var(--text-secondary);
}
.graph-mobile-list {
  display: none;
}
@media (max-width: 760px) {
  .graph-toolbar,
  .graph-viewport {
    display: none;
  }
  .graph-mobile-list {
    display: grid;
    gap: 9px;
    padding: 10px;
  }
  .graph-mobile-node {
    display: grid;
    grid-template-columns: minmax(0, 1fr) auto;
    align-items: center;
    gap: 10px;
    min-height: 64px;
    padding: 10px 12px;
    border: 1px solid var(--border);
    border-radius: 9px;
    background: var(--surface);
    color: inherit;
    text-align: left;
  }
  .graph-mobile-node > span {
    display: grid;
    gap: 4px;
  }
  .graph-mobile-node small {
    overflow: hidden;
    color: var(--muted);
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .graph-mobile-node--selected {
    border-color: var(--accent);
  }
}
</style>
