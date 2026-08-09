<script setup lang="ts">
import { DatabaseBackup, RefreshCw, RotateCcw } from "@lucide/vue";
import { onMounted, reactive, ref } from "vue";
import { useI18n } from "vue-i18n";

import { useWorkspaceRecoveryStore } from "@/features/workspace-recovery/store";
import type {
  Resource,
  WorkspaceRestoreView,
} from "@/shared/api/generated/openapi/types.gen";
import { formatDateTime, shortDigest } from "@/shared/lib/format";
import AsyncPanel from "@/shared/ui/AsyncPanel.vue";
import ModalDialog from "@/shared/ui/ModalDialog.vue";
import PageHeader from "@/shared/ui/PageHeader.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

const store = useWorkspaceRecoveryStore();
const { locale, t } = useI18n();
const backupOpen = ref(false);
const restoreOpen = ref(false);
const selectedBackup = ref<Resource | null>(null);
const backupForm = reactive({
  name: "",
  scope: "WORKSPACE" as "WORKSPACE" | "ALL_WORKSPACES",
  retainUntil: "",
});
const restoreName = ref("");

async function createBackup(): Promise<void> {
  if (!backupForm.retainUntil) return;
  const retainUntil = new Date(backupForm.retainUntil);
  if (Number.isNaN(retainUntil.getTime())) return;
  const ok = await store.createBackup(
    backupForm.name.trim(),
    backupForm.scope,
    retainUntil.toISOString(),
  );
  if (ok) backupOpen.value = false;
}

function beginRestore(backup: Resource): void {
  selectedBackup.value = backup;
  restoreName.value = `${backup.name}-restore`;
  restoreOpen.value = true;
}

async function createRestore(): Promise<void> {
  const backup = selectedBackup.value;
  if (!backup) return;
  const ok = await store.createRestore(backup, restoreName.value.trim());
  if (ok) restoreOpen.value = false;
}

async function backupAction(
  backup: Resource,
  action: "CANCEL" | "RETRY",
): Promise<void> {
  if (
    !window.confirm(t("backups.confirmAction", { action, name: backup.name }))
  )
    return;
  await store.executeBackup(
    backup,
    action,
    action === "CANCEL" ? "OWNER_REQUEST" : undefined,
  );
}

async function restoreAction(
  value: WorkspaceRestoreView,
  action: "CANCEL" | "RETRY",
): Promise<void> {
  if (
    !window.confirm(
      t("backups.confirmAction", { action, name: value.displayName }),
    )
  )
    return;
  await store.executeRestore(
    value,
    action,
    action === "CANCEL" ? "OWNER_REQUEST" : undefined,
  );
}

onMounted(store.loadWorkspaceRecovery);
</script>

