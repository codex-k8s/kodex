<script setup lang="ts">
import { nextTick, onBeforeUnmount, ref, watch } from "vue";
import type {
  PromptTemplatePreview,
  Run,
} from "@/shared/api/generated/openapi/types.gen";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import PromptContextDetails from "@/features/agents/detail/PromptContextDetails.vue";
import ModalDialog from "@/shared/ui/ModalDialog.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";
import { loadRunPromptPreview } from "./run-prompt-preview";

const props = defineProps<{ run: Pick<Run, "ref" | "version" | "attempt"> }>();
const preview = ref<PromptTemplatePreview>();
const problem = ref<AppProblem>();
const loading = ref(false);
const opener = ref<HTMLButtonElement>();
let active: AbortController | undefined;
let mounted = true;
function invalidate(): void {
  active?.abort();
  active = undefined;
  preview.value = undefined;
  problem.value = undefined;
  loading.value = false;
}
watch(
  [() => props.run.ref, () => props.run.version, () => props.run.attempt],
  invalidate,
  { flush: "sync" },
);
onBeforeUnmount(() => {
  mounted = false;
  invalidate();
});
async function closePreview(): Promise<void> {
  const button = opener.value;
  const { ref: runRef, version, attempt } = props.run;
  invalidate();
  await nextTick();
  if (
    mounted &&
    button?.isConnected &&
    opener.value === button &&
    props.run.ref === runRef &&
    props.run.version === version &&
    props.run.attempt === attempt
  )
    button.focus();
}
async function refresh(): Promise<void> {
  if (loading.value) return;
  invalidate();
  const controller = new AbortController();
  active = controller;
  loading.value = true;
  try {
    const result = await loadRunPromptPreview(props.run.ref, controller.signal);
    if (active === controller && !controller.signal.aborted)
      preview.value = result;
  } catch (error) {
    if (active === controller && !controller.signal.aborted)
      problem.value = asProblem(error);
  } finally {
    if (active === controller) {
      active = undefined;
      loading.value = false;
    }
  }
}
</script>

<template>
  <div class="run-prompt-preview stack">
    <button
      ref="opener"
      type="button"
      class="button button--secondary"
      :disabled="loading"
      @click="refresh"
    >
      {{ $t(loading ? "common.loading" : "promptContext.preview") }}
    </button>
    <p class="text-muted">{{ $t("runs.promptPreviewSafeHint") }}</p>
    <ProblemNotice v-if="problem" :problem="problem" @retry="refresh" />
    <ModalDialog
      v-if="preview"
      :title="$t('promptContext.preview')"
      size="xl"
      @close="closePreview"
      @keydown.stop
    >
      <div class="run-prompt-preview__content">
        <StatusBadge :state="preview.complete ? 'AVAILABLE' : 'DRAFT'" />
        <PromptContextDetails :preview="preview" />
      </div>
    </ModalDialog>
  </div>
</template>

<style scoped>
.run-prompt-preview__content {
  display: grid;
  width: 100%;
  min-width: 0;
  grid-template-columns: minmax(0, 1fr);
  align-items: start;
  gap: 12px;
}
</style>
