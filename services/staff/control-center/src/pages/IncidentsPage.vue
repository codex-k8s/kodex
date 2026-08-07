<script setup lang="ts">
import { RefreshCw, TriangleAlert } from "@lucide/vue";
import { onMounted } from "vue";
import { useI18n } from "vue-i18n";

import { useOperationsStore } from "@/features/operations/store";
import { formatDateTime } from "@/shared/lib/format";
import AsyncPanel from "@/shared/ui/AsyncPanel.vue";
import PageHeader from "@/shared/ui/PageHeader.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

const { locale } = useI18n();
const store = useOperationsStore();
onMounted(store.loadIncidents);
</script>

<template>
  <div class="page">
    <PageHeader
      :title="$t('incidents.title')"
      :subtitle="$t('incidents.subtitle')"
      ><template #actions
        ><button
          class="button button--secondary"
          type="button"
          @click="store.loadIncidents"
        >
          <RefreshCw :size="15" aria-hidden="true" />{{ $t("common.refresh") }}
        </button></template
      ></PageHeader
    >
    <section class="panel">
      <AsyncPanel
        :phase="store.incidents.phase"
        :problem="store.incidents.problem"
        @retry="store.loadIncidents"
        ><div class="data-table-wrap">
          <table class="data-table">
            <thead>
              <tr>
                <th>{{ $t("incidents.kind") }}</th>
                <th>{{ $t("incidents.workload") }}</th>
                <th>{{ $t("incidents.fence") }}</th>
                <th>{{ $t("incidents.occurredAt") }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="item in store.incidents.data" :key="item.incidentId">
                <td class="data-table__name">
                  <TriangleAlert :size="16" aria-hidden="true" /><StatusBadge
                    state="FAILED"
                  />{{ item.kind }}
                </td>
                <td>{{ item.workloadId }}</td>
                <td>{{ item.executionFence }}</td>
                <td>{{ formatDateTime(item.occurredAt, locale) }}</td>
              </tr>
            </tbody>
          </table>
        </div></AsyncPanel
      >
    </section>
  </div>
</template>