<template>
  <div class="page">
    <PageHeader
      :title="$t('backups.title')"
      :subtitle="$t('backups.ownerSubtitle')"
      ><template #actions
        ><button
          class="button button--secondary"
          type="button"
          @click="store.loadWorkspaceRecovery"
        >
          <RefreshCw :size="15" aria-hidden="true" />{{
            $t("common.refresh")
          }}</button
        ><button
          class="button button--primary"
          type="button"
          @click="backupOpen = true"
        >
          <DatabaseBackup :size="15" aria-hidden="true" />{{
            $t("backups.create")
          }}
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
          :phase="store.workspaceBackups.phase"
          :problem="store.workspaceBackups.problem"
          @retry="store.loadWorkspaceRecovery"
          ><div class="data-table-wrap">
            <table class="data-table">
              <thead>
                <tr>
                  <th>{{ $t("common.name") }}</th>
                  <th>{{ $t("backups.scope") }}</th>
                  <th>{{ $t("backups.members") }}</th>
                  <th>{{ $t("common.state") }}</th>
                  <th>{{ $t("backups.retainUntil") }}</th>
                  <th>
                    <span class="sr-only">{{ $t("common.actions") }}</span>
                  </th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="item in store.workspaceBackups.data" :key="item.id">
                  <td class="data-table__name">{{ item.name }}</td>
                  <td>{{ item.spec.workspaceBackup?.scope }}</td>
                  <td>{{ item.spec.workspaceBackup?.memberCount }}</td>
                  <td>
                    <StatusBadge
                      :state="item.spec.workspaceBackup?.state ?? item.state"
                    />
                  </td>
                  <td>
                    {{
                      formatDateTime(
                        item.spec.workspaceBackup?.retainUntil,
                        locale,
                      )
                    }}
                  </td>
                  <td>
                    <div class="data-table__actions">
                      <button
                        v-if="item.spec.workspaceBackup?.state === 'AVAILABLE'"
                        class="button button--text"
                        type="button"
                        @click="beginRestore(item)"
                      >
                        <RotateCcw :size="14" aria-hidden="true" />{{
                          $t("backups.restore")
                        }}</button
                      ><button
                        v-if="item.spec.workspaceBackup?.state === 'VERIFYING'"
                        class="button button--text"
                        type="button"
                        @click="backupAction(item, 'CANCEL')"
                      >
                        {{ $t("common.cancel") }}</button
                      ><button
                        v-if="item.spec.workspaceBackup?.state === 'FAILED'"
                        class="button button--text"
                        type="button"
                        @click="backupAction(item, 'RETRY')"
                      >
                        {{ $t("common.retry") }}
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
          :phase="store.workspaceRestores.phase"
          :problem="store.workspaceRestores.problem"
          @retry="store.loadWorkspaceRecovery"
          ><div class="data-table-wrap">
            <table class="data-table">
              <thead>
                <tr>
                  <th>{{ $t("common.name") }}</th>
                  <th>{{ $t("backups.attempt") }}</th>
                  <th>{{ $t("backups.members") }}</th>
                  <th>{{ $t("common.state") }}</th>
                  <th>{{ $t("common.updatedAt") }}</th>
                  <th>
                    <span class="sr-only">{{ $t("common.actions") }}</span>
                  </th>
                </tr>
              </thead>
              <tbody>
                <tr
                  v-for="item in store.workspaceRestores.data"
                  :key="item.restoreRef"
                >
                  <td class="data-table__name">{{ item.displayName }}</td>
                  <td>{{ item.attempt }}</td>
                  <td>{{ item.memberCount }}</td>
                  <td><StatusBadge :state="item.state" /></td>
                  <td>{{ formatDateTime(item.updatedAt, locale) }}</td>
                  <td>
                    <div class="data-table__actions">
                      <button
                        v-for="nextAction in item.nextActions"
                        :key="nextAction"
                        class="button button--text"
                        type="button"
                        @click="restoreAction(item, nextAction)"
                      >
                        {{
                          nextAction === "CANCEL"
                            ? $t("common.cancel")
                            : $t("common.retry")
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
    </div>
    <ModalDialog
      :open="backupOpen"
      :title="$t('backups.create')"
      @close="backupOpen = false"
      ><form class="form-grid" @submit.prevent="createBackup">
        <label class="form-field"
          ><span>{{ $t("common.name") }}</span
          ><input v-model="backupForm.name" required maxlength="160" /></label
        ><label class="form-field"
          ><span>{{ $t("backups.scope") }}</span
          ><select v-model="backupForm.scope">
            <option value="WORKSPACE">WORKSPACE</option>
            <option value="ALL_WORKSPACES">ALL_WORKSPACES</option>
          </select></label
        ><label class="form-field form-field--full"
          ><span>{{ $t("backups.retainUntil") }}</span
          ><input
            v-model="backupForm.retainUntil"
            type="datetime-local"
            required
        /></label>
        <div class="button-row form-field--full">
          <button
            class="button button--primary"
            type="submit"
            :disabled="store.mutating"
          >
            {{ $t("backups.create") }}
          </button>
        </div>
      </form></ModalDialog
    >
    <ModalDialog
      :open="restoreOpen"
      :title="$t('backups.restore')"
      @close="restoreOpen = false"
      ><form class="form-grid" @submit.prevent="createRestore">
        <div class="summary-card form-field--full">
          <strong>{{ selectedBackup?.name }}</strong
          ><span
            >{{ $t("common.version", { version: selectedBackup?.version }) }} ·
            {{
              shortDigest(
                selectedBackup?.spec.workspaceBackup?.membershipSha256,
              )
            }}</span
          >
        </div>
        <label class="form-field form-field--full"
          ><span>{{ $t("common.name") }}</span
          ><input v-model="restoreName" required maxlength="160"
        /></label>
        <div class="button-row form-field--full">
          <button
            class="button button--danger"
            type="submit"
            :disabled="store.mutating"
          >
            {{ $t("backups.restore") }}
          </button>
        </div>
      </form></ModalDialog
    >
  </div>
</template>
