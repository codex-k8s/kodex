<template>
  <div v-if="initiative && workflow" class="mission-workspace">
    <section class="mission-workspace__hero">
      <div class="mission-workspace__eyebrow">Инициатива</div>
      <h2 class="mission-workspace__title">{{ initiative.title }}</h2>
      <p class="mission-workspace__summary">{{ initiative.summary }}</p>

      <div class="mission-workspace__chips">
        <VChip size="small" variant="tonal" color="primary">{{ workflow.title }}</VChip>
        <VChip size="small" variant="tonal" :color="toneColor(initiative.attentionTone)">{{ initiative.attentionLabel }}</VChip>
      </div>

      <div class="mission-workspace__metrics">
        <article class="mission-workspace__metric">
          <span>Следующий шаг</span>
          <strong>{{ initiative.nextAction }}</strong>
        </article>
        <article class="mission-workspace__metric">
          <span>Текущий статус</span>
          <strong>{{ initiative.statusLabel }}</strong>
        </article>
        <article class="mission-workspace__metric">
          <span>Исполнений за инициативой</span>
          <strong>{{ initiative.runSummary.total }}</strong>
        </article>
      </div>
    </section>

    <div class="mission-workspace__modebar">
      <VBtnToggle divided mandatory :model-value="workspaceView" @update:model-value="onUpdateView">
        <VBtn value="overview">Обзор</VBtn>
        <VBtn value="flow">Поток</VBtn>
        <VBtn value="artifacts">Артефакты</VBtn>
        <VBtn value="activity">Активность</VBtn>
      </VBtnToggle>

      <VSpacer />

      <VBtn variant="text" prepend-icon="mdi-shape-outline" @click="$emit('open-studio')">Редактор workflow</VBtn>
      <VBtn variant="text" prepend-icon="mdi-timeline-outline" @click="$emit('open-executions')">Исполнения</VBtn>
    </div>

    <section v-if="workspaceView === 'overview'" class="mission-workspace__overview">
      <div class="mission-workspace__stage-ribbon">
        <article
          v-for="stage in stageViews"
          :key="stage.stageKey"
          class="mission-workspace__stage-pill"
          :class="`mission-workspace__stage-pill--${stage.status}`"
        >
          <div class="mission-workspace__stage-pill-title">{{ stage.label }}</div>
          <div class="mission-workspace__stage-pill-summary">{{ stage.summary }}</div>
        </article>
      </div>

      <div class="mission-workspace__overview-grid">
        <section class="mission-workspace__overview-card">
          <div class="mission-workspace__section-title">Этапы и выходы</div>
          <div class="mission-workspace__stage-list">
            <article v-for="stage in stageViews" :key="stage.stageKey" class="mission-workspace__stage-row">
              <div>
                <div class="mission-workspace__stage-row-title">{{ stage.label }}</div>
                <div class="mission-workspace__stage-row-summary">{{ stage.exitLabel }}</div>
              </div>
              <VChip size="small" :color="statusColor(stage.status)" variant="tonal">{{ statusLabel(stage.status) }}</VChip>
            </article>
          </div>
        </section>

        <section class="mission-workspace__overview-card">
          <div class="mission-workspace__section-title">Артефакты в работе</div>
          <div class="mission-workspace__artifact-list">
            <button
              v-for="artifact in artifactViews"
              :key="artifact.artifactId"
              type="button"
              class="mission-workspace__artifact-card"
              :class="{ 'mission-workspace__artifact-card--selected': artifact.selected }"
              @click="$emit('select-artifact', artifact.artifactId)"
            >
              <div class="mission-workspace__artifact-topline">
                <VChip size="x-small" variant="outlined">{{ kindLabel(artifact.kind) }}</VChip>
                <VChip size="x-small" :color="artifactColor(artifact.status)" variant="tonal">
                  {{ artifactStatusLabel(artifact.status) }}
                </VChip>
              </div>
              <div class="mission-workspace__artifact-title">{{ artifact.title }}</div>
              <div class="mission-workspace__artifact-summary">{{ artifact.summary }}</div>
            </button>
          </div>
        </section>
      </div>
    </section>

    <section v-else-if="workspaceView === 'flow'" class="mission-workspace__flow">
      <div ref="flowCanvasRef" class="mission-workspace__flow-canvas">
        <div class="mission-workspace__float mission-workspace__float--top-left">
          <div class="mission-workspace__float-title">Карта этапов</div>
          <div class="mission-workspace__float-copy">
            Этапы можно двигать. Связи остаются привязанными к нодам, а executions по-прежнему скрыты за артефактами.
          </div>
        </div>

        <div class="mission-workspace__float mission-workspace__float--top-right">
          <VSwitch
            v-model="showRunSummary"
            density="compact"
            color="primary"
            inset
            hide-details
            label="Показывать summary executions"
          />
        </div>

        <svg class="mission-workspace__flow-svg" :viewBox="`0 0 ${flowCanvasWidth} ${flowCanvasHeight}`" preserveAspectRatio="xMinYMin meet">
          <path
            v-for="relation in flowRelations"
            :key="relation.relationId"
            :d="relationPath(relation.sourceNodeId, relation.targetNodeId)"
            class="mission-workspace__flow-path"
          />
        </svg>

        <article
          v-for="node in localFlowNodes"
          :key="node.nodeId"
          class="mission-workspace__flow-node"
          :class="`mission-workspace__flow-node--${node.tone}`"
          :style="{ transform: `translate(${node.layoutX}px, ${node.layoutY}px)` }"
        >
          <div class="mission-workspace__flow-node-head">
            <div class="mission-workspace__flow-node-title">{{ node.title }}</div>
            <button
              type="button"
              class="mission-workspace__drag-handle"
              aria-label="Перетащить этап"
              @pointerdown.prevent="startFlowDrag($event, node.nodeId)"
            >
              <VIcon icon="mdi-drag" size="16" />
            </button>
          </div>

          <div class="mission-workspace__flow-node-summary">{{ node.summary }}</div>
          <div class="mission-workspace__flow-node-status">{{ node.statusLabel }}</div>

          <div class="mission-workspace__flow-node-artifacts">
            <button
              v-for="artifact in artifactsForNode(node.artifactIds)"
              :key="artifact.artifactId"
              type="button"
              class="mission-workspace__flow-artifact"
              @click="$emit('select-artifact', artifact.artifactId)"
            >
              <span>{{ kindLabel(artifact.kind) }}</span>
              <strong>{{ artifact.title }}</strong>
              <small v-if="showRunSummary">Исполнений: {{ artifact.runSummary.total }}</small>
            </button>
          </div>
        </article>

        <aside v-if="selectedArtifact" class="mission-workspace__flow-inspector">
          <div class="mission-workspace__float-title">{{ selectedArtifact.title }}</div>
          <div class="mission-workspace__float-copy">{{ selectedArtifact.summary }}</div>
          <div class="mission-workspace__inspector-meta">
            <span>{{ kindLabel(selectedArtifact.kind) }}</span>
            <span>{{ artifactStatusLabel(selectedArtifact.status) }}</span>
            <span>{{ selectedArtifact.ownerLabel }}</span>
          </div>
          <VBtn block variant="tonal" prepend-icon="mdi-timeline-outline" @click="$emit('open-executions')">
            Открыть executions
          </VBtn>
        </aside>
      </div>
    </section>

    <section v-else-if="workspaceView === 'artifacts'" class="mission-workspace__artifacts">
      <article v-for="stage in stageViews" :key="stage.stageKey" class="mission-workspace__artifact-section">
        <div class="mission-workspace__section-title">{{ stage.label }}</div>
        <div class="mission-workspace__artifact-section-copy">{{ stage.summary }}</div>

        <div class="mission-workspace__artifact-list">
          <button
            v-for="artifact in artifactsForStage(stage.stageKey)"
            :key="artifact.artifactId"
            type="button"
            class="mission-workspace__artifact-card"
            :class="{ 'mission-workspace__artifact-card--selected': artifact.selected }"
            @click="$emit('select-artifact', artifact.artifactId)"
          >
            <div class="mission-workspace__artifact-topline">
              <VChip size="x-small" variant="outlined">{{ kindLabel(artifact.kind) }}</VChip>
              <VChip size="x-small" :color="artifactColor(artifact.status)" variant="tonal">
                {{ artifactStatusLabel(artifact.status) }}
              </VChip>
            </div>
            <div class="mission-workspace__artifact-title">{{ artifact.title }}</div>
            <div class="mission-workspace__artifact-summary">{{ artifact.summary }}</div>
            <div class="mission-workspace__artifact-meta">
              <span>{{ artifact.ownerLabel }}</span>
              <span>{{ artifact.updatedAtLabel }}</span>
            </div>
          </button>
        </div>
      </article>
    </section>

    <section v-else class="mission-workspace__activity">
      <article v-for="item in activity" :key="item.itemId" class="mission-workspace__activity-item">
        <div class="mission-workspace__activity-meta">
          <VChip size="x-small" :color="toneColor(item.tone)" variant="tonal">{{ item.actorLabel }}</VChip>
          <span>{{ item.happenedAtLabel }}</span>
        </div>
        <div class="mission-workspace__activity-title">{{ item.title }}</div>
        <div class="mission-workspace__activity-summary">{{ item.summary }}</div>
      </article>
    </section>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from "vue";

