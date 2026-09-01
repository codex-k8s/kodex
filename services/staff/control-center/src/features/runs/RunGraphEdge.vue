<script setup lang="ts">
import { BaseEdge, getBezierPath, type EdgeProps } from "@vue-flow/core";
import { computed } from "vue";

import type { RunGraphEdgeData } from "@/features/runs/run-graph-flow";

const props = defineProps<EdgeProps<RunGraphEdgeData>>();
const path = computed(
  () =>
    getBezierPath({
      sourceX: props.sourceX,
      sourceY: props.sourceY,
      sourcePosition: props.sourcePosition,
      targetX: props.targetX,
      targetY: props.targetY,
      targetPosition: props.targetPosition,
    })[0],
);
const style = computed(() => ({
  stroke: props.data.color,
  strokeWidth: props.data.strokeWidth,
  strokeDasharray: props.data.dasharray,
}));
</script>

<template>
  <BaseEdge
    :id="id"
    :data-edge-ref="data.edge.ref"
    :data-edge-type="data.edge.type"
    :path="path"
    :marker-end="markerEnd"
    :style="style"
    :interaction-width="0"
  />
</template>
