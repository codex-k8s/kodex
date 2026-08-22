<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";

const props = defineProps<{ state: string }>();
const translator = useI18n();
const label = computed(() =>
  translator.te(`states.${props.state}`)
    ? translator.t(`states.${props.state}`)
    : props.state,
);
const tone = computed(() => {
  if (
    [
      "READY",
      "ACTIVE",
      "SUCCEEDED",
      "PUBLISHED",
      "CONNECTED",
      "CLEAN",
      "APPROVED",
    ].includes(props.state)
  )
    return "success";
  if (["FAILED", "REJECTED", "QUARANTINED", "EXPIRED"].includes(props.state))
    return "danger";
  if (
    [
      "WAITING",
      "WAITING_HUMAN",
      "OPEN",
      "NEEDS_ATTENTION",
      "CANCELLING",
      "RECOVERING",
    ].includes(props.state)
  )
    return "warning";
  return "neutral";
});
</script>

<template>
  <span class="status-badge" :class="`status-badge--${tone}`">
    <span class="status-badge__dot" aria-hidden="true" />{{ label }}
  </span>
</template>
