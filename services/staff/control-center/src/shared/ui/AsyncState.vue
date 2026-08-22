<script setup lang="ts">
import type { AppProblem } from "@/shared/api/problem";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";

defineProps<{
  loading?: boolean;
  problem?: AppProblem;
  empty?: boolean;
  emptyTitle?: string;
  emptyText?: string;
}>();
const emit = defineEmits<{ retry: [] }>();
</script>

<template>
  <div
    v-if="loading"
    class="skeleton-stack"
    role="status"
    :aria-label="$t('common.loading')"
  >
    <span /><span /><span />
  </div>
  <ProblemNotice
    v-else-if="problem"
    :problem="problem"
    @retry="emit('retry')"
  />
  <section v-else-if="empty" class="empty-state">
    <div class="empty-state__icon" aria-hidden="true">+</div>
    <h2>{{ emptyTitle ?? $t("common.empty") }}</h2>
    <p v-if="emptyText">{{ emptyText }}</p>
    <slot name="empty-action" />
  </section>
  <slot v-else />
</template>
