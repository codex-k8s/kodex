<script setup lang="ts">
import type {
  ManagedConfiguration,
  ManagedConfigurationRevision,
} from "@/shared/api/generated/openapi/types.gen";
import { canRestoreRevision } from "./restore-revision";

defineProps<{
  configuration: ManagedConfiguration;
  revision: ManagedConfigurationRevision;
  disabled?: boolean;
}>();
defineEmits<{ restore: [] }>();
</script>

<template>
  <button
    v-if="canRestoreRevision(configuration, revision)"
    type="button"
    class="button"
    :disabled="disabled"
    :title="$t('configurationRestore.help')"
    @click="$emit('restore')"
  >
    {{ $t("configurationRestore.action") }}
  </button>
</template>
