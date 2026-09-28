<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";

import type { AppProblem } from "@/shared/api/problem";

const props = defineProps<{ problem?: AppProblem; compact?: boolean }>();
const emit = defineEmits<{ retry: [] }>();
const translator = useI18n();
const heading = computed(() => {
  if (props.problem?.code === "FRESH_AUTHENTICATION_REQUIRED")
    return translator.t("common.freshAuthenticationRequired");
  if (props.problem?.code === "RESOURCE_IN_USE")
    return translator.t("common.resourceInUse");
  if (props.problem?.kind === "forbidden")
    return translator.t("common.forbidden");
  if (props.problem?.kind === "conflict")
    return translator.t("common.conflict");
  if (props.problem?.kind === "unavailable")
    return translator.t("common.unavailable");
  return translator.t("common.error");
});
const message = computed(() => {
  if (props.problem?.code === "FRESH_AUTHENTICATION_REQUIRED")
    return translator.t("common.freshAuthenticationHelp");
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
      <ul v-if="problem?.diagnostics.length">
        <li
          v-for="diagnostic in problem.diagnostics"
          :key="`${diagnostic.code}-${diagnostic.line}-${diagnostic.column}`"
        >
          {{ diagnostic.message }}
          <code v-if="diagnostic.variableName">{{
            diagnostic.variableName
          }}</code>
          · {{ diagnostic.line }}:{{ diagnostic.column }}
        </li>
      </ul>
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

<style scoped>
.problem-notice {
  box-sizing: border-box;
  width: 100%;
  min-width: 0;
}
.problem-notice > div {
  min-width: 0;
  flex: 1 1 auto;
  overflow-wrap: anywhere;
}
.problem-notice small {
  display: block;
  max-width: 100%;
  overflow-wrap: anywhere;
}
</style>
