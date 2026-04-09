<template>
  <div v-if="workflow" class="mission-studio">
    <div ref="studioCanvasRef" class="mission-studio__canvas">
      <div class="mission-studio__float mission-studio__float--top-left">
        <div class="mission-studio__eyebrow">Редактор workflow</div>
        <div class="mission-studio__title">{{ workflow.title }}</div>
        <div class="mission-studio__summary">{{ workflow.summary }}</div>
      </div>

      <div class="mission-studio__float mission-studio__float--top-right">
        <div class="mission-studio__toolbar">
          <VSelect
            :model-value="workflow.workflowId"
            :items="workflowOptions"
            item-title="title"
            item-value="workflowId"
            label="Шаблон workflow"
            density="compact"
            variant="outlined"
            hide-details
            @update:model-value="onSelectWorkflow"
          />
        </div>
      </div>

      <aside class="mission-studio__float mission-studio__float--left-panel">
        <div class="mission-studio__panel-title">Библиотека блоков</div>
        <div class="mission-studio__block-list">
          <div class="mission-studio__block">Этап</div>
          <div class="mission-studio__block">Owner gate</div>
          <div class="mission-studio__block">Quality gate</div>
          <div class="mission-studio__block">Follow-up</div>
        </div>
      </aside>

      <aside class="mission-studio__float mission-studio__float--right-panel">
        <div class="mission-studio__panel-title">Инспектор</div>
        <div class="mission-studio__inspector-line">
          <span>Запуск</span>
          <strong>{{ workflow.launchSummary }}</strong>
        </div>
        <div class="mission-studio__inspector-line">
          <span>Голосовая подсказка</span>
          <strong>{{ workflow.voiceHint }}</strong>
        </div>
        <div class="mission-studio__policy-list">
          <div v-for="bullet in workflow.policyBullets" :key="bullet" class="mission-studio__policy-item">
            {{ bullet }}
          </div>
        </div>
      </aside>

      <svg class="mission-studio__svg" :viewBox="`0 0 ${canvasWidth} ${canvasHeight}`" preserveAspectRatio="xMinYMin meet">
        <path
          v-for="relation in relations"
          :key="relation.relationId"
          :d="relationPath(relation.sourceNodeId, relation.targetNodeId)"
          class="mission-studio__path"
        />
      </svg>

      <article
        v-for="node in localNodes"
        :key="node.nodeId"
        class="mission-studio__node"
        :class="`mission-studio__node--${node.kind}`"
        :style="{ transform: `translate(${node.layoutX}px, ${node.layoutY}px)` }"
      >
        <div class="mission-studio__node-head">
          <div class="mission-studio__node-title">{{ node.title }}</div>
          <button
            type="button"
            class="mission-studio__drag-handle"
            aria-label="Перетащить блок"
            @pointerdown.prevent="startDrag($event, node.nodeId)"
          >
            <VIcon icon="mdi-drag" size="16" />
          </button>
        </div>
        <div class="mission-studio__node-summary">{{ node.summary }}</div>
        <div class="mission-studio__node-status">{{ node.statusLabel }}</div>
      </article>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from "vue";

import type { MissionCanvasNode, MissionCanvasRelation, MissionWorkflowOption, MissionWorkflowTemplate } from "./types";

const props = defineProps<{
  workflow: MissionWorkflowTemplate | null;
  workflowOptions: MissionWorkflowOption[];
  nodes: MissionCanvasNode[];
  relations: MissionCanvasRelation[];
}>();

const emit = defineEmits<{
  (event: "select-workflow", workflowId: string): void;
}>();

const studioCanvasRef = ref<HTMLElement | null>(null);
const localNodes = ref<MissionCanvasNode[]>([]);
const dragState = ref<null | { nodeId: string; offsetX: number; offsetY: number }>(null);

const nodeById = computed(() => new Map(localNodes.value.map((node) => [node.nodeId, node])));
const canvasWidth = computed(() => Math.max(1460, ...localNodes.value.map((node) => node.layoutX + 620)));
const canvasHeight = computed(() => 680);

watch(
  () => props.nodes,
  (nextNodes) => {
    localNodes.value = nextNodes.map((node) => ({ ...node }));
  },
  { immediate: true, deep: true },
);

function onSelectWorkflow(nextWorkflowId: string | null): void {
  if (typeof nextWorkflowId === "string" && nextWorkflowId !== "") {
    emit("select-workflow", nextWorkflowId);
  }
}

function relationPath(sourceNodeId: string, targetNodeId: string): string {
  const source = nodeById.value.get(sourceNodeId);
  const target = nodeById.value.get(targetNodeId);
  if (!source || !target) {
    return "";
  }

  const startX = source.layoutX + 192;
  const startY = source.layoutY + 74;
  const endX = target.layoutX + 10;
  const endY = target.layoutY + 74;
  const controlOffset = Math.max(64, Math.abs(endX - startX) * 0.28);
  return `M ${startX} ${startY} C ${startX + controlOffset} ${startY}, ${endX - controlOffset} ${endY}, ${endX} ${endY}`;
}

function startDrag(event: PointerEvent, nodeId: string): void {
  const node = nodeById.value.get(nodeId);
  if (!node) {
    return;
  }

  dragState.value = {
    nodeId,
    offsetX: pointerXInCanvas(event) - node.layoutX,
    offsetY: pointerYInCanvas(event) - node.layoutY,
  };
  window.addEventListener("pointermove", onDragMove);
  window.addEventListener("pointerup", stopDrag);
}

