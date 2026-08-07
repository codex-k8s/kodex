<script setup lang="ts">
import {
  CalendarClock,
  History,
  Pencil,
  Play,
  Plus,
  RefreshCw,
  Trash2,
} from "@lucide/vue";
import { onMounted, reactive, ref } from "vue";
import { useI18n } from "vue-i18n";

import ScheduleForm from "@/features/schedules/ScheduleForm.vue";
import { useSchedulesStore } from "@/features/schedules/store";
import type {
  Resource,
  ScheduleInput,
  ScheduleOccurrence,
} from "@/shared/api/generated/openapi/types.gen";
import { formatDateTime } from "@/shared/lib/format";
import AsyncPanel from "@/shared/ui/AsyncPanel.vue";
import ModalDialog from "@/shared/ui/ModalDialog.vue";
import PageHeader from "@/shared/ui/PageHeader.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

const { locale, t } = useI18n();
const store = useSchedulesStore();
const editorOpen = ref(false);
const editing = ref<Resource | null>(null);
const historyOpen = ref(false);
const historySchedule = ref<Resource | null>(null);
const recovery = reactive<
  Record<string, { action: "REPAIR" | "CANCEL" | "SKIP"; reasonCode: string }>
>({});

function openCreate(): void {
  editing.value = null;
  editorOpen.value = true;
}

function openEdit(resource: Resource): void {
  editing.value = resource;
  editorOpen.value = true;
}

async function submit(value: {
  name: string;
  input: ScheduleInput;
}): Promise<void> {
  const success = editing.value
    ? await store.update(editing.value, value)
    : await store.create(value);
  if (success) {
    editorOpen.value = false;
    editing.value = null;
  }
}

async function remove(resource: Resource): Promise<void> {
  if (window.confirm(t("schedules.confirmDelete", { name: resource.name })))
    await store.remove(resource);
}

async function openHistory(resource: Resource): Promise<void> {
  historySchedule.value = resource;
  historyOpen.value = true;
  await store.loadOccurrences(resource.id);
}

function recoveryForm(occurrence: ScheduleOccurrence): {
  action: "REPAIR" | "CANCEL" | "SKIP";
  reasonCode: string;
} {
  const existing = recovery[occurrence.occurrenceId];
  if (existing) return existing;
  const initial = { action: "REPAIR" as const, reasonCode: "" };
  recovery[occurrence.occurrenceId] = initial;
  return initial;
}

async function submitRecovery(occurrence: ScheduleOccurrence): Promise<void> {
  const schedule = historySchedule.value;
  const form = recoveryForm(occurrence);
  if (
    !schedule ||
    !occurrence.recoveryEvidenceSha256 ||
    !form.reasonCode.trim()
  )
    return;
  await store.recover(schedule, occurrence, {
    action: form.action,
    expectedAttempt: occurrence.attempt,
    recoveryEvidenceSha256: occurrence.recoveryEvidenceSha256,
    reasonCode: form.reasonCode.trim(),
  });
}

onMounted(store.load);
</script>

