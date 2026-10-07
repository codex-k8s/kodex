<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";

import RunTranscript from "@/features/runs/RunTranscript.vue";
import { buildRunTranscriptItems } from "@/features/runs/run-activity";
import type { RunEvent } from "@/shared/api/generated/openapi/types.gen";

const props = withDefaults(
  defineProps<{
    events: readonly RunEvent[];
    embedded?: boolean;
    closedExecutionKeys?: readonly string[];
    activeItemId?: string | null;
  }>(),
  { embedded: false, closedExecutionKeys: () => [] },
);
const { t } = useI18n();
const items = computed(() =>
  buildRunTranscriptItems(props.events, { platform: t("runs.platformActor") }),
);
</script>

<template>
  <RunTranscript
    :items="items"
    :embedded="embedded"
    :closed-execution-keys="closedExecutionKeys"
    :active-item-id="activeItemId"
  />
</template>
