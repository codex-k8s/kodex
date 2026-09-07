<script setup lang="ts">
import { computed } from "vue";
import type { IntegrationConfigurationField } from "@/shared/api/generated/openapi/types.gen";
import { readIntegerBounds } from "../integer-bounds";
const props = defineProps<{ field: IntegrationConfigurationField }>();
const bounds = computed(() => readIntegerBounds(props.field));
</script>
<template>
  <span v-if="field.valueType === 'INTEGER'" class="integer-bounds">
    <template v-if="bounds.valid">
      <span v-if="bounds.minimum !== undefined"
        >{{ $t("integrations.minimum") }}: {{ bounds.minimum.toString() }}</span
      >
      <span v-if="bounds.maximum !== undefined"
        >{{ $t("integrations.maximum") }}: {{ bounds.maximum.toString() }}</span
      >
    </template>
    <span v-else role="alert">{{ $t("integrations.invalidBounds") }}</span>
  </span>
</template>
<style scoped>
.integer-bounds {
  display: grid;
  gap: 3px;
  overflow-wrap: anywhere;
}
</style>
