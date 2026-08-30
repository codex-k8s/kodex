<script setup lang="ts">
import { History } from "@lucide/vue";
import { useI18n } from "vue-i18n";

import type { Project, Run } from "@/shared/api/generated/openapi/types.gen";
import { runPath } from "@/shared/routes";
import SafeSummary from "@/shared/ui/SafeSummary.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

const props = defineProps<{ runs: Run[]; projects: Project[] }>();
const { locale, t } = useI18n();

function projectName(projectRef: string): string {
  return (
    props.projects.find((project) => project.ref === projectRef)?.name ??
    t("common.unavailable")
  );
}

function formatDate(run: Run): string {
  return new Intl.DateTimeFormat(locale.value, {
    dateStyle: "medium",
    timeStyle: "short",
  }).format(new Date(run.finishedAt ?? run.createdAt));
}
</script>

<template>
  <div class="home-session-list">
    <RouterLink
      v-for="run in runs"
      :key="run.sessionRef"
      :to="runPath(run.ref, run.projectRef)"
      class="home-session-list__item"
    >
      <span class="home-session-list__icon">
        <History :size="17" aria-hidden="true" />
      </span>
      <div class="home-session-list__copy">
        <h3>{{ run.title }}</h3>
        <SafeSummary
          :content="run.resultSummary"
          :fallback="run.target.displayName"
        />
        <p>{{ projectName(run.projectRef) }} · {{ formatDate(run) }}</p>
      </div>
      <div class="home-session-list__aside">
        <StatusBadge :state="run.state" />
        <span class="button">{{ $t("common.continue") }}</span>
      </div>
    </RouterLink>
  </div>
</template>

<style scoped>
.home-session-list__item {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr) auto;
  align-items: center;
  gap: 12px;
  min-height: 78px;
  padding: 11px 16px;
  border-bottom: 1px solid var(--hairline);
  color: inherit;
  text-decoration: none;
}
.home-session-list__item:last-child {
  border-bottom: 0;
}
.home-session-list__item:hover {
  background: var(--panel);
  text-decoration: none;
}
.home-session-list__icon {
  display: grid;
  place-items: center;
  width: 34px;
  height: 34px;
  border-radius: 8px;
  color: var(--accent-strong);
  background: var(--accent-soft);
}
.home-session-list__copy {
  min-width: 0;
}
.home-session-list h3,
.home-session-list p {
  margin: 0;
}
.home-session-list h3 {
  margin-bottom: 3px;
  overflow-wrap: anywhere;
}
.home-session-list :deep(.safe-summary),
.home-session-list p {
  color: var(--muted);
  font-size: 0.76rem;
}
.home-session-list__aside {
  display: flex;
  align-items: flex-end;
  flex-direction: column;
  gap: 9px;
}
@media (max-width: 620px) {
  .home-session-list__item {
    grid-template-columns: auto minmax(0, 1fr);
  }
  .home-session-list__aside {
    grid-column: 2;
    align-items: center;
    flex-direction: row;
    justify-content: space-between;
  }
}
</style>