import {
  missionArtifactKindLabel,
  missionArtifactStatusColor,
  missionAttentionToneColor,
} from "./presenters";
import type {
  MissionActivityItem,
  MissionArtifactStatus,
  MissionCanvasNode,
  MissionCanvasRelation,
  MissionInitiative,
  MissionInitiativeWorkspaceView,
  MissionWorkspaceArtifactView,
  MissionWorkspaceStageView,
  MissionWorkflowStageStatus,
  MissionWorkflowTemplate,
} from "./types";

const props = defineProps<{
  initiative: MissionInitiative | null;
  workflow: MissionWorkflowTemplate | null;
  workspaceView: MissionInitiativeWorkspaceView;
  stageViews: MissionWorkspaceStageView[];
  artifactViews: MissionWorkspaceArtifactView[];
  selectedArtifact: MissionWorkspaceArtifactView | null;
  activity: MissionActivityItem[];
  flowNodes: MissionCanvasNode[];
  flowRelations: MissionCanvasRelation[];
}>();

const emit = defineEmits<{
  (event: "update:view", nextView: MissionInitiativeWorkspaceView): void;
  (event: "select-artifact", artifactId: string): void;
  (event: "open-executions"): void;
  (event: "open-studio"): void;
}>();

const showRunSummary = ref(false);
const flowCanvasRef = ref<HTMLElement | null>(null);
const localFlowNodes = ref<MissionCanvasNode[]>([]);
const dragState = ref<null | { nodeId: string; offsetX: number; offsetY: number }>(null);

