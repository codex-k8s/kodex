<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";

const props = defineProps<{ state: string }>();
const i18n = useI18n();
const label = computed(() =>
  i18n.te(`states.${props.state}`)
    ? i18n.t(`states.${props.state}`)
    : props.state,
);
const tone = computed(() => {
  if (/FAILED|ERROR|UNAVAILABLE|DELETED|DEAD_LETTER/.test(props.state))
    return "danger";
  if (/WAIT|PENDING|QUEUED|BLOCKED|CLAIMED|RESTORING/.test(props.state))
    return "warning";
  if (
    /ACTIVE|READY|AVAILABLE|SUCCEEDED|COMPLETED|RESTORED|UI|ui/.test(
      props.state,
    )
  )
    return "success";
  return "neutral";
});
</script>

<template>
  <span class="badge" :class="`badge--${tone}`">{{ label }}</span>
</template>
