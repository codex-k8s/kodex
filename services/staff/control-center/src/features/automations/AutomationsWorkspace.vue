<script setup lang="ts">
import {
  Archive,
  Bot,
  CalendarClock,
  History,
  LoaderCircle,
  Pause,
  Pencil,
  Play,
  Plus,
  Search,
  Trash2,
  Workflow,
} from "@lucide/vue";
import {
  computed,
  onBeforeUnmount,
  onMounted,
  ref,
  watch,
  type WatchStopHandle,
} from "vue";
import { useI18n } from "vue-i18n";

import AutomationArchiveDialog from "@/features/automations/AutomationArchiveDialog.vue";
import AutomationEditorDialog from "@/features/automations/AutomationEditorDialog.vue";
import {
  commandSchedule,
  loadSchedulePage,
  loadScheduleRevisionPage,
  loadScheduleRunPage,
  readSchedule,
  removeSchedule,
  saveSchedule,
} from "@/features/automations/api";
import {
  scheduleCapabilities,
  scheduleMatchesFilter,
  type ScheduleFilter,
} from "@/features/automations/model";
import { usePlatformStore } from "@/features/platform/store";
import type {
  Schedule,
  ScheduleCommand,
  ScheduleInput,
  ScheduleRevision,
  ScheduleRunOccurrence,
} from "@/shared/api/generated/openapi/types.gen";
import { AppProblem, asProblem } from "@/shared/api/problem";
import AsyncState from "@/shared/ui/AsyncState.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

const props = defineProps<{ projectRef: string }>();
const platform = usePlatformStore();
const { locale, t } = useI18n();

const search = ref("");
const state = ref<ScheduleFilter>("CURRENT");
const schedules = ref<Schedule[]>([]);
const nextPageToken = ref<string>();
const listLoading = ref(false);
const moreLoading = ref(false);
const listProblem = ref<AppProblem>();
const selectedRef = ref("");
const selectedSection = ref<"OVERVIEW" | "VERSIONS" | "RUNS">("OVERVIEW");
const editorOpen = ref(false);
const editorScheduleRef = ref("");
const editorBusy = ref(false);
const editorProblem = ref<AppProblem>();
const commandBusy = ref("");
const archiveScheduleRef = ref("");
const deleteScheduleRef = ref("");
const problem = ref<AppProblem>();
const revisions = ref<ScheduleRevision[]>([]);
const revisionsToken = ref<string>();
const revisionsLoading = ref(false);
const revisionsProblem = ref<AppProblem>();
const runOccurrences = ref<ScheduleRunOccurrence[]>([]);
const runsToken = ref<string>();
const runsLoading = ref(false);
const runsProblem = ref<AppProblem>();
const listSentinel = ref<HTMLElement>();

let listController: AbortController | undefined;
let historyController: AbortController | undefined;
let searchTimer: ReturnType<typeof setTimeout> | undefined;
let listObserver: IntersectionObserver | undefined;
let stopSentinelWatch: WatchStopHandle | undefined;

