<script setup lang="ts">
import { DatabaseBackup, RefreshCw, RotateCcw } from "@lucide/vue";
import { onMounted } from "vue";
import { useI18n } from "vue-i18n";

import { useOperationsStore } from "@/features/operations/store";
import type { Backup } from "@/shared/api/generated/openapi/types.gen";
import { formatDateTime } from "@/shared/lib/format";
import AsyncPanel from "@/shared/ui/AsyncPanel.vue";
import PageHeader from "@/shared/ui/PageHeader.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

const { locale, t } = useI18n();
const store = useOperationsStore();

async function restore(backup: Backup): Promise<void> {
  if (backup.restorable && window.confirm(t("backups.confirmRestore")))
    await store.restore(backup);
}
async function load(): Promise<void> {
  await Promise.all([store.loadBackups(), store.loadRestores()]);
}
onMounted(load);
</script>

<template>
  <div class="page">
    <PageHeader :title="$t('backups.title')" :subtitle="$t('backups.subtitle')"
      ><template #actions
        ><button class="button button--secondary" type="button" @click="load">
          <RefreshCw :size="15" aria-hidden="true" />{{ $t("common.refresh") }}
        </button></template
      ></PageHeader
    >
    <ProblemNotice :problem="store.mutationProblem" />
    <div class="section-stack" style="margin-top: 15px">
      <section class="panel">
        <header class="panel__header">
          <h2>{{ $t("backups.backups") }}</h2>
        </header>
        <AsyncPanel
          :phase="store.backups.phase"
          :problem="store.backups.problem"
          @retry="store.loadBackups"
          ><div class="data-table-wrap">
            <table class="data-table">
              <thead>
                <tr>
                  <th>{{ $t("backups.scope") }}</th>
                  <th>{{ $t("common.revision") }}</th>
                  <th>{{ $t("common.state") }}</th>
                  <th>{{ $t("backups.retainUntil") }}</th>
                  <th>
                    <span class="sr-only">{{ $t("common.actions") }}</span>
                  </th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="item in store.backups.data" :key="item.backupId">
                  <td class="data-table__name">
                    <DatabaseBackup :size="16" aria-hidden="true" />{{
                      item.scope
                    }}
                  </td>
                  <td>v{{ item.sourceVersion }}</td>
                  <td><StatusBadge :state="item.state" /></td>
                  <td>{{ formatDateTime(item.retainUntil, locale) }}</td>
                  <td>
                    <div class="data-table__actions">
                      <button
                        v-if="item.restorable"
                        class="button button--text"
                        type="button"
                        :disabled="store.mutating"
                        @click="restore(item)"
                      >
                        <RotateCcw :size="14" aria-hidden="true" />{{
                          $t("backups.restore")
                        }}
                      </button>
                    </div>
                  </td>
                </tr>
              </tbody>
            </table>
          </div></AsyncPanel
        >
      </section>
      <section class="panel">
        <header class="panel__header">
          <h2>{{ $t("backups.restores") }}</h2>
        </header>
        <AsyncPanel
          :phase="store.restores.phase"
          :problem="store.restores.problem"
          @retry="store.loadRestores"
          ><div class="data-table-wrap">
            <table class="data-table">
              <thead>
                <tr>
                  <th>{{ $t("backups.scope") }}</th>
                  <th>{{ $t("common.state") }}</th>
                  <th>{{ $t("backups.nextAction") }}</th>
                  <th>{{ $t("common.updatedAt") }}</th>
                </tr>
              </thead>
              <tbody>
                <tr
                  v-for="item in store.restores.data"
                  :key="item.restoreOperationId"
                >
                  <td>{{ item.scope }}</td>
                  <td><StatusBadge :state="item.state" /></td>
                  <td>{{ item.nextAction }}</td>
                  <td>{{ formatDateTime(item.updatedAt, locale) }}</td>
                </tr>
              </tbody>
            </table>
          </div></AsyncPanel
        >
      </section>
    </div>
  </div>
</template>
