<script setup lang="ts">
import { RefreshCw } from "@lucide/vue";
import { onMounted } from "vue";
import { useI18n } from "vue-i18n";

import { useOperationsStore } from "@/features/operations/store";
import { formatDuration } from "@/shared/lib/format";
import AsyncPanel from "@/shared/ui/AsyncPanel.vue";
import PageHeader from "@/shared/ui/PageHeader.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

const store = useOperationsStore();
const { locale } = useI18n();
onMounted(store.loadDiagnostics);
</script>

<template>
  <div class="page">
    <PageHeader
      :title="$t('diagnostics.title')"
      :subtitle="$t('diagnostics.subtitle')"
      ><template #actions
        ><button
          class="button button--secondary"
          type="button"
          @click="store.loadDiagnostics"
        >
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
  </div>
</template>
