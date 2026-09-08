<script setup lang="ts">
import { computed, ref } from "vue";
import { useRoute } from "vue-router";
import CodeEditor from "../../src/shared/ui/CodeEditor.vue";
import VfsBrowser from "../../src/features/files/VfsBrowser.vue";
import RunsPage from "../../src/pages/RunsPage.vue";
import AssistantWorkspace from "../../src/features/assistant/components/AssistantWorkspace.vue";
const route = useRoute();
const source = ref("FROM scratch\n");
const projectRef = computed(() =>
  typeof route.params.projectRef === "string"
    ? route.params.projectRef
    : undefined,
);
const context = computed(() => ({
  route: route.path,
  entityKind: projectRef.value ? "PROJECT" : "HOME",
  entityRef: projectRef.value ?? "",
  entityName: "Fixture",
  allowedOperations: [],
}));
</script>
<template>
  <div class="app-shell">
    <header class="topbar">Fixture</header>
    <div class="page-header"><h1>Readonly fixture</h1></div>
    <section
      v-if="route.path.includes('/configurations/')"
      class="configuration-editor"
    >
      <CodeEditor
        v-model="source"
        language="dockerfile"
        label="Source"
        sensitive
      />
    </section>
    <VfsBrowser
      v-else-if="route.path.includes('/files')"
      :project-ref="projectRef"
    />
    <RunsPage v-else-if="route.path.endsWith('/runs')" />
    <AssistantWorkspace v-else :context="context" :project-ref="projectRef" />
  </div>
</template>
<style scoped>
.topbar {
  height: 58px;
  min-height: 58px;
}
.app-shell {
  display: block;
  min-width: 0;
}
.page-header {
  padding: 8px;
}
</style>
