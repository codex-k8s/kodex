<script setup lang="ts">
import { ShieldX } from "@lucide/vue";

import type { AppProblem } from "@/shared/api/problem";
import ModalDialog from "@/shared/ui/ModalDialog.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";

import type { RuntimeSecret } from "./model";

defineProps<{
  busy?: boolean;
  problem?: AppProblem;
  secret: RuntimeSecret;
}>();
const emit = defineEmits<{ close: []; confirm: [] }>();
</script>

<template>
  <ModalDialog
    :title="$t('runtimeSecrets.revokeTitle')"
    :busy="busy"
    size="md"
    @close="emit('close')"
  >
    <div class="revoke-dialog">
      <ProblemNotice
        v-if="problem"
        class="revoke-dialog__problem"
        :problem="problem"
        compact
      />
      <div class="revoke-dialog__summary">
        <ShieldX :size="28" aria-hidden="true" />
        <div class="revoke-dialog__copy">
          <strong>{{ secret.name }}</strong>
          <p>{{ $t("runtimeSecrets.revokeHelp") }}</p>
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
        {{ $t("common.cancel") }}
      </button>
      <button
        class="button button--danger"
        type="button"
        :disabled="busy"
        @click="emit('confirm')"
      >
        <ShieldX :size="16" aria-hidden="true" />
        {{ $t("runtimeSecrets.revoke") }}
      </button>
    </template>
  </ModalDialog>
</template>

<style scoped>
.revoke-dialog {
  display: grid;
  box-sizing: border-box;
  width: 100%;
  min-width: 0;
  max-width: 100%;
  align-self: stretch;
  gap: 16px;
  color: var(--danger);
}
.revoke-dialog__problem {
  box-sizing: border-box;
  width: 100%;
  min-width: 0;
  max-width: 100%;
}
.revoke-dialog__summary {
  display: grid;
  width: 100%;
  min-width: 0;
  max-width: 100%;
  grid-template-columns: auto minmax(0, 1fr);
  align-items: start;
  gap: 12px;
}
.revoke-dialog__copy {
  min-width: 0;
  overflow-wrap: anywhere;
}
.revoke-dialog__copy p {
  margin: 6px 0 0;
  color: var(--text-secondary);
}
</style>
