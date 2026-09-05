<script setup lang="ts">
import { Activity, ArrowRight } from "@lucide/vue";
import { useI18n } from "vue-i18n";

import AgentAvatar from "@/features/agents/catalog/AgentAvatar.vue";
import type { AgentCatalogItem } from "@/features/agents/catalog/model";
import SafeSummary from "@/shared/ui/SafeSummary.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

defineProps<{ item: AgentCatalogItem; to: string }>();
const { t } = useI18n();
</script>

<template>
  <article class="agent-card">
    <div class="agent-card__head">
      <AgentAvatar
        :initials="item.initials"
        :source="item.avatarUrl"
        :tone="item.avatarTone"
      />
      <div class="agent-card__identity">
        <RouterLink :to="to" class="agent-card__name">{{
          item.name
        }}</RouterLink>
        <p>{{ item.purpose }}</p>
      </div>
    </div>

    <div v-if="item.role || item.runtimeModel" class="agent-card__tags">
      <span v-if="item.role" class="agent-card__tag">{{ item.role }}</span>
      <span
        v-if="item.runtimeModel"
        class="agent-card__tag agent-card__tag--mono"
      >
        {{ item.runtimeModel }}
      </span>
    </div>

    <div
      v-if="item.currentActivity && item.state !== 'READY'"
      class="agent-card__activity"
    >
      <Activity :size="15" aria-hidden="true" />
      <div>
        <span>{{ t("agents.currentActivity") }}</span>
        <SafeSummary
          :content="item.currentActivity"
          :fallback="t(`states.${item.state}`)"
        />
      </div>
    </div>

    <div class="agent-card__footer">
      <div class="agent-card__runtime">
        <strong>{{ item.runtimeName }}</strong>
        <span v-if="item.runtimeProvider || item.runtimeRevision">
          {{
            [item.runtimeProvider, item.runtimeRevision]
              .filter(Boolean)
              .join(" · ")
          }}
        </span>
        <span v-if="!item.runtimeReady" class="agent-card__runtime-warning">
          {{ t("states.UNAVAILABLE") }}
        </span>
      </div>
      <StatusBadge :state="item.state" :tone="item.statusTone" />
    </div>

    <RouterLink :to="to" class="button agent-card__action">
      {{ t("common.open") }}
      <ArrowRight :size="16" aria-hidden="true" />
    </RouterLink>
  </article>
</template>

<style scoped>
.agent-card {
  display: flex;
  min-width: 0;
  min-height: 242px;
  flex-direction: column;
  gap: 12px;
  padding: 15px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--surface);
}
.agent-card:hover {
  border-color: var(--border-strong);
  box-shadow: 0 5px 18px rgba(16, 22, 30, 0.06);
}
.agent-card__head {
  display: flex;
  align-items: flex-start;
  min-width: 0;
  gap: 12px;
}
.agent-card__identity {
  min-width: 0;
}
.agent-card__name {
  display: block;
  overflow: hidden;
  color: var(--text);
  font-size: 0.96rem;
  font-weight: 600;
  line-height: 1.35;
  text-decoration: none;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.agent-card__name:hover {
  color: var(--accent-strong);
  text-decoration: underline;
}
.agent-card__identity p {
  display: -webkit-box;
  min-height: 36px;
  margin: 3px 0 0;
  overflow: hidden;
  color: var(--muted);
  line-height: 1.4;
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 2;
}
.agent-card__tags {
  display: flex;
  min-height: 24px;
  flex-wrap: wrap;
  gap: 6px;
}
.agent-card__tag {
  max-width: 100%;
  overflow: hidden;
  padding: 3px 7px;
  border: 1px solid var(--border);
  border-radius: 5px;
  color: var(--text-secondary);
  background: var(--panel);
  font-size: 0.75rem;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.agent-card__tag--mono {
  font-family: var(--font-mono);
}
.agent-card__activity {
  display: grid;
  grid-template-columns: 15px minmax(0, 1fr);
  min-height: 54px;
  gap: 8px;
  padding: 9px 10px;
  color: var(--muted);
  background: var(--panel);
}
.agent-card__activity > svg {
  margin-top: 2px;
  color: var(--subtle);
}
.agent-card__activity > div {
  min-width: 0;
}
.agent-card__activity > div > span {
  display: block;
  margin-bottom: 2px;
  color: var(--subtle);
  font-size: 0.72rem;
}
.agent-card__footer {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  min-width: 0;
  gap: 10px;
  margin-top: auto;
}
.agent-card__runtime {
  display: grid;
  min-width: 0;
  gap: 2px;
}
.agent-card__runtime strong,
.agent-card__runtime span {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.agent-card__runtime strong {
  font-size: 0.78rem;
  font-weight: 600;
}
.agent-card__runtime span {
  color: var(--subtle);
  font-family: var(--font-mono);
  font-size: 0.68rem;
}
.agent-card__runtime .agent-card__runtime-warning {
  color: var(--warning);
  font-family: var(--font-sans);
}
.agent-card__action {
  width: 100%;
  min-height: 36px;
}
@media (max-width: 560px) {
  .agent-card {
    min-height: 0;
    gap: 10px;
  }
  .agent-card__action {
    min-height: 42px;
  }
}
</style>
