<script setup lang="ts">
import {
  Archive,
  Bot,
  CalendarClock,
  History,
  Pause,
  Pencil,
  Play,
  Plus,
  Search,
  Workflow,
} from "@lucide/vue";
import { computed, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

import AutomationArchiveDialog from "@/features/automations/AutomationArchiveDialog.vue";
import AutomationEditorDialog from "@/features/automations/AutomationEditorDialog.vue";
import {
  scheduleCapabilities,
  verifyScheduleCommandReadback,
  verifyScheduleReadback,
} from "@/features/automations/model";
import { usePlatformStore } from "@/features/platform/store";
import type {
  Schedule,
  ScheduleCommand,
  ScheduleInput,
} from "@/shared/api/generated/openapi/types.gen";
import { AppProblem, asProblem } from "@/shared/api/problem";
import AsyncState from "@/shared/ui/AsyncState.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

const props = defineProps<{ projectRef: string }>();
const platform = usePlatformStore();
const { locale, t } = useI18n();
const search = ref("");
const state = ref<"ALL" | Schedule["state"]>("ALL");
const selectedRef = ref("");
const editorOpen = ref(false);
const editorScheduleRef = ref("");
const editorBusy = ref(false);
const editorProblem = ref<AppProblem>();
const commandBusy = ref("");
const archiveScheduleRef = ref("");
const problem = ref<AppProblem>();
const selectedSection = ref<"OVERVIEW" | "VERSIONS" | "RUNS">("OVERVIEW");

const project = computed(() => platform.projects[props.projectRef]);
const canCreate = computed(() =>
  project.value?.nextActions.includes("CREATE_SCHEDULE"),
);
const schedules = computed(() =>
  Object.values(platform.schedules)
    .filter((schedule) => schedule.projectRef === props.projectRef)
    .sort((left, right) => left.name.localeCompare(right.name, locale.value)),
);
const filteredSchedules = computed(() => {
  const normalized = search.value.trim().toLocaleLowerCase(locale.value);
  return schedules.value.filter(
    (schedule) =>
      (state.value === "ALL" || schedule.state === state.value) &&
      (!normalized ||
        schedule.name.toLocaleLowerCase(locale.value).includes(normalized) ||
        schedule.target.displayName
          .toLocaleLowerCase(locale.value)
          .includes(normalized)),
  );
});
const selectedSchedule = computed(() =>
  schedules.value.find((schedule) => schedule.ref === selectedRef.value),
);
const selectedCapabilities = computed(() =>
  selectedSchedule.value
    ? scheduleCapabilities(selectedSchedule.value)
    : undefined,
);
const editorSchedule = computed(() =>
  schedules.value.find((schedule) => schedule.ref === editorScheduleRef.value),
);
const archiveSchedule = computed(() =>
  schedules.value.find((schedule) => schedule.ref === archiveScheduleRef.value),
);
const agents = computed(() =>
  Object.values(platform.agents)
    .filter(
      (agent) =>
        agent.projectRef === props.projectRef && agent.enabled && !agent.system,
    )
    .sort((left, right) => left.name.localeCompare(right.name, locale.value)),
);
const workflows = computed(() =>
  Object.values(platform.workflows)
    .filter(
      (workflow) =>
        workflow.projectRef === props.projectRef &&
        workflow.state === "PUBLISHED",
    )
    .sort((left, right) => left.name.localeCompare(right.name, locale.value)),
);
const custom = computed(() =>
  locale.value.startsWith("en")
    ? {
        actions: "Automation actions",
        allStates: "All states",
        archive: "Archive",
        archiveDescription:
          "Future runs will be cancelled. The automation and its history will remain available read-only.",
        archiveTitle: "Archive automation?",
        edit: "Edit automation",
        permanentDeleteUnavailable: "Permanent deletion is unavailable",
        permanentDeleteUnavailableText:
          "The current API supports safe archiving with read-only history, but not permanent deletion.",
        lastResult: "Last result",
        list: "Project automations",
        noMatches: "No matching automations",
        noMatchesText: "Change the search text or state filter.",
        schedule: "Schedule",
        search: "Search by name or target",
        target: "Target",
        version: "Version",
      }
    : {
        actions: "Действия с автоматизацией",
        allStates: "Все состояния",
        archive: "Архивировать",
        archiveDescription:
          "Будущие запуски будут отменены. Автоматизация и её история останутся доступны только для чтения.",
        archiveTitle: "Архивировать автоматизацию?",
        edit: "Изменить автоматизацию",
        permanentDeleteUnavailable: "Безвозвратное удаление недоступно",
        permanentDeleteUnavailableText:
          "Текущий API поддерживает безопасную архивацию с историей только для чтения, но не безвозвратное удаление.",
        lastResult: "Последний результат",
        list: "Автоматизации Проекта",
        noMatches: "Подходящих автоматизаций нет",
        noMatchesText: "Измените строку поиска или фильтр состояния.",
        schedule: "Расписание",
        search: "Поиск по названию и цели",
        target: "Цель",
        version: "Версия",
      },
);

watch(
  filteredSchedules,
  (values) => {
    if (!values.some((schedule) => schedule.ref === selectedRef.value))
      selectedRef.value = values[0]?.ref ?? "";
  },
  { immediate: true },
);

watch(selectedRef, () => {
  selectedSection.value = "OVERVIEW";
});

function scheduleLabel(schedule: Schedule): string {
  const preset = t(`automations.presetValue.${schedule.preset}`);
  const day = schedule.dayOfWeek
    ? ` · ${t(`automations.day.${schedule.dayOfWeek}`)}`
    : "";
  const time =
    schedule.preset === "HOURLY" ? "" : ` · ${schedule.timeOfDay ?? ""}`;
  return `${preset}${day}${time}`;
}

function formatDate(value: string): string {
  return new Intl.DateTimeFormat(locale.value, {
    dateStyle: "medium",
    timeStyle: "short",
  }).format(new Date(value));
}

function task(schedule: Schedule): string {
  const value = schedule.input?.task;
  return typeof value === "string" ? value : t("common.noData");
}

async function loadReadback(ref: string): Promise<Schedule | undefined> {
  await platform.loadSchedules(props.projectRef);
  const loadProblem = platform.problems.schedules;
  if (loadProblem) throw loadProblem;
  return platform.schedules[ref];
}

function openCreate(): void {
  editorScheduleRef.value = "";
  editorProblem.value = undefined;
  editorOpen.value = true;
}

function openEdit(schedule: Schedule): void {
  if (!scheduleCapabilities(schedule).canEdit) return;
  editorScheduleRef.value = schedule.ref;
  editorProblem.value = undefined;
  editorOpen.value = true;
}

async function submitEditor(
  input: ScheduleInput,
  current?: Schedule,
): Promise<void> {
  if (!current && !canCreate.value) return;
  editorBusy.value = true;
  editorProblem.value = undefined;
  try {
    const mutationResult = await platform.saveSchedule(
      props.projectRef,
      input,
      current,
    );
    verifyScheduleReadback(
      input,
      mutationResult,
      await loadReadback(mutationResult.ref),
    );
    selectedRef.value = mutationResult.ref;
    editorOpen.value = false;
  } catch (error) {
    const nextProblem = asProblem(error);
    if (nextProblem.kind === "conflict") {
      await platform.loadSchedules(props.projectRef);
      editorOpen.value = false;
      problem.value = nextProblem;
    } else editorProblem.value = nextProblem;
  } finally {
    editorBusy.value = false;
  }
}

async function command(
  schedule: Schedule,
  action: ScheduleCommand["action"],
): Promise<void> {
  const requiredAction =
    action === "PAUSE"
      ? "DISABLE"
      : action === "ARCHIVE"
        ? "ARCHIVE"
        : "ENABLE";
  if (!schedule.nextActions.includes(requiredAction)) return;
  commandBusy.value = schedule.ref;
  problem.value = undefined;
  try {
    const mutationResult = await platform.changeSchedule(schedule, action);
    verifyScheduleCommandReadback(
      mutationResult,
      await loadReadback(mutationResult.ref),
    );
  } catch (error) {
    const nextProblem = asProblem(error);
    if (nextProblem.kind === "conflict")
      await platform.loadSchedules(props.projectRef);
    problem.value = nextProblem;
  } finally {
    commandBusy.value = "";
  }
}

function requestArchive(schedule: Schedule): void {
  archiveScheduleRef.value = schedule.ref;
}

async function confirmArchive(): Promise<void> {
  const schedule = archiveSchedule.value;
  if (!schedule) return;
  await command(schedule, "ARCHIVE");
  if (!problem.value) archiveScheduleRef.value = "";
}

onMounted(
  () =>
    void Promise.all([
      platform.loadSchedules(props.projectRef),
      platform.loadAgents(props.projectRef),
      platform.loadWorkflows(props.projectRef),
      platform.loadProject(props.projectRef),
    ]),
);
</script>

<template>
  <section class="automations-workspace" :aria-label="custom.list">
    <div class="automations-workspace__toolbar">
      <label class="automations-workspace__search">
        <Search :size="16" aria-hidden="true" />
        <span class="sr-only">{{ custom.search }}</span>
        <input v-model="search" type="search" :placeholder="custom.search" />
      </label>
      <label>
        <span class="sr-only">{{ $t("common.status") }}</span>
        <select v-model="state" :aria-label="$t('common.status')">
          <option value="ALL">{{ custom.allStates }}</option>
          <option value="ACTIVE">{{ $t("states.ACTIVE") }}</option>
          <option value="PAUSED">{{ $t("states.PAUSED") }}</option>
          <option value="NEEDS_ATTENTION">
            {{ $t("states.NEEDS_ATTENTION") }}
          </option>
          <option value="ARCHIVED">{{ $t("states.ARCHIVED") }}</option>
        </select>
      </label>
      <span class="automations-workspace__count mono">
        {{ filteredSchedules.length }}
      </span>
      <button
        v-if="canCreate"
        class="button button--primary"
        type="button"
        :disabled="agents.length + workflows.length === 0"
        @click="openCreate"
      >
        <Plus :size="16" aria-hidden="true" />
        {{ $t("automations.new") }}
      </button>
    </div>

    <ProblemNotice v-if="problem" :problem="problem" compact />
    <AsyncState
      :loading="platform.loading.schedules"
      :problem="platform.problems.schedules"
      :empty="schedules.length === 0"
      :empty-title="$t('automations.emptyTitle')"
      :empty-text="$t('automations.emptyText')"
      @retry="platform.loadSchedules(projectRef)"
    >
      <section
        v-if="filteredSchedules.length === 0"
        class="empty-state automations-workspace__empty"
      >
        <h2>{{ custom.noMatches }}</h2>
        <p>{{ custom.noMatchesText }}</p>
      </section>
      <div v-else class="automations-workspace__layout">
        <div class="automations-list" role="list">
          <div class="automations-list__head desktop-only" aria-hidden="true">
            <span>{{ $t("common.name") }} · {{ custom.target }}</span>
            <span>{{ custom.schedule }}</span>
            <span>{{ $t("common.status") }} · {{ custom.version }}</span>
            <span>{{ $t("automations.nextRun") }}</span>
            <span>{{ custom.lastResult }}</span>
          </div>
          <button
            v-for="schedule in filteredSchedules"
            :key="schedule.ref"
            class="automation-row"
            :class="{
              'automation-row--selected': selectedRef === schedule.ref,
            }"
            type="button"
            role="listitem"
            @click="selectedRef = schedule.ref"
            @dblclick="openEdit(schedule)"
          >
            <span class="automation-row__identity">
              <strong>{{ schedule.name }}</strong>
              <small>
                <Bot
                  v-if="schedule.target.type === 'AGENT'"
                  :size="14"
                  aria-hidden="true"
                />
                <Workflow v-else :size="14" aria-hidden="true" />
                {{ schedule.target.displayName }}
              </small>
            </span>
            <span class="automation-row__schedule">
              <strong>{{ scheduleLabel(schedule) }}</strong>
              <small>{{ schedule.timezone }}</small>
            </span>
            <span class="automation-row__state">
              <StatusBadge :state="schedule.state" />
              <small class="mono">v{{ schedule.version }}</small>
            </span>
            <span class="automation-row__next">
              {{ schedule.nextRunAt ? formatDate(schedule.nextRunAt) : "—" }}
            </span>
            <span class="automation-row__outcome">
              {{ schedule.lastOutcome || "—" }}
            </span>
          </button>
        </div>

        <aside v-if="selectedSchedule" class="automation-details">
          <header>
            <div>
              <h2>{{ selectedSchedule.name }}</h2>
              <div class="automation-details__status">
                <StatusBadge :state="selectedSchedule.state" />
                <span class="mono">v{{ selectedSchedule.version }}</span>
              </div>
            </div>
            <CalendarClock :size="22" aria-hidden="true" />
          </header>
          <nav
            class="automation-details__tabs"
            :aria-label="$t('automations.sectionsLabel')"
          >
            <button
              v-for="section in ['OVERVIEW', 'VERSIONS', 'RUNS'] as const"
              :key="section"
              class="automation-details__tab"
              :class="{
                'automation-details__tab--active': selectedSection === section,
              }"
              type="button"
              :aria-current="selectedSection === section ? 'page' : undefined"
              @click="selectedSection = section"
            >
              {{ $t(`automations.sections.${section}`) }}
            </button>
          </nav>

          <dl v-if="selectedSection === 'OVERVIEW'">
            <div>
              <dt>{{ custom.target }}</dt>
              <dd>{{ selectedSchedule.target.displayName }}</dd>
            </div>
            <div>
              <dt>{{ custom.schedule }}</dt>
              <dd>
                {{ scheduleLabel(selectedSchedule) }} ·
                {{ selectedSchedule.timezone }}
              </dd>
            </div>
            <div>
              <dt>{{ $t("common.input") }}</dt>
              <dd>{{ task(selectedSchedule) }}</dd>
            </div>
            <div>
              <dt>{{ $t("automations.sessionPolicy") }}</dt>
              <dd>
                {{
                  $t(
                    selectedSchedule.sessionPolicy === "NEW_EACH_RUN"
                      ? "automations.newSession"
                      : "automations.continueSession",
                  )
                }}
              </dd>
            </div>
            <div>
              <dt>{{ $t("automations.notifications") }}</dt>
              <dd>
                {{
                  $t(
                    selectedSchedule.notificationPolicy ===
                      "CONTROL_CENTER_ONLY"
                      ? "automations.controlCenterOnly"
                      : "automations.optionalChannels",
                  )
                }}
              </dd>
            </div>
            <div>
              <dt>{{ $t("automations.nextRun") }}</dt>
              <dd>
                {{
                  selectedSchedule.nextRunAt
                    ? formatDate(selectedSchedule.nextRunAt)
                    : "—"
                }}
              </dd>
            </div>
            <div>
              <dt>{{ custom.lastResult }}</dt>
              <dd>{{ selectedSchedule.lastOutcome || "—" }}</dd>
            </div>
          </dl>
          <section
            v-else-if="selectedSection === 'VERSIONS'"
            class="automation-details__history"
            :aria-labelledby="`schedule-versions-${selectedSchedule.ref}`"
          >
            <div class="automation-details__history-heading">
              <History :size="18" aria-hidden="true" />
              <h3 :id="`schedule-versions-${selectedSchedule.ref}`">
                {{ $t("automations.versionHistory") }}
              </h3>
            </div>
            <article class="automation-details__revision">
              <div>
                <strong>
                  {{
                    $t("automations.currentVersion", {
                      version: selectedSchedule.version,
                    })
                  }}
                </strong>
                <StatusBadge :state="selectedSchedule.state" />
              </div>
              <p>{{ scheduleLabel(selectedSchedule) }}</p>
              <p>{{ task(selectedSchedule) }}</p>
            </article>
            <div class="automation-details__unavailable" role="note">
              <strong>{{ $t("automations.versionHistoryUnavailable") }}</strong>
              <p>{{ $t("automations.versionHistoryUnavailableText") }}</p>
            </div>
          </section>
          <section
            v-else
            class="automation-details__history"
            :aria-labelledby="`schedule-runs-${selectedSchedule.ref}`"
          >
            <div class="automation-details__history-heading">
              <History :size="18" aria-hidden="true" />
              <h3 :id="`schedule-runs-${selectedSchedule.ref}`">
                {{ $t("automations.runHistory") }}
              </h3>
            </div>
            <dl class="automation-details__run-summary">
              <div>
                <dt>{{ $t("automations.nextRun") }}</dt>
                <dd>
                  {{
                    selectedSchedule.nextRunAt
                      ? formatDate(selectedSchedule.nextRunAt)
                      : $t("automations.notScheduled")
                  }}
                </dd>
              </div>
              <div>
                <dt>{{ custom.lastResult }}</dt>
                <dd>
                  {{
                    selectedSchedule.lastOutcome || $t("automations.noRunsYet")
                  }}
                </dd>
              </div>
            </dl>
            <div class="automation-details__unavailable" role="note">
              <strong>{{ $t("automations.runHistoryUnavailable") }}</strong>
              <p>{{ $t("automations.runHistoryUnavailableText") }}</p>
            </div>
          </section>
          <div class="automation-details__actions" :aria-label="custom.actions">
            <button
              v-if="selectedCapabilities?.canEdit"
              class="button button--primary"
              type="button"
              @click="openEdit(selectedSchedule)"
            >
              <Pencil :size="16" aria-hidden="true" />
              {{ custom.edit }}
            </button>
            <button
              v-if="selectedCapabilities?.canPause"
              class="button"
              type="button"
              :disabled="commandBusy === selectedSchedule.ref"
              @click="command(selectedSchedule, 'PAUSE')"
            >
              <Pause :size="16" aria-hidden="true" />
              {{ $t("automations.pause") }}
            </button>
            <button
              v-if="selectedCapabilities?.canEnable"
              class="button"
              type="button"
              :disabled="commandBusy === selectedSchedule.ref"
              @click="command(selectedSchedule, 'ENABLE')"
            >
              <Play :size="16" aria-hidden="true" />
              {{ $t("common.enable") }}
            </button>
            <button
              v-if="selectedCapabilities?.canArchive"
              class="button button--danger"
              type="button"
              @click="requestArchive(selectedSchedule)"
            >
              <Archive :size="16" aria-hidden="true" />
              {{ custom.archive }}
            </button>
          </div>
          <div
            v-if="!selectedCapabilities?.canDeletePermanently"
            class="automation-details__unavailable"
            role="note"
          >
            <strong>{{ custom.permanentDeleteUnavailable }}</strong>
            <p>{{ custom.permanentDeleteUnavailableText }}</p>
          </div>
        </aside>
      </div>
    </AsyncState>

    <AutomationEditorDialog
      v-if="editorOpen"
      :agents="agents"
      :busy="editorBusy"
      :problem="editorProblem"
      :schedule="editorSchedule"
      :workflows="workflows"
      @close="editorOpen = false"
      @submit="submitEditor"
    />
    <AutomationArchiveDialog
      v-if="archiveSchedule"
      :schedule="archiveSchedule"
      :title="custom.archiveTitle"
      :description="custom.archiveDescription"
      :cancel-label="$t('common.cancel')"
      :confirm-label="custom.archive"
      :busy="commandBusy === archiveSchedule.ref"
      @close="archiveScheduleRef = ''"
      @confirm="confirmArchive"
    />
  </section>
