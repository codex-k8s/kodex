<script setup lang="ts">
import { Handle, Position, type NodeProps } from "@vue-flow/core";
import { Bot, PlugZap, UserRoundCheck, Workflow } from "@lucide/vue";
import type { Component } from "vue";
import { useI18n } from "vue-i18n";

import type { RunGraphNodeData } from "@/features/runs/run-graph-flow";
import type { RunNode } from "@/shared/api/generated/openapi/types.gen";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

defineProps<NodeProps<RunGraphNodeData>>();
const emit = defineEmits<{ select: [node: RunNode] }>();
const { t } = useI18n();

function compactDisplayName(displayName: string): string {
  const characters = Array.from(displayName);
  if (characters.length <= 44) return displayName;
  return `${characters.slice(0, 27).join("")}…${characters.slice(-14).join("")}`;
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
</script>

<template>
  <article
    class="run-node"
    :class="[
      `run-node--${data.node.state.toLowerCase()}`,
      `run-node--${data.surface}`,
      {
        'run-node--future': data.future,
        'run-node--active': data.active,
        'run-node--selected': data.selected,
      },
    ]"
    :data-node-ref="data.node.ref"
    :data-node-type="data.node.type"
    :data-node-surface="data.surface"
    :data-node-future="data.future || undefined"
    role="button"
    tabindex="0"
    :aria-label="data.accessibleLabel"
    :aria-pressed="data.selected"
    :aria-busy="data.active || undefined"
    @keydown.enter.stop.prevent="emit('select', data.node)"
    @keydown.space.stop.prevent="emit('select', data.node)"
  >
    <Handle
      id="target"
      class="run-node__handle"
      type="target"
      :position="Position.Left"
      aria-hidden="true"
    />
    <span class="run-node__heading">
      <component
        :is="nodeIcon(data.node.type)"
        class="run-node__type"
        :size="16"
      />
      <span class="run-node__kind">
        {{
          data.surface === "session"
            ? t("runs.sessionNode")
            : t("runs.controlNode")
        }}
      </span>
      <StatusBadge :state="data.node.state" />
    </span>
    <strong class="run-node__title" :title="data.node.displayName">
      {{ compactDisplayName(data.node.displayName) }}
    </strong>
    <span class="run-node__role" :title="data.node.role">
      {{ data.node.role || t(`runs.nodeTypes.${data.node.type}`) }}
    </span>
    <span
      class="run-node__progress"
      :title="data.node.progressSummary || data.node.inputSummary"
    >
      {{
        data.node.progressSummary ||
        data.node.inputSummary ||
        t("runs.waitingForActivity")
      }}
    </span>
    <span v-if="data.active" class="run-node__activity"> <i /><i /><i /> </span>
    <Handle
      id="source"
      class="run-node__handle"
      type="source"
      :position="Position.Right"
      aria-hidden="true"
    />
  </article>
</template>

<style scoped>
.run-node {
  position: relative;
  display: grid;
  width: 100%;
  height: 100%;
  grid-template-rows: auto auto auto minmax(0, 1fr);
  align-content: stretch;
  gap: 5px;
  padding: 12px;
  overflow: hidden;
  border: 1px solid var(--border-strong);
  border-left-width: 4px;
  border-radius: 8px;
  background: var(--surface);
  box-shadow: 0 3px 12px rgba(16, 22, 30, 0.07);
  color: var(--text);
  text-align: left;
}
.run-node--running {
  border-left-color: var(--accent);
}
.run-node--waiting,
.run-node--queued {
  border-left-color: var(--warning);
}
.run-node--succeeded {
  border-left-color: var(--success);
}
.run-node--failed,
.run-node--cancelled {
  border-left-color: var(--danger);
}
.run-node--session {
  border-left-width: 3px;
}
.run-node--control {
  border-style: dashed;
}
.run-node--skipped {
  border-left-color: var(--subtle);
}
.run-node--future {
  border-style: dashed;
  border-left-width: 2px;
  background: color-mix(in srgb, var(--panel) 82%, transparent);
  box-shadow: none;
  opacity: 0.7;
}
.run-node--active::after {
  position: absolute;
  inset: 3px;
  border: 2px solid color-mix(in srgb, var(--accent) 35%, transparent);
  border-radius: 6px;
  pointer-events: none;
  content: "";
  animation: run-node-pulse 1.8s ease-in-out infinite;
}
.run-node--selected {
  border-color: var(--accent);
  box-shadow: 0 0 0 3px color-mix(in srgb, var(--accent) 18%, transparent);
  opacity: 1;
}
.run-node__heading {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr) auto;
  align-items: center;
  gap: 7px;
}
.run-node__kind,
.run-node__title,
.run-node__role,
.run-node__progress {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.run-node__kind {
  color: var(--subtle);
  font-size: 0.67rem;
  font-weight: 700;
  letter-spacing: 0;
  text-transform: uppercase;
}
.run-node__title {
  display: -webkit-box;
  overflow: hidden;
  line-height: 1.25;
  white-space: normal;
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 2;
}
.run-node__type {
  flex: none;
  color: var(--accent);
}
.run-node__role,
.run-node__progress {
  color: var(--muted);
  font-size: 0.82rem;
}
.run-node__progress {
  color: var(--text-secondary);
}
.run-node__activity {
  position: absolute;
  right: 10px;
  bottom: 8px;
  display: flex;
  height: 10px;
  align-items: end;
  gap: 2px;
}
.run-node__activity i {
  display: block;
  width: 3px;
  height: 4px;
  border-radius: 2px 2px 0 0;
  background: var(--accent);
  animation: run-node-activity 1.1s ease-in-out infinite;
}
.run-node__activity i:nth-child(2) {
  animation-delay: 0.16s;
}
.run-node__activity i:nth-child(3) {
  animation-delay: 0.32s;
}
.run-node__handle {
  width: 1px;
  height: 1px;
  border: 0;
  background: transparent;
  opacity: 0;
  pointer-events: none;
}
@keyframes run-node-pulse {
  0%,
  100% {
    opacity: 0.35;
  }
  50% {
    opacity: 1;
  }
}
@keyframes run-node-activity {
  0%,
  100% {
    height: 3px;
    opacity: 0.45;
  }
  50% {
    height: 10px;
    opacity: 1;
  }
}
@media (prefers-reduced-motion: reduce) {
  .run-node--active::after,
  .run-node__activity i {
    animation: none;
    opacity: 0.75;
  }
}
</style>
