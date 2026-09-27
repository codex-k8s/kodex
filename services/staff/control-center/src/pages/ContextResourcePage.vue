<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import ContextEditor from "@/features/context-resources/ContextEditor.vue";
import PageFrame from "@/shared/ui/PageFrame.vue";
const route = useRoute();
const router = useRouter();
const resourceName = ref("");
watch(
  () => route.fullPath,
  () => {
    resourceName.value = "";
  },
  { flush: "sync" },
);
const kind = computed(() =>
  route.params.kind === "skills" || route.params.kind === "memory"
    ? route.params.kind
    : undefined,
);
const reference = computed(() =>
  typeof route.params.resourceRef === "string" &&
  route.params.resourceRef !== "new"
    ? route.params.resourceRef
    : undefined,
);
const titleKey = computed(() => {
  if (!kind.value) return "errors.NOT_FOUND";
  if (kind.value === "skills")
    return reference.value
      ? "contextResources.skill"
      : "contextResources.newSkill";
  return reference.value
    ? "contextResources.memoryEntry"
    : "contextResources.newMemory";
});
const projectRef = computed(() =>
  typeof route.params.projectRef === "string"
    ? route.params.projectRef
    : typeof route.query.projectRef === "string"
      ? route.query.projectRef
      : undefined,
);
const agentRef = computed(() =>
  typeof route.query.agentRef === "string" ? route.query.agentRef : undefined,
);
function created(ref: string, projectRef: string): void {
  void router.replace({
    name: "project-context-resource",
    params: { kind: kind.value, resourceRef: ref, projectRef },
  });
}
</script>
<template>
  <PageFrame :title="reference && resourceName ? resourceName : $t(titleKey)"
    ><ContextEditor
      v-if="kind"
      :key="route.fullPath"
      :kind="kind"
      :resource-ref="reference"
      :project-ref="projectRef"
      :agent-ref="agentRef"
      @created="created"
      @named="resourceName = $event"
  /></PageFrame>
</template>
