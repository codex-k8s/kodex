<script setup lang="ts">
import {
  AlertTriangle,
  DatabaseBackup,
  GitPullRequestArrow,
  Play,
  RefreshCw,
} from "@lucide/vue";
import { computed, onMounted } from "vue";
import { useI18n } from "vue-i18n";

import { useOverviewStore } from "@/features/overview/store";
import AsyncPanel from "@/shared/ui/AsyncPanel.vue";
import PageHeader from "@/shared/ui/PageHeader.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";
import { formatDateTime } from "@/shared/lib/format";
import type { RemotePhase } from "@/shared/lib/remote";

const { locale } = useI18n();
const overview = useOverviewStore();

const phase = computed<RemotePhase>(() => {
  const phases = [
    overview.projects.phase,
    overview.runs.phase,
    overview.gates.phase,
    overview.incidents.phase,
    overview.backups.phase,
  ];
  if (phases.some((item) => item === "error")) return "error";
  if (phases.some((item) => item === "forbidden")) return "forbidden";
  if (phases.some((item) => item === "loading" || item === "idle"))
    return "loading";
  return "ready";
});
const attention = computed(
  () =>
    overview.gates.data.length +
    overview.incidents.data.length +
    overview.backups.data.filter((item) => item.restorable).length,
);

async function load(): Promise<void> {
  await overview.load();
}
onMounted(load);
</script>

<template>
  <div class="page">
    <PageHeader
      :title="$t('overview.title')"
      :subtitle="$t('overview.subtitle')"
    >
      <template #actions
        ><button class="button button--secondary" type="button" @click="load">
          <RefreshCw :size="15" aria-hidden="true" />{{ $t("common.refresh") }}
        </button></template
      >
    </PageHeader>
    <AsyncPanel :phase="phase" @retry="load">
      <div class="summary-strip">
        <div class="summary-stat">
          <strong>{{ overview.projects.data.length }}</strong
          ><span>{{ $t("overview.workspaces") }}</span>
        </div>
        <div class="summary-stat">
          <strong>{{ overview.runs.data.length }}</strong
          ><span>{{ $t("overview.activeRuns") }}</span>
        </div>
        <div class="summary-stat">
          <strong>{{ overview.gates.data.length }}</strong
          ><span>{{ $t("overview.gates") }}</span>
        </div>
        <div class="summary-stat">
          <strong>{{ overview.incidents.data.length }}</strong
          ><span>{{ $t("overview.incidents") }}</span>
        </div>
      </div>
      <div class="split-layout">
        <section class="panel">
          <header class="panel__header">
            <h2>{{ $t("overview.attention") }}</h2>
            <span class="badge badge--warning">{{ attention }}</span>
          </header>
          <div v-if="attention === 0" class="state-panel state-panel--quiet">
            {{ $t("common.empty") }}
          </div>
          <div v-else class="data-table-wrap">
            <table class="data-table">
              <tbody>
                <tr v-for="gate in overview.gates.data" :key="gate.id">
                  <td><GitPullRequestArrow :size="16" aria-hidden="true" /></td>
                  <td class="data-table__name">{{ gate.name }}</td>
                  <td><StatusBadge :state="gate.state" /></td>
                  <td>{{ formatDateTime(gate.updatedAt, locale) }}</td>
                </tr>
                <tr
                  v-for="incident in overview.incidents.data"
                  :key="incident.incidentRef"
                >
                  <td><AlertTriangle :size="16" aria-hidden="true" /></td>
                  <td class="data-table__name">{{ incident.kind }}</td>
                  <td><StatusBadge :state="incident.severity" /></td>
                  <td>{{ formatDateTime(incident.occurredAt, locale) }}</td>
                </tr>
                <tr
                  v-for="backup in overview.backups.data.filter(
                    (item) => item.restorable,
                  )"
                  :key="backup.backupId"
                >
                  <td><DatabaseBackup :size="16" aria-hidden="true" /></td>
                  <td class="data-table__name">{{ backup.scope }}</td>
                  <td><StatusBadge :state="backup.state" /></td>
                  <td>{{ formatDateTime(backup.updatedAt, locale) }}</td>
                </tr>
              </tbody>
            </table>
          </div>
        </section>
        <section class="panel">
          <header class="panel__header">
            <h2>{{ $t("overview.platform") }}</h2>
          </header>
          <div class="panel__body section-stack">
            <div class="resource-card__header">
              <span>{{ $t("diagnostics.schemaVersion") }}</span
              ><strong>{{
                overview.diagnostics.data?.schemaVersion ?? "—"
              }}</strong>
            </div>
            <div class="resource-card__header">
              <span>{{ $t("diagnostics.pendingOutbox") }}</span
              ><strong>{{
                overview.diagnostics.data?.pendingOutboxEvents ?? "—"
              }}</strong>
            </div>
            <div class="resource-card__header">
              <span>{{ $t("diagnostics.activeLeases") }}</span
              ><strong>{{
                overview.diagnostics.data?.activeTurnLeases ?? "—"
              }}</strong>
            </div>
          </div>
        </section>
      </div>
      <section class="panel" style="margin-top: 18px">
        <header class="panel__header">
          <h2>{{ $t("overview.recentRuns") }}</h2>
          <Play :size="17" aria-hidden="true" />
        </header>
        <div
          v-if="overview.runs.data.length === 0"
          class="state-panel state-panel--quiet"
        >
          {{ $t("common.empty") }}
        </div>
        <div v-else class="data-table-wrap">
          <table class="data-table">
            <tbody>
              <tr
                v-for="run in overview.runs.data.slice(0, 8)"
                :key="run.runRef"
              >
                <td class="data-table__name">{{ run.displayName }}</td>
                <td><StatusBadge :state="run.state" /></td>
                <td>{{ formatDateTime(run.updatedAt, locale) }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>
    </AsyncPanel>
  </div>
</template>
