<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import AsyncEntityPicker from "@/shared/ui/AsyncEntityPicker.vue";
import ModalDialog from "@/shared/ui/ModalDialog.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import CodeEditorSurface from "./CodeEditorSurface.vue";
import { loadOverlayHistory, readOverlayRevision } from "./overlay-history";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import type { ConfigOverlayVersion } from "@/shared/api/generated/openapi/types.gen";
const props = defineProps<{
  agentRef: string;
  agentVersion: number;
  disabled: boolean;
  canEdit: boolean;
}>();
const emit = defineEmits<{ restore: [revisionRef: string] }>();
const { t } = useI18n();
const opened = ref(false);
const selected = ref<ConfigOverlayVersion>();
const selectedRef = ref("");
const loading = ref(false);
const problem = ref<AppProblem>();
let controller: AbortController | undefined;
let generation = 0;
async function loadPage(
  query: string,
  cursor: string | undefined,
  signal: AbortSignal,
) {
  const page = await loadOverlayHistory(props.agentRef, query, cursor, signal);
  return {
    items: page.items.map((item) => ({
      ref: item.ref,
      title: t("managed.revision", { revision: item.revision }),
      description: `${item.createdAt} · ${item.digest}`,
    })),
    nextPageToken: page.nextPageToken,
  };
}
async function choose(value: string | null | readonly string[]) {
  if (props.disabled || typeof value !== "string") return;
  controller?.abort();
  const active = new AbortController();
  controller = active;
  const current = ++generation;
  selected.value = undefined;
  selectedRef.value = value;
  problem.value = undefined;
  loading.value = true;
  try {
    const revision = await readOverlayRevision(
      props.agentRef,
      value,
      active.signal,
    );
    if (current === generation) selected.value = revision;
  } catch (error) {
    if (current === generation && !active.signal.aborted)
      problem.value = asProblem(error);
  } finally {
    if (current === generation) loading.value = false;
  }
}
function close() {
  controller?.abort();
  generation++;
  opened.value = false;
  selected.value = undefined;
  selectedRef.value = "";
  loading.value = false;
  problem.value = undefined;
}
function restore() {
  if (!props.canEdit || props.disabled || loading.value || !selected.value)
    return;
  emit("restore", selected.value.ref);
  close();
}
watch(() => [props.agentRef, props.agentVersion], close);
onBeforeUnmount(close);
</script>
<template>
  <button
    type="button"
    class="button"
    :disabled="disabled"
    @click="opened = true"
  >
    {{ $t("runtimeOverlay.history") }}
  </button>
  <ModalDialog
    v-if="opened"
    :title="$t('runtimeOverlay.history')"
    size="xl"
    @close="close"
  >
    <p>{{ $t("runtimeOverlay.restoreHelp") }}</p>
    <AsyncEntityPicker
      :model-value="selectedRef"
      :load-page="loadPage"
      :placeholder="$t('runtimeOverlay.chooseRevision')"
      :disabled="disabled"
      :clearable="false"
      @update:model-value="choose"
    />
    <p v-if="loading" role="status">{{ $t("common.loading") }}</p>
    <ProblemNotice
      v-if="problem"
      :problem="problem"
      @retry="choose(selectedRef)"
    />
    <template v-if="selected">
      <p class="overlay-history-identity">
        {{ selected.ref }} · {{ selected.digest }}
      </p>
      <CodeEditorSurface
        :model-value="selected.content"
        language="toml"
        readonly
        :label="$t('runtimeOverlay.history')"
      />
      <button
        v-if="canEdit"
        type="button"
        class="button button--primary"
        :disabled="disabled || loading"
        @click="restore"
      >
        {{ $t("runtimeOverlay.restore") }}
      </button>
    </template>
  </ModalDialog>
</template>
<style scoped>
.overlay-history-identity {
  overflow-wrap: anywhere;
}
</style>