const project = computed(() => platform.projects[props.projectRef]);
const canCreate = computed(() =>
  project.value?.nextActions.includes("CREATE_SCHEDULE"),
);
const filteredSchedules = computed(() =>
  schedules.value.filter((schedule) =>
    scheduleMatchesFilter(schedule, state.value),
  ),
);
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
const deleteCandidate = computed(() =>
  schedules.value.find((schedule) => schedule.ref === deleteScheduleRef.value),
);
const custom = computed(() =>
  locale.value.startsWith("en")
    ? {
        actions: "Automation actions",
        allStates: "All states",
        archive: "Archive",
        archiveDescription:
          "Future runs will be cancelled. Immutable revisions and run history remain available read-only.",
        archiveTitle: "Archive automation?",
        automationRef: "Automation ref",
        currentStates: "Current",
        current: "Current",
        createdAt: "Created",
        delete: "Delete",
        deleted: "Deleted",
        deleteDescription:
          "The automation will be terminally deleted. Immutable revisions and existing run history remain available read-only.",
        deleteTitle: "Delete automation permanently?",
        edit: "Edit automation",
        lastResult: "Last result",
        list: "Project automations",
        loadMore: "Load more",
        noMatches: "No matching automations",
        noMatchesText: "Change the search text or state filter.",
        noRevisions: "No immutable revisions were returned.",
        noRuns: "This automation has no runs yet.",
        revision: "Revision",
        revisionRef: "Revision ref",
        runRef: "Run ref",
        schedule: "Schedule",
        search: "Search automations on the server",
        target: "Target",
        version: "Resource version",
      }
    : {
        actions: "Действия с автоматизацией",
        allStates: "Все состояния",
        archive: "Архивировать",
        archiveDescription:
          "Будущие запуски будут отменены. Неизменяемые ревизии и история запусков останутся доступны только для чтения.",
        archiveTitle: "Архивировать автоматизацию?",
        automationRef: "Ссылка автоматизации",
        currentStates: "Действующие",
        current: "Текущая",
        createdAt: "Создана",
        delete: "Удалить",
        deleted: "Удалена",
        deleteDescription:
          "Автоматизация будет окончательно удалена. Неизменяемые ревизии и существующая история запусков останутся доступны только для чтения.",
        deleteTitle: "Удалить автоматизацию?",
        edit: "Изменить автоматизацию",
        lastResult: "Последний результат",
        list: "Автоматизации Проекта",
        loadMore: "Загрузить ещё",
        noMatches: "Подходящих автоматизаций нет",
        noMatchesText: "Измените строку поиска или фильтр состояния.",
        noRevisions: "Неизменяемые ревизии не найдены.",
        noRuns: "У этой автоматизации ещё нет запусков.",
        revision: "Ревизия",
        revisionRef: "Ссылка ревизии",
        runRef: "Ссылка запуска",
        schedule: "Расписание",
        search: "Поиск автоматизаций на сервере",
        target: "Цель",
        version: "Версия ресурса",
      },
);

function mergeByRef<T extends { ref: string }>(
  current: readonly T[],
  incoming: readonly T[],
): T[] {
  const values = new Map(current.map((item) => [item.ref, item]));
  for (const item of incoming) values.set(item.ref, item);
  return [...values.values()];
}

function mergeRunOccurrences(
  current: readonly ScheduleRunOccurrence[],
  incoming: readonly ScheduleRunOccurrence[],
): ScheduleRunOccurrence[] {
  const key = (occurrence: ScheduleRunOccurrence) =>
    `${occurrence.scheduleRef}\u0000${occurrence.scheduleRevisionRef}\u0000${occurrence.run.ref}`;
  const values = new Map(current.map((item) => [key(item), item]));
  for (const item of incoming) values.set(key(item), item);
  return [...values.values()];
}

function replaceSchedule(schedule: Schedule): void {
  schedules.value = mergeByRef(schedules.value, [schedule]);
  platform.schedules[schedule.ref] = schedule;
}

async function loadList(reset = false): Promise<void> {
  if (!reset && moreLoading.value) return;
  if (!reset && !nextPageToken.value) return;
  if (reset) {
    listController?.abort();
    listController = new AbortController();
    listLoading.value = true;
    listProblem.value = undefined;
  } else moreLoading.value = true;
  const controller = listController ?? new AbortController();
  const requestedProject = props.projectRef;
  try {
    const page = await loadSchedulePage(
      requestedProject,
      search.value,
      reset ? undefined : nextPageToken.value,
      controller.signal,
    );
    if (controller.signal.aborted || requestedProject !== props.projectRef)
      return;
    schedules.value = reset
      ? page.items
      : mergeByRef(schedules.value, page.items);
    nextPageToken.value = page.nextPageToken || undefined;
    for (const schedule of page.items)
      platform.schedules[schedule.ref] = schedule;
  } catch (error) {
    if (!controller.signal.aborted) listProblem.value = asProblem(error);
  } finally {
    if (reset) listLoading.value = false;
    else moreLoading.value = false;
  }
}

