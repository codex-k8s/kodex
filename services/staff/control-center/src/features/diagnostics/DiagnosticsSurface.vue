<script setup lang="ts">
import { RefreshCw } from "@lucide/vue";
import { onMounted } from "vue";
import { useI18n } from "vue-i18n";

import { useDiagnosticsStore } from "@/features/diagnostics/store";
import { formatDateTime, formatDuration } from "@/shared/lib/format";
import AsyncPanel from "@/shared/ui/AsyncPanel.vue";
import PageHeader from "@/shared/ui/PageHeader.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

const store = useDiagnosticsStore();
const { locale } = useI18n();
const load = () => Promise.all([store.loadDiagnostics(), store.loadHealth()]);
onMounted(load);
</script>

<template>
  <div class="page">
    <PageHeader
      :title="$t('diagnostics.title')"
      :subtitle="$t('diagnostics.subtitle')"
      ><template #actions
        ><button class="button button--secondary" type="button" @click="load">
          <RefreshCw :size="15" aria-hidden="true" />{{ $t("common.refresh") }}
        </button></template
      ></PageHeader
    >
    <AsyncPanel
      :phase="store.diagnostics.phase"
      :problem="store.diagnostics.problem"
      @retry="store.loadDiagnostics"
    >
      <div v-if="store.diagnostics.data" class="metric-grid">
        <article class="metric">
          <span>{{ $t("diagnostics.schemaVersion") }}</span
          ><strong>v{{ store.diagnostics.data.schemaVersion }}</strong>
        </article>
        <article class="metric">
          <span>{{ $t("diagnostics.pendingOutbox") }}</span
          ><strong>{{ store.diagnostics.data.pendingOutboxEvents }}</strong>
        </article>
        <article class="metric">
          <span>{{ $t("diagnostics.terminalOutbox") }}</span
          ><strong>{{ store.diagnostics.data.terminalOutboxEvents }}</strong>
        </article>
        <article class="metric">
          <span>{{ $t("diagnostics.oldestPending") }}</span
          ><strong>{{
            formatDuration(
              store.diagnostics.data.oldestPendingAgeSeconds,
              locale,
            )
          }}</strong>
        </article>
        <article class="metric">
          <span>{{ $t("diagnostics.activeLeases") }}</span
          ><strong>{{ store.diagnostics.data.activeTurnLeases }}</strong>
        </article>
        <article class="metric">
          <span>{{ $t("diagnostics.queuedSchedules") }}</span
          ><strong>{{
            store.diagnostics.data.queuedScheduleOccurrences
          }}</strong>
        </article>
        <article class="metric">
          <span>{{ $t("diagnostics.principalStatus") }}</span
          ><StatusBadge
            :state="store.diagnostics.data.runtimePrincipalStatus"
          />
        </article>
        <article class="metric">
          <span>{{ $t("diagnostics.principalGeneration") }}</span
          ><strong>{{
            store.diagnostics.data.runtimePrincipalGeneration
          }}</strong>
        </article>
      </div>
    </AsyncPanel>
    <section class="panel" style="margin-top: 15px">
      <header class="panel__header">
        <h2>{{ $t("diagnostics.health") }}</h2>
      </header>
      <AsyncPanel
        :phase="store.health.phase"
        :problem="store.health.problem"
        @retry="store.loadHealth"
      >
        <div class="data-table-wrap">
          <table class="data-table">
            <thead>
              <tr>
                <th>{{ $t("diagnostics.component") }}</th>
                <th>{{ $t("diagnostics.source") }}</th>
                <th>{{ $t("common.state") }}</th>
                <th>{{ $t("diagnostics.value") }}</th>
                <th>{{ $t("diagnostics.observedAt") }}</th>
              </tr>
            </thead>
            <tbody>
              <tr
                v-for="item in store.health.data"
                :key="`${item.source}-${item.component}`"
              >
                <td class="data-table__name">{{ item.component }}</td>
                <td>{{ item.source }}</td>
                <td><StatusBadge :state="item.status" /></td>
                <td>{{ item.value }}</td>
                <td>{{ formatDateTime(item.observedAt, locale) }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </AsyncPanel>
    </section>
  </div>
</template>
