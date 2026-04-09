<template>
  <div class="prototype-canvas">
    <div class="prototype-canvas__viewport">
      <div class="prototype-canvas__stage" :style="stageStyle">
        <svg
          class="prototype-canvas__relations"
          :viewBox="`0 0 ${viewport.canvasWidth} ${viewport.canvasHeight}`"
          :style="{ width: `${viewport.canvasWidth}px`, height: `${viewport.canvasHeight}px` }"
        >
          <g v-for="relation in relations" :key="relation.relationId">
            <path
              class="prototype-canvas__edge"
              :class="{
                'prototype-canvas__edge--highlighted': relation.highlighted,
                'prototype-canvas__edge--dimmed': relation.dimmed,
              }"
              :d="relation.path"
            />
            <text
              class="prototype-canvas__edge-label"
              :class="{ 'prototype-canvas__edge-label--dimmed': relation.dimmed }"
              :x="relation.labelX"
              :y="relation.labelY"
              text-anchor="middle"
            >
              {{ relation.label }}
            </text>
          </g>
        </svg>

        <div
          v-for="initiative in initiatives"
          :key="initiative.initiativeId"
          class="prototype-canvas__cluster"
          :class="[
            `prototype-canvas__cluster--${initiative.accentToken}`,
            { 'prototype-canvas__cluster--dimmed': initiative.dimmed },
          ]"
          :style="{
            left: `${initiative.bounds.left}px`,
            top: `${initiative.bounds.top}px`,
            width: `${initiative.bounds.width}px`,
            height: `${initiative.bounds.height}px`,
          }"
        >
          <div class="prototype-canvas__cluster-label">{{ initiative.label }}</div>
          <div class="prototype-canvas__cluster-meta">
            {{ t("pages.missionControlPrototype.toolbar.initiativeNodes", { count: initiative.nodeCount }) }}
          </div>
        </div>

        <MissionControlPrototypeNodeCard
          v-for="node in nodes"
          :key="node.nodeId"
          :node="node"
          @select="emit('selectNode', node.nodeId)"
        />
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";

import MissionControlPrototypeNodeCard from "./MissionControlPrototypeNodeCard.vue";
import type {
  MissionCanvasNodeView,
  MissionCanvasRelationView,
  MissionCanvasViewport,
  MissionInitiativeView,
} from "./types";

const props = defineProps<{
  initiatives: MissionInitiativeView[];
  nodes: MissionCanvasNodeView[];
  relations: MissionCanvasRelationView[];
  viewport: MissionCanvasViewport;
}>();

const emit = defineEmits<{
  selectNode: [nodeId: string];
}>();

const { t } = useI18n({ useScope: "global" });

const stageStyle = computed(() => ({
  width: `${props.viewport.canvasWidth}px`,
  height: `${props.viewport.canvasHeight}px`,
  transform: `translate(${props.viewport.panX}px, ${props.viewport.panY}px) scale(${props.viewport.zoomLevel})`,
  transformOrigin: "top left",
}));
</script>

<style scoped>
.prototype-canvas {
  position: relative;
  min-height: 100%;
  overflow: hidden;
  background:
    linear-gradient(180deg, rgba(248, 250, 252, 0.92), rgba(255, 255, 255, 0.98)),
    radial-gradient(circle at top left, rgba(217, 119, 6, 0.08), transparent 32%),
    radial-gradient(circle at bottom right, rgba(13, 148, 136, 0.08), transparent 28%);
}

.prototype-canvas__viewport {
  position: relative;
  width: 100%;
  min-height: 100%;
  overflow: auto;
  padding: 28px;
  background-image:
    linear-gradient(rgba(148, 163, 184, 0.1) 1px, transparent 1px),
    linear-gradient(90deg, rgba(148, 163, 184, 0.1) 1px, transparent 1px);
  background-size: 32px 32px;
}

.prototype-canvas__stage {
  position: relative;
  transition: transform 0.2s ease;
}

.prototype-canvas__relations {
  position: absolute;
  inset: 0;
  overflow: visible;
  pointer-events: none;
}

.prototype-canvas__edge {
  fill: none;
  stroke: rgba(71, 85, 105, 0.42);
  stroke-width: 2.5px;
  stroke-linecap: round;
  stroke-dasharray: 0;
}

.prototype-canvas__edge--highlighted {
  stroke: rgba(14, 116, 144, 0.94);
  stroke-width: 3.4px;
}

.prototype-canvas__edge--dimmed {
  stroke: rgba(148, 163, 184, 0.28);
}

.prototype-canvas__edge-label {
  fill: rgb(71, 85, 105);
  font-size: 0.8rem;
  font-weight: 700;
}

.prototype-canvas__edge-label--dimmed {
  fill: rgba(148, 163, 184, 0.82);
}

.prototype-canvas__cluster {
  position: absolute;
  border-radius: 38px;
  border: 1px dashed rgba(148, 163, 184, 0.5);
  background: rgba(255, 255, 255, 0.54);
}

.prototype-canvas__cluster::before {
  content: "";
  position: absolute;
  inset: 10px;
  border-radius: 28px;
  border: 1px solid rgba(255, 255, 255, 0.7);
  pointer-events: none;
}

.prototype-canvas__cluster--dimmed {
  opacity: 0.48;
}

.prototype-canvas__cluster--amber {
  box-shadow: inset 0 0 0 1px rgba(217, 119, 6, 0.08);
}

.prototype-canvas__cluster--teal {
  box-shadow: inset 0 0 0 1px rgba(13, 148, 136, 0.08);
}

.prototype-canvas__cluster--rose {
  box-shadow: inset 0 0 0 1px rgba(225, 29, 72, 0.08);
}

.prototype-canvas__cluster--lime {
  box-shadow: inset 0 0 0 1px rgba(101, 163, 13, 0.08);
}

.prototype-canvas__cluster--slate {
  box-shadow: inset 0 0 0 1px rgba(71, 85, 105, 0.08);
}

.prototype-canvas__cluster-label {
  position: absolute;
  top: 16px;
  left: 18px;
  padding: 6px 12px;
  border-radius: 999px;
  background: rgba(255, 255, 255, 0.86);
  font-size: 0.82rem;
  font-weight: 800;
  letter-spacing: 0.03em;
  color: rgb(30, 41, 59);
}

.prototype-canvas__cluster-meta {
  position: absolute;
  top: 52px;
  left: 20px;
  font-size: 0.76rem;
  color: rgb(100, 116, 139);
}
</style>
