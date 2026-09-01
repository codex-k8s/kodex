<script setup lang="ts">
import { ArrowRight } from "@lucide/vue";
import { computed } from "vue";
import { useI18n } from "vue-i18n";

import AgentAvatar from "@/features/agents/catalog/AgentAvatar.vue";
import type { AgentCatalogItem } from "@/features/agents/catalog/model";
import SafeSummary from "@/shared/ui/SafeSummary.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

defineProps<{ items: AgentCatalogItem[]; projectRef: string }>();
const { locale, t } = useI18n();
const formatter = computed(
  () =>
    new Intl.DateTimeFormat(locale.value, {
      dateStyle: "medium",
      timeStyle: "short",
    }),
);

function formattedDate(value: string): string {
  const date = new Date(value);
  return Number.isNaN(date.valueOf())
    ? t("common.noData")
    : formatter.value.format(date);
}
</script>

<template>
  <div class="agent-table-wrap">
    <table class="agent-table">
      <thead>
        <tr>
          <th>{{ t("common.name") }}</th>
          <th>{{ t("agents.role") }}</th>
          <th>{{ t("common.status") }}</th>
          <th>{{ t("agents.runtime") }}</th>
          <th>{{ t("agents.currentActivity") }}</th>
          <th>{{ t("agents.updatedAt") }}</th>
          <th>
            <span class="sr-only">{{ t("common.actions") }}</span>
          </th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="item in items" :key="item.ref">
          <td>
            <div class="agent-table__identity">
              <AgentAvatar
                :initials="item.initials"
                :source="item.avatarUrl"
                :tone="item.avatarTone"
                size="compact"
              />
              <div>
                <RouterLink :to="`/projects/${projectRef}/agents/${item.ref}`">
                  {{ item.name }}
                </RouterLink>
                <span>{{ item.purpose }}</span>
              </div>
            </div>
          </td>
          <td>{{ item.role || t("common.noData") }}</td>
          <td>
            <StatusBadge :state="item.state" :tone="item.statusTone" />
          </td>
          <td>
            <div class="agent-table__runtime">
              <strong>{{ item.runtimeName }}</strong>
              <span>
                {{
                  item.runtimeModel ||
                  item.runtimeProvider ||
                  t("common.noData")
                }}
                <template v-if="!item.runtimeReady">
                  · {{ t("states.UNAVAILABLE") }}
                </template>
              </span>
            </div>
          </td>
          <td>
            <SafeSummary
              :content="item.currentActivity"
              :fallback="t(`states.${item.state}`)"
            />
          </td>
          <td>
            <time :datetime="item.updatedAt">{{
              formattedDate(item.updatedAt)
            }}</time>
          </td>
          <td>
            <RouterLink
              :to="`/projects/${projectRef}/agents/${item.ref}`"
              class="button button--ghost agent-table__action"
              :aria-label="`${t('common.open')}: ${item.name}`"
              :title="t('common.open')"
            >
              <ArrowRight :size="17" aria-hidden="true" />
            </RouterLink>
          </td>
        </tr>
      </tbody>
    </table>
  </div>
</template>

<style scoped>
.agent-table-wrap {
  width: 100%;
  overflow: auto;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--surface);
}
.agent-table {
  width: 100%;
  min-width: 930px;
  border-collapse: collapse;
  table-layout: fixed;
}
.agent-table th,
.agent-table td {
  padding: 10px 12px;
  border-bottom: 1px solid var(--hairline);
  text-align: left;
  vertical-align: middle;
}
.agent-table th {
  position: sticky;
  z-index: 1;
  top: 0;
  color: var(--subtle);
  background: var(--panel);
  font-size: 0.72rem;
  font-weight: 600;
}
.agent-table th:nth-child(1) {
  width: 235px;
}
.agent-table th:nth-child(2) {
  width: 130px;
}
.agent-table th:nth-child(3) {
  width: 120px;
}
.agent-table th:nth-child(4) {
  width: 150px;
}
.agent-table th:nth-child(5) {
  width: 220px;
}
.agent-table th:nth-child(6) {
  width: 145px;
}
.agent-table th:last-child {
  width: 52px;
}
.agent-table tbody tr:last-child td {
  border-bottom: 0;
}
.agent-table tbody tr:hover {
  background: var(--panel);
}
.agent-table__identity {
  display: flex;
  align-items: center;
  min-width: 0;
  gap: 10px;
}
.agent-table__identity > div,
.agent-table__runtime {
  display: grid;
  min-width: 0;
  gap: 2px;
}
.agent-table__identity a,
.agent-table__identity span,
.agent-table__runtime strong,
.agent-table__runtime span {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.agent-table__identity a {
  font-weight: 600;
  text-decoration: none;
}
.agent-table__identity a:hover {
  color: var(--accent-strong);
  text-decoration: underline;
}
.agent-table__identity span,
.agent-table__runtime span {
  color: var(--subtle);
  font-size: 0.74rem;
}
.agent-table__runtime strong {
  font-size: 0.8rem;
}
.agent-table__runtime span {
  font-family: var(--font-mono);
}
.agent-table time {
  color: var(--subtle);
  font-size: 0.72rem;
  white-space: nowrap;
}
.agent-table__action {
  width: 32px;
  height: 32px;
  padding: 0;
}
</style>