</template>

<style scoped>
.automations-workspace {
  min-height: 620px;
  overflow: hidden;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--surface);
}
.automations-workspace__toolbar {
  display: flex;
  min-height: 58px;
  align-items: center;
  gap: 8px;
  padding: 10px 14px;
  border-bottom: 1px solid var(--border);
}
.automations-workspace__toolbar select {
  min-height: 36px;
}
.automations-workspace__search {
  display: flex;
  width: 280px;
  align-items: center;
  gap: 7px;
  padding: 0 9px;
  border: 1px solid var(--border-strong);
  border-radius: 6px;
}
.automations-workspace__search input {
  width: 100%;
  min-height: 34px;
  padding: 0;
  border: 0;
  outline: 0;
}
.automations-workspace__count {
  margin-left: auto;
  color: var(--muted);
}
.automations-workspace > .problem-notice {
  margin: 10px 14px 0;
}
.automations-workspace__layout {
  display: grid;
  min-height: 540px;
  grid-template-columns: minmax(0, 1fr) 360px;
}
.automations-list {
  min-width: 700px;
  max-height: 68vh;
  overflow: auto;
}
.automations-list__head,
.automation-row {
  display: grid;
  grid-template-columns:
    minmax(230px, 1.35fr) minmax(160px, 0.9fr) 126px 138px
    minmax(110px, 0.7fr);
  gap: 12px;
  align-items: center;
}
.automations-list__head {
  position: sticky;
  z-index: 2;
  top: 0;
  min-height: 40px;
  padding: 0 14px;
  border-bottom: 1px solid var(--border);
  background: var(--panel);
  color: var(--subtle);
  font-size: 0.72rem;
  font-weight: 600;
}
.automation-row {
  width: 100%;
  min-height: 72px;
  padding: 9px 14px;
  border: 0;
  border-bottom: 1px solid var(--hairline);
  background: var(--surface);
  color: inherit;
  text-align: left;
  cursor: pointer;
}
.automation-row:hover,
.automation-row--selected {
  background: var(--accent-soft);
}
.automation-row > span {
  min-width: 0;
}
.automation-row strong,
.automation-row small {
  display: block;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.automation-row small,
.automation-row__next,
.automation-row__outcome {
  color: var(--muted);
  font-size: 0.78rem;
}
.automation-row__identity small {
  display: flex;
  align-items: center;
  gap: 5px;
  margin-top: 4px;
}
.automation-row__state {
  display: grid;
  gap: 4px;
}
.automation-row__state small {
  padding-left: 2px;
}
.automation-details {
  max-height: 68vh;
  overflow: auto;
  padding: 16px;
  border-left: 1px solid var(--border);
}
.automation-details header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
  padding-bottom: 14px;
  border-bottom: 1px solid var(--border);
}
.automation-details header h2 {
  margin: 0;
  overflow-wrap: anywhere;
  font-size: 1.05rem;
}
.automation-details header > svg {
  color: var(--accent);
}
.automation-details__status {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 8px;
}
.automation-details dl {
  margin: 0;
}
.automation-details__tabs {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 4px;
  padding: 10px 0;
  border-bottom: 1px solid var(--border);
}
.automation-details__tab {
  min-height: 36px;
  padding: 6px 8px;
  border: 0;
  border-radius: 6px;
  background: transparent;
  color: var(--muted);
  font: inherit;
  font-size: 0.78rem;
  cursor: pointer;
}
.automation-details__tab:hover,
.automation-details__tab--active {
  background: var(--accent-soft);
  color: var(--accent);
}
.automation-details__tab--active {
  font-weight: 700;
}
.automation-details dl div {
  display: grid;
  grid-template-columns: 112px minmax(0, 1fr);
  gap: 10px;
  padding: 11px 0;
  border-bottom: 1px solid var(--hairline);
}
.automation-details dt {
  color: var(--subtle);
  font-size: 0.76rem;
}
.automation-details dd {
  margin: 0;
  overflow-wrap: anywhere;
}
.automation-details__actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  padding-top: 14px;
}
.automation-details__history {
  display: grid;
  gap: 12px;
  padding-top: 14px;
}
.automation-details__history-heading,
.automation-details__revision > div {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}
.automation-details__history-heading {
  justify-content: flex-start;
  color: var(--accent);
}
.automation-details__history-heading h3 {
  margin: 0;
  color: var(--text);
  font-size: 0.9rem;
}
.automation-details__revision {
  padding: 12px;
  border: 1px solid var(--border);
  border-radius: 7px;
}
.automation-details__revision p {
  margin: 8px 0 0;
  color: var(--muted);
  font-size: 0.78rem;
  overflow-wrap: anywhere;
}
.automation-details dl.automation-details__run-summary div {
  grid-template-columns: 98px minmax(0, 1fr);
}
.automation-details__unavailable {
  margin-top: 14px;
  padding: 12px;
  border: 1px solid var(--border);
  border-radius: 7px;
  background: var(--panel);
}
.automation-details__unavailable p {
  margin: 4px 0 0;
  color: var(--muted);
  font-size: 0.78rem;
}
.automations-workspace__empty {
  margin: 16px;
}
@media (max-width: 980px) {
  .automations-workspace__layout {
    grid-template-columns: minmax(0, 1fr) 300px;
  }
}
@media (max-width: 760px) {
  .automations-workspace {
    min-height: 0;
    overflow: visible;
    border-right: 0;
    border-left: 0;
    border-radius: 0;
  }
  .automations-workspace__toolbar {
    flex-wrap: wrap;
    padding: 10px 0;
  }
  .automations-workspace__search {
    width: 100%;
  }
  .automations-workspace__count {
    margin-left: auto;
  }
  .automations-workspace__layout {
    display: block;
    min-height: 0;
  }
  .automations-list {
    min-width: 0;
    max-height: none;
    overflow: visible;
  }
  .automation-row {
    display: grid;
    grid-template-columns: minmax(0, 1fr) auto;
    gap: 7px 10px;
    min-height: 132px;
    padding: 12px 2px;
  }
  .automation-row__identity {
    grid-column: 1 / -1;
  }
  .automation-row__schedule {
    grid-column: 1;
  }
  .automation-row__state {
    grid-column: 2;
    grid-row: 2;
    justify-items: end;
  }
  .automation-row__next {
    grid-column: 1 / -1;
  }
  .automation-row__outcome {
    display: none;
  }
  .automation-details {
    max-height: none;
    padding: 16px 0;
    border-top: 1px solid var(--border);
    border-left: 0;
  }
  .automation-details__actions .button {
    min-height: 44px;
  }
}
</style>
