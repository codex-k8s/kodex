<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";

import type { AppProblem } from "@/shared/api/problem";

const props = defineProps<{ problem?: AppProblem; compact?: boolean }>();
const emit = defineEmits<{ retry: [] }>();
const translator = useI18n();
const heading = computed(() => {
  if (props.problem?.kind === "forbidden")
    return translator.t("common.forbidden");
  if (props.problem?.kind === "conflict")
    return translator.t("common.conflict");
  if (props.problem?.kind === "unavailable")
    return translator.t("common.unavailable");
  return translator.t("common.error");
});
const message = computed(() => {
  if (props.problem?.title) return props.problem.title;
  const key = `errors.${props.problem?.code ?? "default"}`;
  return translator.te(key)
    ? translator.t(key)
    : translator.t("errors.default");
});
</script>

<template>
  <section class="problem-notice" role="alert">
    <div>
      <strong>{{ heading }}</strong>
      <p>{{ message }}</p>
      <small v-if="problem?.correlationId">{{ problem.correlationId }}</small>
    </div>
    <button
      v-if="problem?.retryable && !compact"
      class="button button--secondary"
      type="button"
      @click="emit('retry')"
    >
      {{ $t("common.retry") }}
    </button>
  </section>
</template>
