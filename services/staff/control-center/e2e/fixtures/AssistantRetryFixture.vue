<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from "vue";
import { useRoute } from "vue-router";
import AssistantWorkspace from "../../src/features/assistant/components/AssistantWorkspace.vue";
import { useAssistantStore } from "../../src/features/assistant/store";
import { usePlatformStore } from "../../src/features/platform/store";
import { useRealtimeStore } from "../../src/features/realtime/store";
import ConfirmDialogHost from "../../src/shared/ui/ConfirmDialogHost.vue";
import { selectProjectRef } from "../../src/shared/project-context";
import type {
  AssistantContextDescriptor,
  AssistantScope,
} from "../../src/shared/api/generated/openapi/types.gen";

const route = useRoute();
const assistant = useAssistantStore(),
  platform = usePlatformStore(),
  realtime = useRealtimeStore();
const scope: AssistantScope =
  new URLSearchParams(location.search).get("scope") === "PROJECT"
    ? "PROJECT"
    : "SYSTEM";
const projectRef = scope === "PROJECT" ? "prj_retry_fixture" : undefined;
const context: AssistantContextDescriptor = {
  route: projectRef ? `/projects/${projectRef}` : "/onboarding",
  entityKind: projectRef ? "PROJECT" : "PLATFORM",
  entityRef: projectRef ?? "",
  entityName: "Проверка retry",
  entityVersion: 1,
  allowedOperations: [],
};
const initialized = ref(false);
const revision = computed(() =>
  [
    platform.assistantRealtimeScopeKey,
    ...Object.values(platform.conversations).map(
      (value) => `${value.ref}:${String(value.version)}`,
    ),
  ].join("|"),
);
watch(revision, () => {
  if (platform.assistantRealtimeScopeKey !== (projectRef ?? "")) return;
  assistant.applyRealtimeSnapshot(
    platform.assistant,
    Object.values(platform.conversations),
    projectRef,
    platform.assistantConversationNextPageToken,
  );
});
onMounted(async () => {
  selectProjectRef(projectRef);
  await platform.loadBootstrap();
  await assistant.load(context, projectRef, true, scope);
  initialized.value = true;
  realtime.openPlatform();
});
onUnmounted(() => realtime.closeAll());
const diagnostic = computed(() =>
  JSON.stringify({
    initialized: initialized.value,
    route: route.fullPath,
    state: realtime.platformState.state,
    runState: realtime.state,
    runLoading: platform.runLoading,
    selectedRef: assistant.selectedRef,
    conversations: assistant.conversations,
    runs: platform.runs,
    artifacts: platform.artifacts,
  }),
);
</script>
<template>
  <main class="assistant-retry-fixture">
    <RouterView />
    <output hidden data-testid="retry-state">{{ diagnostic }}</output>
    <AssistantWorkspace
      :context="context"
      :project-ref="projectRef"
      :live="realtime.platformState.state === 'live'"
    />
    <ConfirmDialogHost />
  </main>
</template>
<style scoped>
.assistant-retry-fixture {
  display: flex;
  flex-direction: column;
  height: 100dvh;
  box-sizing: border-box;
  padding: 12px;
  min-width: 0;
}
</style>
