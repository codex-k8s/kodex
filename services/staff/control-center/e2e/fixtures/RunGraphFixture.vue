<script setup lang="ts">
import { computed, ref } from "vue";
import RunGraphCanvas from "../../src/features/runs/RunGraphCanvas.vue";
import type {
  RunNode,
  RunEdge,
} from "../../src/shared/api/generated/openapi/types.gen";

const large = new URLSearchParams(location.search).has("large");
const nodes = ref<RunNode[]>([]);
const edges = ref<RunEdge[]>([]);
const selectedRef = ref("coordinator_0");
function addNode(ref: string, parent?: string, coordinator = false): RunNode {
  const index = nodes.value.length;
  const createdAt = new Date(
    Date.UTC(2026, 0, 1, 0, 0, index * 2),
  ).toISOString();
  const node: RunNode = {
    ref,
    runRef: "run_example",
    type: "AGENT_EXECUTION",
    state: "SUCCEEDED",
    displayName: coordinator ? "Координатор" : `Исполнитель ${String(index)}`,
    agentRef: coordinator ? "agent_coordinator" : `agent_${String(index)}`,
    parentNodeRef: parent,
    createdAt,
    startedAt: createdAt,
    finishedAt: new Date(Date.parse(createdAt) + 1000).toISOString(),
    attempt: 1,
    artifactRefs: [],
    childRunRefs: [],
    nextActions: [],
  };
  nodes.value.push(node);
  return node;
}
function link(
  source: string,
  target: string,
  type: RunEdge["type"] = "DELEGATED_TO",
) {
  edges.value.push({
    ref: `edge_${String(edges.value.length)}`,
    runRef: "run_example",
    sourceNodeRef: source,
    targetNodeRef: target,
    type,
    label: "",
  });
}
const history = addNode("history_root");
history.type = "ROOT_PROCESS";
history.displayName = "Предыдущий запуск";
const root = addNode("root");
root.type = "ROOT_PROCESS";
root.displayName = "Текущий запуск";
link(history.ref, root.ref, "CONTINUES");
let previous = addNode("coordinator_0", root.ref, true);
link(root.ref, previous.ref);
for (let phase = 1; phase <= (large ? 20 : 2); phase++) {
  for (let branch = 0; branch < (large ? 3 : 1); branch++) {
    const child = addNode(
      `child_${String(phase)}_${String(branch)}`,
      previous.ref,
    );
    link(previous.ref, child.ref);
    if (large && branch === 1) {
      const nested = addNode(`nested_${String(phase)}`, child.ref);
      link(child.ref, nested.ref);
      link(nested.ref, child.ref, "CALLBACK_TO");
    }
    link(child.ref, previous.ref, "CALLBACK_TO");
  }
  const next = addNode(`coordinator_${String(phase)}`, previous.ref, true);
  link(previous.ref, next.ref, "CONTINUES");
  previous = next;
}
const active = computed(() =>
  nodes.value.filter((n) => n.state === "RUNNING").map((n) => n.ref),
);
function advance() {
  const next = addNode("live_child", previous.ref);
  next.state = "RUNNING";
  link(previous.ref, next.ref);
  selectedRef.value = next.ref;
}
</script>
<template>
  <main>
    <header>
      <button
        @click="advance"
        :disabled="nodes.some((n) => n.ref === 'live_child')"
      >
        Добавить активную ветку</button
      ><output data-testid="selection">{{ selectedRef }}</output>
    </header>
    <RunGraphCanvas
      :nodes="nodes"
      :edges="edges"
      :selected-ref="selectedRef"
      :active-node-refs="active"
      compact
      @select="selectedRef = $event.ref"
      @details="selectedRef = $event.ref"
    />
  </main>
</template>
<style scoped>
main {
  height: 100dvh;
  display: grid;
  grid-template-rows: 48px minmax(0, 1fr);
}
header {
  display: flex;
  gap: 16px;
  align-items: center;
  padding: 8px;
}
</style>