function onDragMove(event: PointerEvent): void {
  if (!dragState.value) {
    return;
  }

  const targetNode = localNodes.value.find((node) => node.nodeId === dragState.value?.nodeId);
  if (!targetNode) {
    return;
  }

  targetNode.layoutX = Math.max(
    36,
    Math.min(canvasWidth.value - 560, pointerXInCanvas(event) - dragState.value.offsetX),
  );
  targetNode.layoutY = Math.max(
    150,
    Math.min(canvasHeight.value - 180, pointerYInCanvas(event) - dragState.value.offsetY),
  );
}

function stopDrag(): void {
  dragState.value = null;
  window.removeEventListener("pointermove", onDragMove);
  window.removeEventListener("pointerup", stopDrag);
}

function pointerXInCanvas(event: PointerEvent): number {
  const rect = studioCanvasRef.value?.getBoundingClientRect();
  return event.clientX - (rect?.left ?? 0) + (studioCanvasRef.value?.scrollLeft ?? 0);
}

function pointerYInCanvas(event: PointerEvent): number {
  const rect = studioCanvasRef.value?.getBoundingClientRect();
  return event.clientY - (rect?.top ?? 0) + (studioCanvasRef.value?.scrollTop ?? 0);
}

onBeforeUnmount(() => {
  stopDrag();
});
</script>

<style scoped>
.mission-studio {
  min-height: 720px;
}

.mission-studio__canvas {
  position: relative;
  min-height: 720px;
  overflow: auto;
  padding: 24px 340px 36px 250px;
  border-radius: 30px;
  background:
    linear-gradient(rgba(208, 213, 221, 0.3) 1px, transparent 1px),
    linear-gradient(90deg, rgba(208, 213, 221, 0.3) 1px, transparent 1px),
    linear-gradient(145deg, rgb(248, 249, 252), rgb(244, 246, 250));
  background-size: 32px 32px, 32px 32px, auto;
  border: 1px solid rgba(220, 225, 232, 0.92);
}

.mission-studio__float {
  position: absolute;
  z-index: 3;
  padding: 16px;
  border-radius: 22px;
  background: rgba(255, 255, 255, 0.94);
  border: 1px solid rgba(223, 227, 233, 0.94);
  box-shadow: 0 18px 38px rgba(26, 29, 35, 0.08);
  backdrop-filter: blur(12px);
}

.mission-studio__float--top-left {
  top: 18px;
  left: 18px;
  max-width: 220px;
}

.mission-studio__float--top-right {
  top: 18px;
  right: 18px;
  width: 280px;
}

.mission-studio__float--left-panel {
  left: 18px;
  top: 146px;
  width: 200px;
}

.mission-studio__float--right-panel {
  right: 18px;
  top: 146px;
  width: 280px;
}

.mission-studio__eyebrow {
  font-size: 0.78rem;
  text-transform: uppercase;
  letter-spacing: 0.08em;
  font-weight: 700;
  color: rgb(109, 88, 28);
}

.mission-studio__title {
  margin-top: 8px;
  font-size: 1.1rem;
  font-weight: 700;
  color: rgb(31, 36, 44);
}

.mission-studio__summary {
  margin-top: 8px;
  font-size: 0.88rem;
  line-height: 1.5;
  color: rgb(91, 100, 114);
}

.mission-studio__toolbar {
  display: grid;
  gap: 10px;
}

.mission-studio__panel-title {
  font-size: 0.94rem;
  font-weight: 700;
  color: rgb(31, 36, 43);
}

.mission-studio__block-list,
.mission-studio__policy-list {
  display: grid;
  gap: 10px;
  margin-top: 14px;
}

.mission-studio__block,
.mission-studio__policy-item {
  padding: 10px 12px;
  border-radius: 16px;
  background: rgba(248, 250, 252, 0.94);
  border: 1px solid rgba(224, 229, 235, 0.92);
  font-size: 0.84rem;
  color: rgb(87, 96, 109);
}

.mission-studio__inspector-line {
  display: grid;
  gap: 4px;
  margin-top: 12px;
}

.mission-studio__inspector-line span {
  font-size: 0.78rem;
  color: rgb(104, 113, 126);
}

.mission-studio__inspector-line strong {
  font-size: 0.88rem;
  line-height: 1.45;
  color: rgb(33, 38, 45);
}

.mission-studio__svg {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  z-index: 1;
}

.mission-studio__path {
  fill: none;
  stroke: rgba(77, 121, 206, 0.58);
  stroke-width: 3;
}

.mission-studio__node {
  position: absolute;
  z-index: 2;
  width: 180px;
  display: grid;
  gap: 10px;
  padding: 14px;
  border-radius: 22px;
  background: rgba(255, 255, 255, 0.96);
  border: 1px solid rgba(223, 227, 233, 0.92);
  box-shadow: 0 16px 32px rgba(26, 29, 35, 0.08);
}

.mission-studio__node--gate {
  background: linear-gradient(180deg, rgba(255, 247, 226, 0.96), rgba(255, 255, 255, 0.92));
}

.mission-studio__node-head {
  display: flex;
  justify-content: space-between;
  gap: 10px;
  align-items: flex-start;
}

.mission-studio__node-title {
  font-size: 0.96rem;
  font-weight: 700;
  color: rgb(31, 36, 43);
}

.mission-studio__drag-handle {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 28px;
  border-radius: 50%;
  border: 1px solid rgba(221, 226, 233, 0.92);
  background: rgba(248, 250, 252, 0.92);
  color: rgb(96, 104, 118);
  cursor: grab;
}

.mission-studio__node-summary,
.mission-studio__node-status {
  font-size: 0.8rem;
  line-height: 1.45;
  color: rgb(95, 104, 117);
}

@media (max-width: 1100px) {
  .mission-studio__canvas {
    padding-right: 24px;
    padding-left: 24px;
    padding-top: 180px;
  }

  .mission-studio__float {
    position: static;
    width: auto;
    max-width: none;
    margin-bottom: 12px;
  }
}
</style>