watch(search, () => {
  if (searchTimer) clearTimeout(searchTimer);
  searchTimer = setTimeout(() => void loadList(true), 280);
});

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
  resetHistory();
});

watch(selectedSection, (section) => {
  if (section === "VERSIONS" && revisions.value.length === 0)
    void loadRevisions(true);
  if (section === "RUNS" && runOccurrences.value.length === 0)
    void loadRuns(true);
});

function resetHistory(): void {
  historyController?.abort();
  historyController = undefined;
  revisions.value = [];
  revisionsToken.value = undefined;
  revisionsProblem.value = undefined;
  runOccurrences.value = [];
  runsToken.value = undefined;
  runsProblem.value = undefined;
}

async function loadRevisions(reset = false): Promise<void> {
  const scheduleRef = selectedRef.value;
  if (
    !scheduleRef ||
    revisionsLoading.value ||
    (!reset && !revisionsToken.value)
  )
    return;
  if (reset) {
    historyController?.abort();
    historyController = new AbortController();
    revisions.value = [];
    revisionsProblem.value = undefined;
  }
  revisionsLoading.value = true;
  const controller = historyController ?? new AbortController();
  try {
    const page = await loadScheduleRevisionPage(
      scheduleRef,
      reset ? undefined : revisionsToken.value,
      controller.signal,
    );
    if (controller.signal.aborted || scheduleRef !== selectedRef.value) return;
    revisions.value = reset
      ? page.items
      : mergeByRef(revisions.value, page.items);
    revisionsToken.value = page.nextPageToken || undefined;
  } catch (error) {
    if (!controller.signal.aborted) revisionsProblem.value = asProblem(error);
  } finally {
    revisionsLoading.value = false;
  }
}

async function loadRuns(reset = false): Promise<void> {
  const scheduleRef = selectedRef.value;
  if (!scheduleRef || runsLoading.value || (!reset && !runsToken.value)) return;
  if (reset) {
    historyController?.abort();
    historyController = new AbortController();
    runOccurrences.value = [];
    runsProblem.value = undefined;
  }
  runsLoading.value = true;
  const controller = historyController ?? new AbortController();
  try {
    const page = await loadScheduleRunPage(
      scheduleRef,
      reset ? undefined : runsToken.value,
      controller.signal,
    );
    if (controller.signal.aborted || scheduleRef !== selectedRef.value) return;
    runOccurrences.value = reset
      ? page.items
      : mergeRunOccurrences(runOccurrences.value, page.items);
    runsToken.value = page.nextPageToken || undefined;
  } catch (error) {
    if (!controller.signal.aborted) runsProblem.value = asProblem(error);
  } finally {
    runsLoading.value = false;
  }
}

function scheduleLabel(schedule: Schedule): string {
  const preset = t(`automations.presetValue.${schedule.preset}`);
  const day = schedule.dayOfWeek
    ? ` · ${t(`automations.day.${schedule.dayOfWeek}`)}`
    : "";
  const time =
    schedule.preset === "HOURLY" ? "" : ` · ${schedule.timeOfDay ?? ""}`;
  return `${preset}${day}${time}`;
}

function revisionLabel(revision: ScheduleRevision): string {
  return `${t(`automations.presetValue.${revision.preset}`)} · ${revision.cronExpression} · ${revision.timezone}`;
}

function formatDate(value: string): string {
  return new Intl.DateTimeFormat(locale.value, {
    dateStyle: "medium",
    timeStyle: "short",
  }).format(new Date(value));
}

function task(value: Schedule | ScheduleRevision): string {
  const item = value.input?.task;
  return typeof item === "string" ? item : t("common.noData");
}

