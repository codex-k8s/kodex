<script setup lang="ts">
import { AlertTriangle, RotateCcw, Trash2 } from "@lucide/vue";
import { computed, ref, watch } from "vue";

import {
  trashBulkConfirmed,
  type ArtifactTrashBulkAction,
} from "@/features/files/model";
import ModalDialog from "@/shared/ui/ModalDialog.vue";

const props = defineProps<{
  action: ArtifactTrashBulkAction;
  busy?: boolean;
  count: number;
  labels: {
    cancel: string;
    confirm: Record<"DELETE" | "RESTORE" | "PURGE" | "EMPTY", string>;
    confirmationHint: string;
    confirmationPhrase: string;
    description: Record<"DELETE" | "RESTORE" | "PURGE" | "EMPTY", string>;
    executionHint: string;
    title: Record<"DELETE" | "RESTORE" | "PURGE" | "EMPTY", string>;
  };
}>();

const emit = defineEmits<{ close: []; confirm: [] }>();
const confirmation = ref("");
const destructive = computed(
  () => props.action === "PURGE" || props.action === "EMPTY",
);
const confirmed = computed(() =>
  trashBulkConfirmed(
    props.action,
    confirmation.value,
    props.labels.confirmationPhrase,
  ),
);

watch(
  () => props.action,
  () => {
    confirmation.value = "";
  },
);
</script>

<template>
  <ModalDialog
    :title="labels.title[action]"
    :busy="busy"
    size="md"
    @close="emit('close')"
  >
    <div class="trash-bulk-dialog">
      <span
        class="trash-bulk-dialog__icon"
        :class="{ 'trash-bulk-dialog__icon--danger': destructive }"
        aria-hidden="true"
      >
        <RotateCcw v-if="action === 'RESTORE'" :size="24" />
        <Trash2 v-else :size="24" />
      </span>
      <div>
        <p>
          <strong>{{ count }}</strong>
          {{ labels.description[action] }}
        </p>
        <p>{{ labels.executionHint }}</p>
        <div v-if="destructive" class="trash-bulk-dialog__warning">
          <AlertTriangle :size="18" aria-hidden="true" />
          <span>{{ labels.confirmationHint }}</span>
        </div>
        <label v-if="destructive">
          <span class="mono">{{ labels.confirmationPhrase }}</span>
          <input
            v-model="confirmation"
            type="text"
            autocomplete="off"
            spellcheck="false"
            :disabled="busy"
          />
        </label>
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
        :disabled="busy || !confirmed"
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
.trash-bulk-dialog {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr);
  gap: 16px;
  align-items: start;
}
.trash-bulk-dialog__icon {
  display: grid;
  width: 46px;
  height: 46px;
  place-items: center;
  border-radius: 7px;
  background: var(--accent-soft);
  color: var(--accent-strong);
}
.trash-bulk-dialog__icon--danger {
  background: var(--danger-soft, #fff0f0);
  color: var(--danger, #b42318);
}
.trash-bulk-dialog p {
  margin: 0 0 14px;
  color: var(--muted);
  line-height: 1.5;
}
.trash-bulk-dialog p strong {
  color: var(--text);
}
.trash-bulk-dialog__warning {
  display: flex;
  gap: 8px;
  padding: 10px 12px;
  border: 1px solid var(--warning-border, #e8c46a);
  border-radius: 7px;
  background: var(--warning-soft, #fff8e6);
  color: var(--text);
  line-height: 1.4;
}
.trash-bulk-dialog label {
  display: grid;
  gap: 6px;
  margin-top: 14px;
}
.trash-bulk-dialog input {
  width: 100%;
}
</style>
