<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";

type StatusBadgeTone = "success" | "danger" | "warning" | "accent" | "neutral";

const props = defineProps<{
  state: string;
  label?: string;
  tone?: StatusBadgeTone;
}>();
const translator = useI18n();
const label = computed(() => {
  if (props.label) return props.label;
  return translator.te(`states.${props.state}`)
    ? translator.t(`states.${props.state}`)
    : translator.t("common.unknownStatus");
});
const tone = computed(() => {
  if (props.tone) return props.tone;
  if (["PUBLISHED", "APPLIED"].includes(props.state)) return "accent";
  if (
    [
      "READY",
      "ACTIVE",
      "SUCCEEDED",
      "PUBLISHED",
      "CONNECTED",
      "CLEAN",
      "APPROVED",
      "OUTCOME_SUCCEEDED",
    ].includes(props.state)
  )
    return "success";
  if (
    [
      "FAILED",
      "REJECTED",
      "REVOKED",
      "QUARANTINED",
      "EXPIRED",
      "OUTCOME_FAILED",
    ].includes(props.state)
  )
    return "danger";
  if (
    [
      "WAITING",
      "WAITING_HUMAN",
      "OPEN",
      "NEEDS_ATTENTION",
      "CANCELLING",
      "RECOVERING",
      "DEGRADED",
      "STALE",
      "OUTCOME_NEEDS_ATTENTION",
    ].includes(props.state)
  )
    return "warning";
  if (props.state === "OUTCOME_CANCELLED") return "neutral";
  return "neutral";
});
</script>

<template>
  <span
    class="status-badge"
    :class="`status-badge--${tone}`"
    :data-state="state"
  >
    <span class="status-badge__dot" aria-hidden="true" />{{ label }}
  </span>
</template>
