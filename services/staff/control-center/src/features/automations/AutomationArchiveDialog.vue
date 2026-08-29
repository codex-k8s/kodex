<script setup lang="ts">
import { Archive, CircleAlert } from "@lucide/vue";

import type { Schedule } from "@/shared/api/generated/openapi/types.gen";
import ModalDialog from "@/shared/ui/ModalDialog.vue";

defineProps<{
  busy?: boolean;
  cancelLabel: string;
  confirmLabel: string;
  description: string;
  schedule: Schedule;
  title: string;
}>();
const emit = defineEmits<{ close: []; confirm: [] }>();
</script>

<template>
  <ModalDialog :title="title" :busy="busy" @close="emit('close')">
    <div class="automation-archive-confirmation">
      <CircleAlert :size="24" aria-hidden="true" />
      <div>
        <strong>{{ schedule.name }}</strong>
        <p>{{ description }}</p>
      </div>
    </div>
    <template #actions>
      <button
        class="button"
        type="button"
        :disabled="busy"
        @click="emit('close')"
      >
        {{ cancelLabel }}
      </button>
      <button
        class="button button--danger"
        type="button"
        :disabled="busy"
        @click="emit('confirm')"
      >
        <Archive :size="16" aria-hidden="true" />
        {{ confirmLabel }}
      </button>
    </template>
  </ModalDialog>
</template>

<style scoped>
.automation-archive-confirmation {
  display: grid;
  width: min(480px, 72vw);
  grid-template-columns: 28px minmax(0, 1fr);
  gap: 12px;
  color: var(--danger);
}
.automation-archive-confirmation p {
  margin: 6px 0 0;
  color: var(--muted);
}
</style>
