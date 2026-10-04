<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from "vue";
import { useRouter } from "vue-router";
import AssistantWorkspace from "../../src/features/assistant/components/AssistantWorkspace.vue";
import { useAssistantStore } from "../../src/features/assistant/store";
import { usePlatformStore } from "../../src/features/platform/store";
import { useRealtimeStore } from "../../src/features/realtime/store";
import ConfirmDialogHost from "../../src/shared/ui/ConfirmDialogHost.vue";
import { selectProjectRef } from "../../src/shared/project-context";
import type { AssistantContextDescriptor } from "../../src/shared/api/generated/openapi/types.gen";

const router = useRouter();
const assistant = useAssistantStore();
const platform = usePlatformStore();
const realtime = useRealtimeStore();
const projectRef = ref("prj_concurrency_a");
const initialized = ref(false);
const context = computed<AssistantContextDescriptor>(() => ({
  route: `/projects/${projectRef.value}`,
  entityKind: "PROJECT",
  entityRef: projectRef.value,
  entityName: projectRef.value,
  entityVersion: 1,
  allowedOperations: [],
}));
const snapshotRevision = computed(() =>
  [
    platform.assistantRealtimeScopeKey,
    platform.assistant?.version,
    ...Object.values(platform.conversations).map(
      (value) => `${value.ref}:${String(value.version)}`,
    ),
  ].join("|"),
);
// Тот же мост cache → workspace, что у AppShell; методы store не подменяются.
watch(snapshotRevision, () => {
  if (platform.assistantRealtimeScopeKey !== projectRef.value) return;
  assistant.applyRealtimeSnapshot(
    platform.assistant,
    Object.values(platform.conversations),
    projectRef.value,
    platform.assistantConversationNextPageToken,
  );
});
async function changeProject(value: string): Promise<void> {
  projectRef.value = value;
  selectProjectRef(value);
  await router.push(context.value.route);
  assistant.setContext(context.value, value);
  await assistant.load(context.value, value);
  realtime.changeProjectScope();
}
onMounted(async () => {
  selectProjectRef(projectRef.value);
  await assistant.load(context.value, projectRef.value);
  initialized.value = true;
  realtime.openPlatform();
});
onUnmounted(() => realtime.closeAll());
const diagnostic = computed(() =>
  JSON.stringify({
    initialized: initialized.value,
    projectRef: projectRef.value,
    state: realtime.platformState.state,
    sequence: realtime.platformSequence,
    selectedRef: assistant.selectedRef,
    conversations: Object.values(assistant.conversations),
  }),
);
</script>
<template>
  <main>
    <h1>Изоляция параллельных диалогов — синтетическая проверка</h1>
    <button
      class="button"
      data-testid="project-a"
      @click="changeProject('prj_concurrency_a')"
    >
      Проект A
    </button>
    <button
      class="button"
      data-testid="project-b"
      @click="changeProject('prj_concurrency_b')"
    >
      Проект B
    </button>
    <output hidden data-testid="concurrency-state">{{ diagnostic }}</output>
    <AssistantWorkspace
      :context="context"
      :project-ref="projectRef"
      :live="realtime.platformState.state === 'live'"
    />
    <ConfirmDialogHost />
  </main>
</template>