const flowNodeById = computed(() => new Map(localFlowNodes.value.map((node) => [node.nodeId, node])));
const flowCanvasWidth = computed(() => Math.max(1420, ...localFlowNodes.value.map((node) => node.layoutX + 620)));
const flowCanvasHeight = computed(() => 620);

watch(
  () => props.flowNodes,
  (nextNodes) => {
    localFlowNodes.value = nextNodes.map((node) => ({ ...node }));
  },
  { immediate: true, deep: true },
);

function onUpdateView(nextValue: string): void {
  if (nextValue === "overview" || nextValue === "flow" || nextValue === "artifacts" || nextValue === "activity") {
    emit("update:view", nextValue);
  }
}

function toneColor(tone: MissionActivityItem["tone"] | MissionInitiative["attentionTone"]): string {
  return missionAttentionToneColor(tone);
}

function artifactColor(status: MissionArtifactStatus): string {
  return missionArtifactStatusColor(status);
}

function kindLabel(kind: MissionWorkspaceArtifactView["kind"]): string {
  return missionArtifactKindLabel(kind);
}

function statusColor(status: MissionWorkflowStageStatus): string {
  switch (status) {
    case "done":
      return "success";
    case "blocked":
      return "error";
    case "active":
    case "attention":
      return "warning";
    case "pending":
      return "info";
  }
}

function statusLabel(status: MissionWorkflowStageStatus): string {
  switch (status) {
    case "done":
      return "Готово";
    case "blocked":
      return "Блокер";
    case "active":
      return "В работе";
    case "attention":
      return "Нужно решение";
    case "pending":
      return "Дальше по очереди";
  }
}

function artifactStatusLabel(status: MissionArtifactStatus): string {
  switch (status) {
    case "draft":
      return "Черновик";
    case "active":
      return "В работе";
    case "review":
      return "На review";
    case "blocked":
      return "Блокер";
    case "done":
      return "Готово";
  }
}