function statusLabel(schedule: Schedule): string | undefined {
  return schedule.state === "DELETED" ? custom.value.deleted : undefined;
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

async function refreshExact(ref: string): Promise<void> {
  replaceSchedule(await readSchedule(ref));
}

async function submitEditor(
  input: ScheduleInput,
  current?: Schedule,
): Promise<void> {
  if (!current && !canCreate.value) return;
  editorBusy.value = true;
  editorProblem.value = undefined;
  try {
    const result = await saveSchedule(props.projectRef, input, current);
    replaceSchedule(result);
    selectedRef.value = result.ref;
    search.value = "";
    editorOpen.value = false;
    resetHistory();
  } catch (error) {
    const nextProblem = asProblem(error);
    if (nextProblem.kind === "conflict" && current)
      await refreshExact(current.ref).catch(() => undefined);
    editorProblem.value = nextProblem;
  } finally {
    editorBusy.value = false;
  }
}

async function runCommand(
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
    replaceSchedule(await commandSchedule(schedule, action));
  } catch (error) {
    const nextProblem = asProblem(error);
    if (nextProblem.kind === "conflict")
      await refreshExact(schedule.ref).catch(() => undefined);
    problem.value = nextProblem;
  } finally {
    commandBusy.value = "";
  }
}

async function confirmArchive(): Promise<void> {
  const schedule = archiveSchedule.value;
  if (!schedule) return;
  await runCommand(schedule, "ARCHIVE");
  if (!problem.value) archiveScheduleRef.value = "";
}

async function confirmDelete(): Promise<void> {
  const schedule = deleteCandidate.value;
  if (!schedule || !schedule.nextActions.includes("DELETE")) return;
  commandBusy.value = schedule.ref;
  problem.value = undefined;
  try {
    replaceSchedule(await removeSchedule(schedule));
    deleteScheduleRef.value = "";
  } catch (error) {
    const nextProblem = asProblem(error);
    if (nextProblem.kind === "conflict")
      await refreshExact(schedule.ref).catch(() => undefined);
    problem.value = nextProblem;
  } finally {
    commandBusy.value = "";
  }
}

function observeSentinel(element: HTMLElement | undefined): void {
  listObserver?.disconnect();
  if (!element || typeof IntersectionObserver === "undefined") return;
  listObserver = new IntersectionObserver((entries) => {
    if (entries.some((entry) => entry.isIntersecting)) void loadList(false);
  });
  listObserver.observe(element);
}

onMounted(() => {
  void Promise.all([loadList(true), platform.loadProject(props.projectRef)]);
  stopSentinelWatch = watch(listSentinel, observeSentinel, { immediate: true });
});