<template>
  <div class="page">
    <PageHeader
      :title="$t('schedules.title')"
      :subtitle="$t('schedules.subtitle')"
    >
      <template #actions>
        <button
          class="button button--secondary"
          type="button"
          @click="store.load"
        >
          <RefreshCw :size="15" aria-hidden="true" />{{ $t("common.refresh") }}
        </button>
        <button
          class="button button--primary"
          type="button"
          @click="openCreate"
        >
          <Plus :size="15" aria-hidden="true" />{{ $t("schedules.create") }}
        </button>
      </template>
    </PageHeader>
    <ProblemNotice :problem="store.mutationProblem" />
    <section class="panel" style="margin-top: 15px">
      <AsyncPanel
        :phase="store.schedules.phase"
        :problem="store.schedules.problem"
        @retry="store.load"
      >
        <div class="data-table-wrap">
          <table class="data-table">
            <thead>
              <tr>
                <th>{{ $t("common.name") }}</th>
                <th>{{ $t("common.state") }}</th>
                <th>{{ $t("schedules.target") }}</th>
                <th>{{ $t("schedules.nextRun") }}</th>
                <th>
                  <span class="sr-only">{{ $t("common.actions") }}</span>
                </th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="item in store.schedules.data" :key="item.id">
                <td class="data-table__name">
                  <CalendarClock :size="16" aria-hidden="true" />{{ item.name }}
                </td>
                <td><StatusBadge :state="item.state" /></td>
                <td>
                  {{
                    store.catalogs.data.targets.find(
                      (target) =>
                        target.id === item.spec.schedule?.targetResourceId,
                    )?.name ?? $t("common.noValue")
                  }}
                </td>
                <td>
                  {{ formatDateTime(item.spec.schedule?.nextRunAt, locale) }}
                </td>
                <td>
                  <div class="data-table__actions">
                    <button
                      class="icon-button"
                      type="button"
                      :aria-label="$t('schedules.runNow')"
                      :disabled="store.mutating"
                      @click="store.runNow(item)"
                    >
                      <Play :size="15" aria-hidden="true" />
                    </button>
                    <button
                      class="icon-button"
                      type="button"
                      :aria-label="$t('schedules.loadOccurrences')"
                      @click="openHistory(item)"
                    >
                      <History :size="15" aria-hidden="true" />
                    </button>
                    <button
                      class="icon-button"
                      type="button"
                      :aria-label="$t('common.edit')"
                      @click="openEdit(item)"
                    >
                      <Pencil :size="15" aria-hidden="true" />
                    </button>
                    <button
                      class="icon-button"
                      type="button"
                      :aria-label="$t('common.delete')"
                      @click="remove(item)"
                    >
                      <Trash2 :size="15" aria-hidden="true" />
                    </button>
                  </div>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </AsyncPanel>
    </section>

    <ModalDialog
      :open="editorOpen"
      :title="$t('schedules.editor')"
      @close="
        editorOpen = false;
        editing = null;
      "
    >
      <ProblemNotice :problem="store.mutationProblem" />
      <ScheduleForm
        :key="editing?.id ?? 'create'"
        :catalogs="store.catalogs.data"
        :initial="editing"
        :busy="store.mutating"
        @submit="submit"
      />
    </ModalDialog>

    <ModalDialog
      :open="historyOpen"
      :title="$t('schedules.occurrences')"
      @close="
        historyOpen = false;
        historySchedule = null;
      "
    >
      <AsyncPanel
        :phase="store.occurrences.phase"
        :problem="store.occurrences.problem"
        @retry="historySchedule && store.loadOccurrences(historySchedule.id)"
      >
        <div v-if="historySchedule" class="section-stack">
          <article
            v-for="item in store.occurrences.data[historySchedule.id] ?? []"
            :key="item.occurrenceId"
            class="resource-card"
          >
            <div class="resource-card__header">
              <strong>{{ formatDateTime(item.scheduledFor, locale) }}</strong
              ><StatusBadge :state="item.state" />
            </div>
            <div class="resource-card__meta">
              <span>{{ $t("schedules.attempt", { value: item.attempt }) }}</span
              ><span>{{ formatDateTime(item.availableAt, locale) }}</span>
            </div>
            <form
              v-if="
                item.state === 'RECOVERY_BLOCKED' && item.recoveryEvidenceSha256
              "
              class="form-grid"
              @submit.prevent="submitRecovery(item)"
            >
              <label class="form-field"
                ><span>{{ $t("schedules.recoveryAction") }}</span
                ><select v-model="recoveryForm(item).action">
                  <option value="REPAIR">{{ $t("schedules.repair") }}</option>
                  <option value="CANCEL">{{ $t("common.cancel") }}</option>
                  <option value="SKIP">{{ $t("schedules.skip") }}</option>
                </select></label
              >
              <label class="form-field"
                ><span>{{ $t("schedules.reasonCode") }}</span
                ><input
                  v-model="recoveryForm(item).reasonCode"
                  required
                  minlength="3"
                  maxlength="128"
                  autocomplete="off"
              /></label>
              <div class="button-row form-field--full">
                <button
                  class="button button--primary"
                  type="submit"
                  :disabled="store.mutating"
                >
                  {{ $t("common.confirm") }}
                </button>
              </div>
            </form>
          </article>
        </div>
      </AsyncPanel>
    </ModalDialog>
  </div>
</template>
