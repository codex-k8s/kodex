<script setup lang="ts">
import {
  Bot,
  CalendarClock,
  Monitor,
  Network,
  Plug,
  UsersRound,
} from "@lucide/vue";
import { computed } from "vue";

import { runExecutor } from "@/features/workboard/model";
import type { Run } from "@/shared/api/generated/openapi/types.gen";
import { runPath } from "@/shared/routes";
import SafeSummary from "@/shared/ui/SafeSummary.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

const props = withDefaults(
  defineProps<{ run: Run; preserveProject?: boolean; compact?: boolean }>(),
  {
    preserveProject: false,
    compact: false,
  },
);
const executor = computed(() => runExecutor(props.run));
const link = computed(() =>
  runPath(
    props.run.ref,
    props.preserveProject ? props.run.projectRef : undefined,
  ),
);
const sourceIcon = computed(() => {
  switch (props.run.source) {
    case "SYSTEM_ASSISTANT":
      return Bot;
    case "SCHEDULE":
      return CalendarClock;
    case "INTEGRATION":
      return Plug;
    case "AGENT_DELEGATION":
      return Network;
    case "MATTERMOST":
      return UsersRound;
    default:
      return Monitor;
  }
});
</script>

<template>
  <RouterLink
    :to="link"
    class="run-work-item"
    :class="{ 'run-work-item--compact': compact }"
  >
    <div class="run-work-item__body">
      <h3>{{ run.title }}</h3>
      <SafeSummary
        :content="run.currentActivity ?? run.resultSummary"
        :fallback="run.target.displayName"
      />
      <dl class="run-work-item__actors">
        <div>
          <dt>{{ $t("workboard.executor") }}</dt>
          <dd>{{ executor ?? $t("workboard.executorUnavailable") }}</dd>
        </div>
        <div>
          <dt>{{ $t("workboard.initiator") }}</dt>
          <dd>{{ run.initiator.displayName }}</dd>
        </div>
        <div class="run-work-item__source">
          <dt>{{ $t("common.source") }}</dt>
          <dd
            :title="
              $t('workboard.sourceHint', {
                source: $t(`runs.source.${run.source}`),
              })
            "
          >
            <component :is="sourceIcon" :size="14" aria-hidden="true" />{{
              $t(`runs.source.${run.source}`)
            }}
          </dd>
        </div>
      </dl>
    </div>
    <div class="run-work-item__aside">
      <StatusBadge :state="run.state" />
      <time :datetime="run.createdAt">{{
        new Date(run.createdAt).toLocaleString()
      }}</time>
    </div>
  </RouterLink>
</template>

<style scoped>
.run-work-item {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 16px;
  min-height: 76px;
  padding: 12px 16px;
  border-bottom: 1px solid var(--hairline);
  color: inherit;
  text-decoration: none;
}
.run-work-item:last-child {
  border-bottom: 0;
}
.run-work-item:hover {
  background: var(--panel);
  text-decoration: none;
}
.run-work-item__body {
  min-width: 0;
}
.run-work-item h3 {
  margin: 0 0 4px;
  overflow-wrap: anywhere;
}
.run-work-item :deep(p) {
  margin: 0 0 7px;
  color: var(--muted);
}
.run-work-item__actors {
  display: flex;
  flex-wrap: wrap;
  gap: 5px 14px;
  margin: 0;
  color: var(--muted);
  font-size: 0.78rem;
}
.run-work-item__actors div {
  display: flex;
  gap: 4px;
  min-width: 0;
}
.run-work-item__actors dt::after {
  content: ":";
}
.run-work-item__actors dd {
  margin: 0;
  color: var(--text);
  font-weight: 500;
  overflow-wrap: anywhere;
}
.run-work-item__source dd {
  display: inline-flex;
  align-items: center;
  gap: 4px;
}
.run-work-item__aside {
  display: flex;
  flex-direction: column;
  align-items: flex-end;
  justify-content: space-between;
  gap: 10px;
}
.run-work-item__aside time {
  color: var(--muted);
  font-size: 0.72rem;
  white-space: nowrap;
}
.run-work-item--compact {
  min-height: 64px;
  padding-block: 9px;
}
@media (max-width: 680px) {
  .run-work-item {
    grid-template-columns: minmax(0, 1fr);
  }
  .run-work-item__aside {
    align-items: flex-start;
    flex-direction: row;
  }
  .run-work-item__actors {
    display: grid;
    gap: 4px;
  }
}
</style>
