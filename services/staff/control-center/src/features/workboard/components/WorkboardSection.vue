<script setup lang="ts">
import type { AppProblem } from "@/shared/api/problem";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";

defineProps<{
  title: string;
  count?: number;
  loading?: boolean;
  refreshing?: boolean;
  ready: boolean;
  problem?: AppProblem;
  empty?: boolean;
  emptyText?: string;
}>();
const emit = defineEmits<{ retry: [] }>();
</script>

<template>
  <section class="workboard-section panel">
    <header class="workboard-section__header">
      <div class="workboard-section__title">
        <h2>{{ title }}</h2>
        <span v-if="count !== undefined" class="workboard-section__count">{{
          count
        }}</span>
      </div>
      <div class="workboard-section__actions">
        <span
          v-if="refreshing && ready"
          class="workboard-section__refresh"
          role="status"
        >
          <span aria-hidden="true" />{{ $t("workboard.refreshing") }}
        </span>
        <slot name="action" />
      </div>
    </header>

    <div
      v-if="loading && !ready"
      class="workboard-section__skeleton"
      role="status"
      :aria-label="$t('common.loading')"
    >
      <span /><span /><span />
    </div>
    <ProblemNotice
      v-else-if="problem && !ready"
      :problem="problem"
      @retry="emit('retry')"
    />
    <template v-else>
      <ProblemNotice
        v-if="problem"
        class="workboard-section__warning"
        :problem="problem"
        @retry="emit('retry')"
      />
      <div v-if="empty" class="workboard-section__empty">
        {{ emptyText ?? $t("common.empty") }}
      </div>
      <slot v-else />
    </template>
  </section>
</template>

<style scoped>
.workboard-section {
  padding: 0;
  overflow: hidden;
}
.workboard-section__header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  min-height: 52px;
  padding: 12px 16px;
  border-bottom: 1px solid var(--hairline);
}
.workboard-section__title,
.workboard-section__actions,
.workboard-section__refresh {
  display: flex;
  align-items: center;
  gap: 8px;
}
.workboard-section__title h2 {
  margin: 0;
  font-size: 0.95rem;
}
.workboard-section__count {
  min-width: 23px;
  padding: 2px 7px;
  border-radius: 999px;
  color: var(--muted);
  background: var(--hairline);
  font-family: var(--font-mono);
  font-size: 0.75rem;
  text-align: center;
}
.workboard-section__refresh {
  color: var(--muted);
  font-size: 0.75rem;
}
.workboard-section__refresh > span {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: var(--accent);
  animation: refresh-pulse 1.2s ease-in-out infinite;
}
.workboard-section__skeleton {
  display: grid;
  gap: 10px;
  padding: 14px 16px;
}
.workboard-section__skeleton span {
  height: 52px;
  border-radius: 8px;
  background: linear-gradient(
    90deg,
    var(--hairline),
    var(--panel),
    var(--hairline)
  );
  background-size: 200% 100%;
  animation: refresh-skeleton 1.4s linear infinite;
}
.workboard-section__warning {
  margin: 12px 16px 0;
}
.workboard-section__empty {
  padding: 28px 16px;
  color: var(--muted);
  text-align: center;
}
@keyframes refresh-pulse {
  50% {
    opacity: 0.35;
  }
}
@keyframes refresh-skeleton {
  to {
    background-position: -200% 0;
  }
}
@media (prefers-reduced-motion: reduce) {
  .workboard-section__refresh > span,
  .workboard-section__skeleton span {
    animation: none;
  }
}
@media (max-width: 620px) {
  .workboard-section__header {
    align-items: flex-start;
  }
  .workboard-section__actions {
    justify-content: flex-end;
    flex-wrap: wrap;
  }
}
</style>