onBeforeUnmount(() => {
  listController?.abort();
  historyController?.abort();
  if (searchTimer) clearTimeout(searchTimer);
  listObserver?.disconnect();
  stopSentinelWatch?.();
});
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
          <option value="CURRENT">{{ custom.currentStates }}</option>
          <option value="ALL">{{ custom.allStates }}</option>
          <option value="ACTIVE">{{ $t("states.ACTIVE") }}</option>
          <option value="PAUSED">{{ $t("states.PAUSED") }}</option>
          <option value="NEEDS_ATTENTION">
            {{ $t("states.NEEDS_ATTENTION") }}
          </option>
          <option value="ARCHIVED">{{ $t("states.ARCHIVED") }}</option>
          <option value="DELETED">{{ custom.deleted }}</option>
        </select>
      </label>
      <span class="automations-workspace__count mono">{{
        filteredSchedules.length
      }}</span>
      <button
        v-if="canCreate"
        class="button button--primary"
        type="button"
        @click="openCreate"
      >
        <Plus :size="16" aria-hidden="true" />
        {{ $t("automations.new") }}
      </button>
    </div>

    <ProblemNotice v-if="problem" :problem="problem" compact />
    <AsyncState
      :loading="listLoading"
      :problem="listProblem"
      :empty="schedules.length === 0"
      :empty-title="$t('automations.emptyTitle')"
      :empty-text="$t('automations.emptyText')"
      @retry="loadList(true)"
    >
      <section
        v-if="filteredSchedules.length === 0"
        class="empty-state automations-workspace__empty"
      >
        <h2>{{ custom.noMatches }}</h2>
        <p>{{ custom.noMatchesText }}</p>
        <button
          v-if="nextPageToken"
          class="button"
          type="button"
          :disabled="moreLoading"
          @click="loadList(false)"
        >
          {{ custom.loadMore }}
        </button>
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
              <StatusBadge
                :state="schedule.state"
                :label="statusLabel(schedule)"
              />
              <small class="mono">v{{ schedule.version }}</small>
            </span>
            <span class="automation-row__next">
              {{ schedule.nextRunAt ? formatDate(schedule.nextRunAt) : "—" }}
            </span>
            <span class="automation-row__outcome">{{
              schedule.lastOutcome || "—"
            }}</span>
          </button>
          <div ref="listSentinel" class="automation-list-sentinel">
            <LoaderCircle
              v-if="moreLoading"
              class="spin"
              :size="18"
              aria-hidden="true"
            />
            <button
              v-else-if="nextPageToken"
              class="button"
              type="button"
              @click="loadList(false)"
            >
              {{ custom.loadMore }}
            </button>
          </div>
        </div>

        <aside v-if="selectedSchedule" class="automation-details">
          <header>
            <div>
              <h2>{{ selectedSchedule.name }}</h2>
              <div class="automation-details__status">
                <StatusBadge
                  :state="selectedSchedule.state"
                  :label="statusLabel(selectedSchedule)"
                />
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
              <dt>{{ custom.revision }}</dt>
              <dd>
                <span class="mono">{{
                  selectedSchedule.currentRevision.revision
                }}</span>
                · {{ selectedSchedule.currentRevision.ref }}
              </dd>
            </div>
            <div>
              <dt>{{ custom.automationRef }}</dt>
              <dd class="mono">{{ selectedSchedule.ref }}</dd>
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
          >
            <div class="automation-details__history-heading">
              <History :size="18" aria-hidden="true" />
              <h3>{{ $t("automations.versionHistory") }}</h3>
            </div>
            <ProblemNotice
              v-if="revisionsProblem"
              :problem="revisionsProblem"
              compact
            />
            <div
              v-if="revisionsLoading && revisions.length === 0"
              class="automation-details__loading"
            >
              <LoaderCircle class="spin" :size="20" aria-hidden="true" />
            </div>
            <p
              v-else-if="revisions.length === 0"
              class="automation-details__empty"
            >
              {{ custom.noRevisions }}
            </p>
            <article
              v-for="revision in revisions"
              :key="revision.ref"
              class="automation-details__revision"
            >
              <div>
                <strong>{{ custom.revision }} {{ revision.revision }}</strong>
                <span
                  v-if="revision.ref === selectedSchedule.currentRevision.ref"
                  class="automation-details__current"
                  >{{ custom.current }}</span
                >
              </div>
              <p>{{ revisionLabel(revision) }}</p>
              <p>{{ task(revision) }}</p>
              <dl>
                <div>
                  <dt>{{ custom.revisionRef }}</dt>
                  <dd class="mono">{{ revision.ref }}</dd>
                </div>
                <div>
                  <dt>Digest</dt>
                  <dd class="mono">{{ revision.digest }}</dd>
                </div>
                <div>
                  <dt>{{ custom.target }}</dt>
                  <dd>{{ revision.target.displayName }}</dd>
                </div>
                <div>
                  <dt>{{ custom.createdAt }}</dt>
                  <dd>{{ formatDate(revision.createdAt) }}</dd>
                </div>
              </dl>
            </article>
            <button
              v-if="revisionsToken"
              class="button"
              type="button"
              :disabled="revisionsLoading"
              @click="loadRevisions(false)"
            >
              {{ custom.loadMore }}
            </button>
          </section>

          <section v-else class="automation-details__history">
            <div class="automation-details__history-heading">
              <History :size="18" aria-hidden="true" />
              <h3>{{ $t("automations.runHistory") }}</h3>
            </div>
            <ProblemNotice v-if="runsProblem" :problem="runsProblem" compact />
            <div
              v-if="runsLoading && runOccurrences.length === 0"
              class="automation-details__loading"
            >
              <LoaderCircle class="spin" :size="20" aria-hidden="true" />
            </div>
            <p
              v-else-if="runOccurrences.length === 0"
              class="automation-details__empty"
            >
              {{ custom.noRuns }}
            </p>
            <article
              v-for="occurrence in runOccurrences"
              :key="`${occurrence.scheduleRef}:${occurrence.scheduleRevisionRef}:${occurrence.run.ref}`"
              class="automation-details__run"
            >
              <div>
                <strong>{{ occurrence.run.title }}</strong
                ><StatusBadge :state="occurrence.run.state" />
              </div>
              <p>
                {{
                  occurrence.run.activitySummary ||
                  occurrence.run.resultSummary ||
                  "—"
                }}
              </p>
              <dl>
                <div>
                  <dt>{{ custom.runRef }}</dt>
                  <dd class="mono">{{ occurrence.run.ref }}</dd>
                </div>
                <div>
                  <dt>{{ custom.automationRef }}</dt>
                  <dd class="mono">{{ occurrence.scheduleRef }}</dd>
                </div>
                <div>
                  <dt>{{ custom.revision }}</dt>
                  <dd>{{ occurrence.scheduleRevision }}</dd>
                </div>
                <div>
                  <dt>{{ custom.revisionRef }}</dt>
                  <dd class="mono">{{ occurrence.scheduleRevisionRef }}</dd>
                </div>
                <div>
                  <dt>{{ custom.createdAt }}</dt>
                  <dd>{{ formatDate(occurrence.run.createdAt) }}</dd>
                </div>
              </dl>
              <RouterLink
                class="button"
                :to="`/projects/${projectRef}/runs/${occurrence.run.ref}`"
                >{{ $t("common.open") }}</RouterLink
              >
            </article>
            <button
              v-if="runsToken"
              class="button"
              type="button"
              :disabled="runsLoading"
              @click="loadRuns(false)"
            >
              {{ custom.loadMore }}
            </button>
          </section>

          <div class="automation-details__actions" :aria-label="custom.actions">
            <button
              v-if="selectedCapabilities?.canEdit"
              class="button button--primary"
              type="button"
              @click="openEdit(selectedSchedule)"
            >
              <Pencil :size="16" aria-hidden="true" />{{ custom.edit }}
            </button>
            <button
              v-if="selectedCapabilities?.canPause"
              class="button"
              type="button"
              :disabled="commandBusy === selectedSchedule.ref"
              @click="runCommand(selectedSchedule, 'PAUSE')"
            >
              <Pause :size="16" aria-hidden="true" />{{
                $t("automations.pause")
              }}
            </button>
            <button
              v-if="selectedCapabilities?.canEnable"
              class="button"
              type="button"
              :disabled="commandBusy === selectedSchedule.ref"
              @click="runCommand(selectedSchedule, 'ENABLE')"
            >
              <Play :size="16" aria-hidden="true" />{{ $t("common.enable") }}
            </button>
            <button
              v-if="selectedCapabilities?.canArchive"
              class="button"
              type="button"
              @click="archiveScheduleRef = selectedSchedule.ref"
            >
              <Archive :size="16" aria-hidden="true" />{{ custom.archive }}
            </button>
            <button
              v-if="selectedCapabilities?.canDelete"
              class="button button--danger"
              type="button"
              @click="deleteScheduleRef = selectedSchedule.ref"
            >
              <Trash2 :size="16" aria-hidden="true" />{{ custom.delete }}
            </button>
          </div>
        </aside>
      </div>
    </AsyncState>

    <AutomationEditorDialog
      v-if="editorOpen"
      :busy="editorBusy"
      :problem="editorProblem"
      :project-ref="projectRef"
      :schedule="editorSchedule"
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
    <AutomationArchiveDialog
      v-if="deleteCandidate"
      :schedule="deleteCandidate"
      :title="custom.deleteTitle"
      :description="custom.deleteDescription"
      :cancel-label="$t('common.cancel')"
      :confirm-label="custom.delete"
      :busy="commandBusy === deleteCandidate.ref"
      kind="DELETE"
      @close="deleteScheduleRef = ''"
      @confirm="confirmDelete"
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
  width: min(460px, 55vw);
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
  grid-template-columns: minmax(0, 1fr) minmax(340px, 28vw);
}
.automations-list {
  min-width: 700px;
  max-height: 72vh;
  overflow: auto;
}
.automations-list__head,
.automation-row {
  display: grid;
  grid-template-columns:
    minmax(230px, 1.35fr) minmax(160px, 0.9fr)
    126px 138px minmax(110px, 0.7fr);
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
.automation-list-sentinel {
  display: flex;
  min-height: 52px;
  align-items: center;
  justify-content: center;
}
.automation-details {
  max-height: 72vh;
  overflow: auto;
  padding: 16px;
  border-left: 1px solid var(--border);
}
.automation-details header,
.automation-details__history-heading,
.automation-details__revision > div,
.automation-details__run > div {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 10px;
}
.automation-details header {
  padding-bottom: 14px;
  border-bottom: 1px solid var(--border);
}
.automation-details header h2 {
  margin: 0;
  overflow-wrap: anywhere;
  font-size: 1.05rem;
}
.automation-details header > svg,
.automation-details__history-heading {
  color: var(--accent);
}
.automation-details__status {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 8px;
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
.automation-details dl {
  margin: 0;
}
.automation-details dl div {
  display: grid;
  grid-template-columns: 112px minmax(0, 1fr);
  gap: 10px;
  padding: 10px 0;
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
  gap: 10px;
  padding-top: 14px;
}
.automation-details__history-heading {
  justify-content: flex-start;
  align-items: center;
}
.automation-details__history-heading h3 {
  margin: 0;
  color: var(--text);
  font-size: 0.9rem;
}
.automation-details__revision,
.automation-details__run {
  padding: 12px;
  border: 1px solid var(--border);
  border-radius: 7px;
}
.automation-details__revision p,
.automation-details__run p {
  margin: 8px 0;
  color: var(--muted);
  font-size: 0.78rem;
  overflow-wrap: anywhere;
}
.automation-details__revision dl,
.automation-details__run dl {
  margin: 8px 0;
}
.automation-details__revision dl div,
.automation-details__run dl div {
  grid-template-columns: 92px minmax(0, 1fr);
  padding: 6px 0;
}
.automation-details__current {
  padding: 3px 7px;
  border-radius: 999px;
  background: var(--accent-soft);
  color: var(--accent);
  font-size: 0.72rem;
}
.automation-details__loading {
  display: flex;
  min-height: 80px;
  align-items: center;
  justify-content: center;
}
.automation-details__empty {
  color: var(--muted);
}
.automations-workspace__empty {
  margin: 16px;
}
.spin {
  animation: rotate 0.8s linear infinite;
}
@keyframes rotate {
  to {
    transform: rotate(360deg);
  }
}
@media (max-width: 980px) {
  .automations-workspace__layout {
    grid-template-columns: minmax(0, 1fr) 320px;
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