function artifactsForStage(stageKey: MissionWorkspaceStageView["stageKey"]): MissionWorkspaceArtifactView[] {
  return props.artifactViews.filter((artifact) => artifact.stageKey === stageKey);
}

function artifactsForNode(artifactIds: string[]): MissionWorkspaceArtifactView[] {
  const allowed = new Set(artifactIds);
  return props.artifactViews.filter((artifact) => allowed.has(artifact.artifactId));
}

function relationPath(sourceNodeId: string, targetNodeId: string): string {
  const source = flowNodeById.value.get(sourceNodeId);
  const target = flowNodeById.value.get(targetNodeId);
  if (!source || !target) {
    return "";
  }

  const startX = source.layoutX + 212;
  const startY = source.layoutY + 84;
  const endX = target.layoutX + 12;
  const endY = target.layoutY + 84;
  const controlOffset = Math.max(64, Math.abs(endX - startX) * 0.28);
  return `M ${startX} ${startY} C ${startX + controlOffset} ${startY}, ${endX - controlOffset} ${endY}, ${endX} ${endY}`;
}

function startFlowDrag(event: PointerEvent, nodeId: string): void {
  const node = flowNodeById.value.get(nodeId);
  if (!node) {
    return;
  }

  dragState.value = {
    nodeId,
    offsetX: pointerXInCanvas(event) - node.layoutX,
    offsetY: pointerYInCanvas(event) - node.layoutY,
  };
  window.addEventListener("pointermove", onFlowDragMove);
  window.addEventListener("pointerup", stopFlowDrag);
}

function onFlowDragMove(event: PointerEvent): void {
  if (!dragState.value) {
    return;
  }

  const targetNode = localFlowNodes.value.find((node) => node.nodeId === dragState.value?.nodeId);
  if (!targetNode) {
    return;
  }

  targetNode.layoutX = Math.max(
    32,
    Math.min(flowCanvasWidth.value - 560, pointerXInCanvas(event) - dragState.value.offsetX),
  );
  targetNode.layoutY = Math.max(
    110,
    Math.min(flowCanvasHeight.value - 220, pointerYInCanvas(event) - dragState.value.offsetY),
  );
}

function stopFlowDrag(): void {
  dragState.value = null;
  window.removeEventListener("pointermove", onFlowDragMove);
  window.removeEventListener("pointerup", stopFlowDrag);
}

function pointerXInCanvas(event: PointerEvent): number {
  const rect = flowCanvasRef.value?.getBoundingClientRect();
  return event.clientX - (rect?.left ?? 0) + (flowCanvasRef.value?.scrollLeft ?? 0);
}

function pointerYInCanvas(event: PointerEvent): number {
  const rect = flowCanvasRef.value?.getBoundingClientRect();
  return event.clientY - (rect?.top ?? 0) + (flowCanvasRef.value?.scrollTop ?? 0);
}

onBeforeUnmount(() => {
  stopFlowDrag();
});
</script>

<style scoped>
.mission-workspace {
  display: grid;
  gap: 18px;
}

.mission-workspace__hero {
  display: grid;
  gap: 16px;
  padding: 22px;
  border-radius: 28px;
  background:
    radial-gradient(circle at top right, rgba(124, 193, 255, 0.2), transparent 32%),
    linear-gradient(140deg, rgba(251, 248, 242, 0.96), rgba(255, 255, 255, 0.94));
  border: 1px solid rgba(220, 225, 232, 0.88);
  box-shadow: 0 20px 40px rgba(26, 29, 35, 0.05);
}

.mission-workspace__eyebrow {
  font-size: 0.78rem;
  text-transform: uppercase;
  letter-spacing: 0.08em;
  font-weight: 700;
  color: rgb(79, 91, 112);
}

.mission-workspace__title {
  margin: 0;
  font-size: 1.6rem;
  line-height: 1.2;
  color: rgb(30, 35, 42);
}

.mission-workspace__summary {
  margin: 0;
  font-size: 0.96rem;
  line-height: 1.55;
  color: rgb(84, 94, 109);
}

.mission-workspace__chips,
.mission-workspace__metrics {
  display: flex;
  gap: 10px;
  flex-wrap: wrap;
}

.mission-workspace__metric {
  min-width: 240px;
  display: grid;
  gap: 6px;
  padding: 14px 16px;
  border-radius: 20px;
  background: rgba(255, 255, 255, 0.92);
  border: 1px solid rgba(224, 228, 235, 0.9);
}

