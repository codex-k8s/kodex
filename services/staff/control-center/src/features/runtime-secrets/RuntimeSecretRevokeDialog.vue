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
      <ProblemNotice v-if="problem" :problem="problem" compact />
      <ShieldX :size="28" aria-hidden="true" />
      <div>
        <strong>{{ secret.name }}</strong>
        <p>{{ $t("runtimeSecrets.revokeHelp") }}</p>
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
  grid-template-columns: 34px minmax(0, 1fr);
  gap: 12px;
  color: var(--danger);
}
.revoke-dialog p {
  margin: 6px 0 0;
  color: var(--text-secondary);
}
</style>
