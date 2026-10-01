<script setup lang="ts">
import { AlertTriangle } from "@lucide/vue";
import { useI18n } from "vue-i18n";

import ModalDialog from "@/shared/ui/ModalDialog.vue";
import {
  confirmationState,
  resolveConfirmation,
} from "@/shared/ui/confirmation";

const { t } = useI18n();
</script>

<template>
  <Teleport to="body">
    <ModalDialog
      v-if="confirmationState.open"
      :title="confirmationState.title || t('confirmation.title')"
      size="sm"
      @close="resolveConfirmation(false)"
    >
      <div class="confirmation-dialog">
        <span
          class="confirmation-dialog__icon"
          :class="{
            'confirmation-dialog__icon--danger':
              confirmationState.tone === 'danger',
          }"
          aria-hidden="true"
        >
          <AlertTriangle :size="22" />
        </span>
        <p>{{ confirmationState.message }}</p>
      </div>
      <template #actions>
        <button
          class="button"
          type="button"
          @click="resolveConfirmation(false)"
        >
          {{ confirmationState.cancelLabel || t("common.cancel") }}
        </button>
        <button
          class="button"
          :class="
            confirmationState.tone === 'danger'
              ? 'button--danger'
              : 'button--primary'
          "
          type="button"
          data-confirm-dialog-primary
          @click="resolveConfirmation(true)"
        >
          {{ confirmationState.confirmLabel || t("common.confirm") }}
        </button>
      </template>
    </ModalDialog>
  </Teleport>
</template>

<style scoped>
.confirmation-dialog {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr);
  gap: 14px;
  align-items: start;
}
.confirmation-dialog__icon {
  display: grid;
  width: 42px;
  height: 42px;
  place-items: center;
  border-radius: 8px;
  color: var(--accent-strong);
  background: var(--accent-soft);
}
.confirmation-dialog__icon--danger {
  color: var(--danger);
  background: var(--danger-soft);
}
.confirmation-dialog p {
  margin: 2px 0 0;
  color: var(--text);
  line-height: 1.5;
  white-space: pre-line;
}
</style>