.mission-workspace__metric span {
  font-size: 0.8rem;
  color: rgb(102, 111, 125);
}

.mission-workspace__metric strong {
  font-size: 0.96rem;
  line-height: 1.45;
  color: rgb(32, 37, 45);
}

.mission-workspace__modebar {
  display: flex;
  gap: 12px;
  align-items: center;
  flex-wrap: wrap;
}

.mission-workspace__overview,
.mission-workspace__artifacts,
.mission-workspace__activity {
  display: grid;
  gap: 18px;
}

.mission-workspace__stage-ribbon {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
  gap: 12px;
}

.mission-workspace__stage-pill,
.mission-workspace__overview-card,
.mission-workspace__artifact-section,
.mission-workspace__activity-item {
  padding: 16px;
  border-radius: 22px;
  border: 1px solid rgba(223, 227, 233, 0.9);
  background: rgba(255, 255, 255, 0.92);
  box-shadow: 0 14px 28px rgba(26, 29, 35, 0.04);
}

.mission-workspace__stage-pill--done {
  background: linear-gradient(180deg, rgba(238, 251, 242, 0.96), rgba(255, 255, 255, 0.92));
}

.mission-workspace__stage-pill--active,
.mission-workspace__stage-pill--attention {
  background: linear-gradient(180deg, rgba(255, 247, 227, 0.96), rgba(255, 255, 255, 0.92));
}

.mission-workspace__stage-pill--blocked {
  background: linear-gradient(180deg, rgba(255, 238, 236, 0.96), rgba(255, 255, 255, 0.92));
}

.mission-workspace__stage-pill-title,
.mission-workspace__section-title,
.mission-workspace__activity-title {
  font-size: 1rem;
  font-weight: 700;
  color: rgb(31, 36, 44);
}

.mission-workspace__stage-pill-summary,
.mission-workspace__activity-summary,
.mission-workspace__artifact-section-copy {
  margin-top: 8px;
  font-size: 0.9rem;
  line-height: 1.5;
  color: rgb(92, 101, 115);
}

.mission-workspace__overview-grid {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
  gap: 18px;
}

.mission-workspace__stage-list,
.mission-workspace__artifact-list {
  display: grid;
  gap: 12px;
  margin-top: 14px;
}

.mission-workspace__stage-row {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  align-items: flex-start;
  padding: 12px 14px;
  border-radius: 16px;
  background: rgba(248, 250, 252, 0.92);
}

.mission-workspace__stage-row-title,
.mission-workspace__artifact-title {
  font-size: 0.95rem;
  font-weight: 700;
  line-height: 1.4;
  color: rgb(31, 36, 43);
}

.mission-workspace__stage-row-summary,
.mission-workspace__artifact-summary {
  margin-top: 4px;
  font-size: 0.87rem;
  line-height: 1.5;
  color: rgb(97, 107, 122);
}

.mission-workspace__artifact-card {
  display: grid;
  gap: 10px;
  width: 100%;
  padding: 14px;
  border-radius: 18px;
  border: 1px solid rgba(223, 227, 233, 0.94);
  background: white;
  text-align: left;
}

.mission-workspace__artifact-card--selected {
  border-color: rgba(86, 132, 255, 0.7);
  box-shadow: 0 0 0 3px rgba(86, 132, 255, 0.12);
}

.mission-workspace__artifact-topline,
.mission-workspace__artifact-meta,
.mission-workspace__activity-meta {
  display: flex;
  gap: 8px;
  align-items: center;
  flex-wrap: wrap;
  font-size: 0.8rem;
  color: rgb(104, 113, 126);
}

.mission-workspace__flow {
  min-height: 640px;
}

.mission-workspace__flow-canvas {
  position: relative;
  min-height: 620px;
  overflow: auto;
  padding: 24px 340px 40px 24px;
  border-radius: 30px;
  background:
    linear-gradient(rgba(220, 225, 232, 0.34) 1px, transparent 1px),
    linear-gradient(90deg, rgba(220, 225, 232, 0.34) 1px, transparent 1px),
    linear-gradient(135deg, rgb(248, 249, 251), rgb(242, 244, 247));
  background-size: 28px 28px, 28px 28px, auto;
  border: 1px solid rgba(220, 225, 232, 0.9);
}

