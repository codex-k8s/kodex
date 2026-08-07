<script setup lang="ts">
import { GitCompareArrows, RefreshCw } from "@lucide/vue";
import { onMounted } from "vue";
import { useI18n } from "vue-i18n";

import { useOperationsStore } from "@/features/operations/store";
import { formatDateTime } from "@/shared/lib/format";
import AsyncPanel from "@/shared/ui/AsyncPanel.vue";
import PageHeader from "@/shared/ui/PageHeader.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

const { locale } = useI18n();
const store = useOperationsStore();
onMounted(store.loadChanges);
</script>

<template>
  <div class="page">
    <PageHeader
      :title="$t('configuration.title')"
      :subtitle="$t('configuration.subtitle')"
      ><template #actions
        ><button
          class="button button--secondary"
          type="button"
          @click="store.loadChanges"
        >
          <RefreshCw :size="15" aria-hidden="true" />{{ $t("common.refresh") }}
        </button></template
      ></PageHeader
    >
    <section class="panel">
      <AsyncPanel
        :phase="store.changes.phase"
        :problem="store.changes.problem"
        @retry="store.loadChanges"
        ><div class="data-table-wrap">
          <table class="data-table">
            <thead>
              <tr>
                <th>{{ $t("audit.action") }}</th>
                <th>{{ $t("audit.resource") }}</th>
                <th>{{ $t("audit.outcome") }}</th>
                <th>{{ $t("audit.policy") }}</th>
                <th>{{ $t("audit.occurredAt") }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="item in store.changes.data" :key="item.id">
                <td class="data-table__name">
                  <GitCompareArrows :size="15" aria-hidden="true" />{{
                    item.action
                  }}
                </td>
                <td>{{ item.resourceKind }} · v{{ item.resourceVersion }}</td>
                <td><StatusBadge :state="item.outcome" /></td>
                <td>v{{ item.policyRevision }}</td>
                <td>{{ formatDateTime(item.occurredAt, locale) }}</td>
              </tr>
            </tbody>
          </table>
        </div></AsyncPanel
      >
    </section>
  </div>
</template>
