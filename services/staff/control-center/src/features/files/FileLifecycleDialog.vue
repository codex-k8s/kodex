<script setup lang="ts">
import { AlertTriangle, RotateCcw, Trash2 } from "@lucide/vue";
import { computed } from "vue";

import type {
  ArtifactLifecycleAction,
  ArtifactLifecycleBlockReason,
  ArtifactLifecycleState,
} from "@/features/files/model";
import type { Artifact } from "@/shared/api/generated/openapi/types.gen";
import ModalDialog from "@/shared/ui/ModalDialog.vue";

const props = defineProps<{
  action: ArtifactLifecycleAction;
  artifact: Artifact;
  busy?: boolean;
  labels: {
    cancel: string;
    confirm: Record<ArtifactLifecycleAction, string>;
    description: Record<ArtifactLifecycleAction, string>;
    impactUnavailable: string;
    reason: Record<ArtifactLifecycleBlockReason, string>;
    title: Record<ArtifactLifecycleAction, string>;
  };
  state: ArtifactLifecycleState;
}>();

const emit = defineEmits<{ close: []; confirm: [] }>();
const destructive = computed(() => props.action !== "RESTORE");
</script>

<template>
  <ModalDialog
    :title="labels.title[action]"
    :busy="busy"
    size="sm"
    @close="emit('close')"
  >
    <div class="file-lifecycle-dialog">
      <span
        class="file-lifecycle-dialog__icon"
        :class="{ 'file-lifecycle-dialog__icon--danger': destructive }"
        aria-hidden="true"
      >
        <RotateCcw v-if="action === 'RESTORE'" :size="22" />
        <Trash2 v-else :size="22" />
      </span>
      <div>
        <p>
          <strong>{{ artifact.fileName }}</strong>
        </p>
        <p>{{ labels.description[action] }}</p>
      </div>
      <div
        v-if="!state.available"
        class="file-lifecycle-dialog__notice"
        role="status"
      >
        <AlertTriangle :size="18" aria-hidden="true" />
        <div>
          <strong>{{ labels.reason[state.reason] }}</strong>
          <p>{{ labels.impactUnavailable }}</p>
        </div>
      </div>
    </div>

    <template #actions>
      <button
        class="button"
        type="button"
        :disabled="busy"
        @click="emit('close')"
      >
        {{ labels.cancel }}
      </button>
      <button
        class="button"
        :class="destructive ? 'button--danger' : 'button--primary'"
        type="button"
        :disabled="busy || !state.available"
        :title="state.available ? undefined : labels.reason[state.reason]"
        @click="emit('confirm')"
      >
        <RotateCcw v-if="action === 'RESTORE'" :size="16" aria-hidden="true" />
        <Trash2 v-else :size="16" aria-hidden="true" />
        {{ labels.confirm[action] }}
      </button>
    </template>
  </ModalDialog>
</template>

<style scoped>
.file-lifecycle-dialog {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr);
  gap: 14px;
  align-items: start;
}
.file-lifecycle-dialog__icon {
  display: grid;
  width: 42px;
  height: 42px;
  place-items: center;
  border-radius: 7px;
  background: var(--accent-soft);
  color: var(--accent-strong);
}
.file-lifecycle-dialog__icon--danger {
  background: var(--danger-soft, #fff0f0);
  color: var(--danger, #b42318);
}
.file-lifecycle-dialog p {
  margin: 0 0 8px;
  color: var(--muted);
  line-height: 1.45;
}
.file-lifecycle-dialog strong {
  color: var(--text);
  overflow-wrap: anywhere;
}
.file-lifecycle-dialog__notice {
  display: flex;
  grid-column: 1 / -1;
  gap: 10px;
  padding: 12px;
  border: 1px solid var(--warning-border, #e8c46a);
  border-radius: 7px;
  background: var(--warning-soft, #fff8e6);
}
.file-lifecycle-dialog__notice svg {
  flex: 0 0 auto;
  color: var(--warning, #8a6100);
}
.file-lifecycle-dialog__notice p {
  margin: 4px 0 0;
  font-size: 0.82rem;
}
</style>