.mission-workspace__float {
  position: absolute;
  z-index: 3;
  max-width: 280px;
  padding: 14px 16px;
  border-radius: 20px;
  background: rgba(255, 255, 255, 0.94);
  border: 1px solid rgba(223, 227, 233, 0.92);
  box-shadow: 0 16px 34px rgba(26, 29, 35, 0.08);
  backdrop-filter: blur(12px);
}

.mission-workspace__float--top-left {
  left: 18px;
  top: 18px;
}

.mission-workspace__float--top-right {
  right: 18px;
  top: 18px;
}

.mission-workspace__float-title {
  font-size: 0.95rem;
  font-weight: 700;
  color: rgb(30, 35, 42);
}

.mission-workspace__float-copy {
  margin-top: 6px;
  font-size: 0.84rem;
  line-height: 1.45;
  color: rgb(95, 104, 117);
}

.mission-workspace__flow-svg {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  min-width: 100%;
  min-height: 100%;
  z-index: 1;
  overflow: visible;
}

.mission-workspace__flow-path {
  fill: none;
  stroke: rgba(73, 131, 211, 0.56);
  stroke-width: 3;
  stroke-linecap: round;
}

.mission-workspace__flow-node {
  position: absolute;
  z-index: 2;
  width: 200px;
  display: grid;
  gap: 10px;
  padding: 14px;
  border-radius: 22px;
  border: 1px solid rgba(223, 227, 233, 0.92);
  background: rgba(255, 255, 255, 0.96);
  box-shadow: 0 16px 34px rgba(26, 29, 35, 0.08);
}

.mission-workspace__flow-node--warning {
  border-color: rgba(237, 178, 72, 0.58);
}

.mission-workspace__flow-node--error {
  border-color: rgba(227, 122, 122, 0.58);
}

.mission-workspace__flow-node--success {
  border-color: rgba(109, 196, 141, 0.58);
}

.mission-workspace__flow-node-head {
  display: flex;
  justify-content: space-between;
  gap: 10px;
  align-items: flex-start;
}

.mission-workspace__flow-node-title {
  font-size: 0.98rem;
  font-weight: 700;
  color: rgb(31, 36, 43);
}

.mission-workspace__drag-handle {
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

.mission-workspace__flow-node-summary,
.mission-workspace__flow-node-status {
  font-size: 0.82rem;
  line-height: 1.5;
  color: rgb(96, 105, 118);
}

.mission-workspace__flow-node-artifacts {
  display: grid;
  gap: 8px;
}

.mission-workspace__flow-artifact {
  display: grid;
  gap: 2px;
  padding: 9px 10px;
  border-radius: 14px;
  border: 1px solid rgba(224, 229, 236, 0.92);
  background: rgba(248, 250, 252, 0.9);
  text-align: left;
}

.mission-workspace__flow-artifact span,
.mission-workspace__flow-artifact small {
  font-size: 0.76rem;
  color: rgb(104, 113, 126);
}

.mission-workspace__flow-artifact strong {
  font-size: 0.84rem;
  line-height: 1.35;
  color: rgb(35, 40, 47);
}

.mission-workspace__flow-inspector {
  position: absolute;
  right: 18px;
  top: 96px;
  z-index: 3;
  width: 280px;
  display: grid;
  gap: 12px;
  padding: 16px;
  border-radius: 22px;
  background: rgba(255, 255, 255, 0.95);
  border: 1px solid rgba(223, 227, 233, 0.92);
  box-shadow: 0 20px 40px rgba(26, 29, 35, 0.12);
}

.mission-workspace__inspector-meta {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
  font-size: 0.8rem;
  color: rgb(104, 113, 126);
}

@media (max-width: 1100px) {
  .mission-workspace__overview-grid {
    grid-template-columns: 1fr;
  }

  .mission-workspace__flow-canvas {
    padding-right: 24px;
    padding-top: 110px;
  }

  .mission-workspace__flow-inspector {
    position: static;
    width: 100%;
    margin-top: 480px;
  }
}

@media (max-width: 720px) {
  .mission-workspace__hero,
  .mission-workspace__flow-canvas {
    padding: 16px;
  }

  .mission-workspace__float {
    position: static;
    max-width: none;
    margin-bottom: 12px;
  }
}
</style>
