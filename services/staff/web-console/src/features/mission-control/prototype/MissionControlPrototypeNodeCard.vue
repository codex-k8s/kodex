<template>
  <button
    type="button"
    class="prototype-node"
    :class="[
      `prototype-node--${node.initiativeAccentToken}`,
      { 'prototype-node--selected': node.selected, 'prototype-node--dimmed': node.dimmed },
    ]"
    :style="cardStyle"
    @click="emit('select')"
  >
    <div class="prototype-node__glow" />
    <div class="prototype-node__head">
      <VChip size="x-small" variant="flat" :color="kindTone">
        {{ t(`pages.missionControlPrototype.nodeKinds.${node.nodeKind}`) }}
      </VChip>
      <VChip size="x-small" variant="tonal" :color="stateTone">
        {{ t(`pages.missionControlPrototype.nodeStates.${node.state}`) }}
      </VChip>
    </div>

    <div class="prototype-node__title">{{ node.title }}</div>
    <div class="prototype-node__stage">{{ node.stageLabel }}</div>

    <div class="prototype-node__meta">
      <VChip v-for="meta in node.meta.slice(0, 2)" :key="meta" size="x-small" variant="outlined">
        {{ meta }}
      </VChip>
    </div>

    <div class="prototype-node__badges">
      <VChip
        v-for="badge in node.badges.slice(0, 2)"
        :key="badge"
        size="x-small"
        :color="badgeTone(badge)"
        variant="tonal"
      >
        {{ t(`pages.missionControlPrototype.badges.${badge}`) }}
      </VChip>
    </div>
  </button>
</template>

<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";

import {
  missionControlPrototypeBadgeTone,
  missionControlPrototypeCardHeight,
  missionControlPrototypeCardWidth,
  missionControlPrototypeStateTone,
} from "./presenters";
import type { MissionCanvasNodeBadge, MissionCanvasNodeView } from "./types";

const props = defineProps<{
  node: MissionCanvasNodeView;
}>();

const emit = defineEmits<{
  select: [];
}>();

const { t } = useI18n({ useScope: "global" });

const cardStyle = computed(() => ({
  left: `${props.node.layoutX}px`,
  top: `${props.node.layoutY}px`,
  width: `${missionControlPrototypeCardWidth}px`,
  minHeight: `${missionControlPrototypeCardHeight}px`,
}));

const kindTone = computed(() => {
  switch (props.node.nodeKind) {
    case "Issue":
      return "warning";
    case "PR":
      return "success";
    case "Run":
      return "info";
  }
});

const stateTone = computed(() => missionControlPrototypeStateTone(props.node.state));

function badgeTone(badge: MissionCanvasNodeBadge): string {
  return missionControlPrototypeBadgeTone(badge);
}
</script>

<style scoped>
.prototype-node {
  position: absolute;
  display: flex;
  flex-direction: column;
  gap: 10px;
  padding: 14px;
  border-radius: 22px;
  border: 1px solid rgba(148, 163, 184, 0.18);
  background: rgba(255, 255, 255, 0.92);
  box-shadow: 0 18px 40px rgba(15, 23, 42, 0.12);
  text-align: left;
  transition:
    transform 0.2s ease,
    box-shadow 0.2s ease,
    opacity 0.2s ease,
    border-color 0.2s ease;
  overflow: hidden;
}

.prototype-node:hover {
  transform: translateY(-3px);
  box-shadow: 0 24px 50px rgba(15, 23, 42, 0.16);
}

.prototype-node--selected {
  border-color: rgba(13, 148, 136, 0.42);
  box-shadow: 0 28px 54px rgba(13, 148, 136, 0.18);
}

.prototype-node--dimmed {
  opacity: 0.46;
}

.prototype-node__glow {
  position: absolute;
  inset: 0 auto auto 0;
  width: 100%;
  height: 6px;
}

.prototype-node--amber .prototype-node__glow {
  background: linear-gradient(90deg, rgba(217, 119, 6, 0.96), rgba(245, 158, 11, 0.35));
}

.prototype-node--teal .prototype-node__glow {
  background: linear-gradient(90deg, rgba(13, 148, 136, 0.96), rgba(45, 212, 191, 0.3));
}

.prototype-node--rose .prototype-node__glow {
  background: linear-gradient(90deg, rgba(225, 29, 72, 0.92), rgba(251, 113, 133, 0.28));
}

.prototype-node--lime .prototype-node__glow {
  background: linear-gradient(90deg, rgba(101, 163, 13, 0.92), rgba(163, 230, 53, 0.3));
}

.prototype-node--slate .prototype-node__glow {
  background: linear-gradient(90deg, rgba(71, 85, 105, 0.92), rgba(148, 163, 184, 0.32));
}

.prototype-node__head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  flex-wrap: wrap;
}

.prototype-node__title {
  margin-top: 8px;
  font-size: 0.98rem;
  font-weight: 700;
  line-height: 1.35;
  color: rgb(15, 23, 42);
}

.prototype-node__stage {
  font-size: 0.8rem;
  color: rgb(71, 85, 105);
}

.prototype-node__meta,
.prototype-node__badges {
  display: flex;
  gap: 6px;
  flex-wrap: wrap;
}
</style>
