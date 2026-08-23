<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from "vue";
import { useRoute } from "vue-router";

import { usePlatformStore } from "@/features/platform/store";
import AsyncState from "@/shared/ui/AsyncState.vue";
import PageFrame from "@/shared/ui/PageFrame.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

const platform = usePlatformStore();
const route = useRoute();
const query = ref("");
const projectRef = computed(() =>
  typeof route.query.projectRef === "string"
    ? route.query.projectRef
    : undefined,
);
const list = computed(() => platform.auditEvents);
let searchTimer: ReturnType<typeof setTimeout> | undefined;

function load(): void {
  void platform.loadAudit(projectRef.value, query.value);
}

watch(query, () => {
  if (searchTimer) clearTimeout(searchTimer);
  searchTimer = setTimeout(load, 250);
});
watch(projectRef, load);
onMounted(load);
onUnmounted(() => {
  if (searchTimer) clearTimeout(searchTimer);
});
</script>

<template>
  <PageFrame :title="$t('audit.title')" :subtitle="$t('audit.subtitle')">
    <label class="field audit-search"
      ><span>{{ $t("audit.search") }}</span
      ><input
        v-model="query"
        type="search"
        :placeholder="$t('audit.searchPlaceholder')"
        autocomplete="off"
    /></label>
    <AsyncState
      :loading="platform.loading.audit"
      :problem="platform.problems.audit"
      :empty="list.length === 0"
      :empty-title="$t('audit.emptyTitle')"
      @retry="load"
    >
      <div class="audit-table" role="table" :aria-label="$t('audit.title')">
        <div class="audit-table__header" role="row">
          <strong role="columnheader">{{ $t("audit.time") }}</strong
          ><strong role="columnheader">{{ $t("audit.initiator") }}</strong
          ><strong role="columnheader">{{ $t("audit.action") }}</strong
          ><strong role="columnheader">{{ $t("audit.resource") }}</strong
          ><strong role="columnheader">{{ $t("audit.outcome") }}</strong>
        </div>
        <article
          v-for="event in list"
          :key="event.ref"
          class="audit-table__row"
          role="row"
        >
          <time role="cell" :datetime="event.occurredAt">{{
            new Date(event.occurredAt).toLocaleString()
          }}</time>
          <div role="cell">
            <strong>{{ event.initiator.displayName }}</strong
            ><small>{{ event.executor }}</small>
          </div>
          <div role="cell">
            <strong>{{ event.action }}</strong
            ><small>{{ event.safeSummary }}</small>
          </div>
          <div role="cell">
            <strong>{{ event.resourceName }}</strong
            ><small>{{ event.resourceType }}</small>
          </div>
          <StatusBadge role="cell" :state="event.outcome" />
        </article>
      </div>
    </AsyncState>
  </PageFrame>
</template>

<style scoped>
.audit-search {
  max-width: 520px;
  margin-bottom: 18px;
}
.audit-table {
  display: grid;
  border: 1px solid var(--border);
  border-radius: 10px;
  overflow: hidden;
  background: var(--surface);
}
.audit-table__header,
.audit-table__row {
  display: grid;
  grid-template-columns: 160px 1fr 1.2fr 1fr auto;
  gap: 12px;
  align-items: center;
  padding: 12px 14px;
}
.audit-table__header {
  background: var(--panel);
  border-bottom: 1px solid var(--border);
}
.audit-table__row + .audit-table__row {
  border-top: 1px solid var(--border);
}
.audit-table__row div {
  display: grid;
  gap: 3px;
}
.audit-table small {
  color: var(--muted);
}
@media (max-width: 800px) {
  .audit-table__header {
    display: none;
  }
  .audit-table__row {
    grid-template-columns: 1fr auto;
  }
  .audit-table__row > * {
    grid-column: 1/-1;
  }
  .audit-table__row .status-badge {
    grid-column: 2;
    grid-row: 1;
  }
  .audit-table__row time {
    grid-column: 1;
    grid-row: 1;
  }
}
</style>
