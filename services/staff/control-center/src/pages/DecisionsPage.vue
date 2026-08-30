<script setup lang="ts">
import {
  CalendarClock,
  ExternalLink,
  FileStack,
  FolderKanban,
  ShieldQuestion,
  UserRound,
} from "@lucide/vue";
import { computed, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute } from "vue-router";

import { usePlatformStore } from "@/features/platform/store";
import {
  decisionActionLayout,
  decisionHistory,
  decisionInbox,
  groupDecisionInbox,
  type DecisionAction,
  type DecisionInboxItem,
} from "@/features/workboard/model";
import type { OwnerGate } from "@/shared/api/generated/openapi/types.gen";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import { runPath } from "@/shared/routes";
import AsyncState from "@/shared/ui/AsyncState.vue";
import AttachmentComposer from "@/shared/ui/AttachmentComposer.vue";
import type { AttachmentComposerState } from "@/shared/ui/attachment-composer";
import PageFrame from "@/shared/ui/PageFrame.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

const platform = usePlatformStore();
const route = useRoute();
const { locale, t } = useI18n();
const projectFilter = ref(
  typeof route.query.projectRef === "string" ? route.query.projectRef : "",
);
const view = ref<"PENDING" | "HISTORY">("PENDING");
const selectedRef = ref("");
const comments = ref<Record<string, string>>({});
const attachmentStates = ref<Record<string, AttachmentComposerState>>({});
const busyRef = ref("");
const problem = ref<AppProblem>();

const inbox = computed(() =>
  decisionInbox(
    platform.gateList,
    platform.projectList,
    projectFilter.value || undefined,
    new Date(),
    platform.runList,
  ),
);
const history = computed(() =>
  decisionHistory(
    platform.gateList,
    platform.projectList,
    projectFilter.value || undefined,
    platform.runList,
  ),
);
const visibleItems = computed(() =>
  view.value === "PENDING" ? inbox.value : history.value,
);
const groups = computed(() => groupDecisionInbox(visibleItems.value));
const selected = computed(() =>
  visibleItems.value.find((item) => item.gate.ref === selectedRef.value),
);
const selectedActions = computed(() =>
  selected.value
    ? decisionActionLayout(selected.value.gate)
    : { primary: undefined, secondary: [] },
);
const projectsWithGates = computed(() => {
  const refs = new Set(
    platform.gateList
      .filter((gate) =>
        view.value === "PENDING" ? gate.state === "OPEN" : true,
      )
      .map((gate) => gate.projectRef),
  );
  return platform.projectList.filter((project) => refs.has(project.ref));
});

watch(
  visibleItems,
  (items) => {
    if (!items.some((item) => item.gate.ref === selectedRef.value))
      selectedRef.value = items[0]?.gate.ref ?? "";
  },
  { immediate: true },
);

function formatDate(value?: string): string {
  if (!value) return "";
  return new Intl.DateTimeFormat(locale.value, {
    dateStyle: "medium",
    timeStyle: "short",
  }).format(new Date(value));
}

function projectPath(item: DecisionInboxItem): string {
  return `/projects/${encodeURIComponent(item.gate.projectRef)}`;
}

function runNodePath(item: DecisionInboxItem) {
  return {
    path: runPath(item.gate.runRef, item.gate.projectRef),
    query: { nodeRef: item.gate.nodeRef },
  };
}

function artifactPath(gate: OwnerGate): string {
  const first = gate.artifactRefs[0];
  const prefix = `/projects/${encodeURIComponent(gate.projectRef)}/files`;
  return first ? `${prefix}?artifactRef=${encodeURIComponent(first)}` : prefix;
}

async function decide(
  gate: OwnerGate,
  decision: "APPROVE" | "REJECT" | "REQUEST_CHANGES" | "CANCEL",
): Promise<void> {
  if (
    !gate.nextActions.includes("RESOLVE_GATE") ||
    !gate.allowedDecisions.includes(decision)
  )
    return;
  busyRef.value = gate.ref;
  problem.value = undefined;
  try {
    await platform.decide(gate, {
      decision,
      comment: comments.value[gate.ref] ?? "",
      ...(attachmentStates.value[gate.ref]?.refs.length
        ? { artifactRefs: attachmentStates.value[gate.ref]?.refs ?? [] }
        : {}),
    });
    Reflect.deleteProperty(comments.value, gate.ref);
    Reflect.deleteProperty(attachmentStates.value, gate.ref);
  } catch (error) {
    problem.value = asProblem(error);
    if (problem.value.kind === "conflict") await platform.loadGates();
  } finally {
    busyRef.value = "";
  }
}

async function uploadAttachment(
  gate: OwnerGate,
  file: File,
): Promise<{ ref: string }> {
  return platform.uploadProjectArtifact(gate.projectRef, file);
}

async function uploadSelectedAttachment(file: File): Promise<{ ref: string }> {
  const item = selected.value;
  if (!item) throw new Error("Owner Gate is unavailable");
  return uploadAttachment(item.gate, file);
}

function attachmentsReady(gateRef: string): boolean {
  return attachmentStates.value[gateRef]?.ready ?? true;
}

function decisionLabel(decision: DecisionAction): string {
  if (decision === "APPROVE") return t("common.approve");
  if (decision === "REQUEST_CHANGES") return t("common.requestChanges");
  if (decision === "REJECT") return t("common.reject");
  return t("common.cancel");
}

function secondaryActionClass(decision: DecisionAction): string[] {
  return ["button", ...(decision === "REJECT" ? ["button--danger"] : [])];
}

onMounted(
  () =>
    void Promise.all([
      platform.loadGates(),
      platform.loadProjects(),
      platform.loadRuns(),
    ]),
);
</script>

<template>
  <PageFrame
    :title="$t('decisions.title')"
    :subtitle="$t('decisions.subtitle')"
  >
    <div class="decision-toolbar">
      <div class="decision-toolbar__filters">
        <div class="decision-view-switch" role="group">
          <button
            class="button"
            type="button"
            :aria-pressed="view === 'PENDING'"
            @click="view = 'PENDING'"
          >
            {{ $t("decisions.pending") }}
          </button>
          <button
            class="button"
            type="button"
            :aria-pressed="view === 'HISTORY'"
            @click="view = 'HISTORY'"
          >
            {{ $t("decisions.history") }}
          </button>
        </div>
        <label>
          <span>{{ $t("decisions.projectFilter") }}</span>
          <select v-model="projectFilter">
            <option value="">{{ $t("decisions.allProjects") }}</option>
            <option
              v-for="project in projectsWithGates"
              :key="project.ref"
              :value="project.ref"
            >
              {{ project.name }}
            </option>
          </select>
        </label>
      </div>
      <span class="decision-toolbar__count">
        {{
          $t(
            view === "PENDING"
              ? "decisions.pendingCount"
              : "decisions.historyCount",
            { count: visibleItems.length },
          )
        }}
      </span>
    </div>

    <ProblemNotice v-if="problem" :problem="problem" compact />
    <AsyncState
      :loading="
        platform.loading.gates ||
        platform.loading.projects ||
        platform.loading.runs
      "
      :problem="platform.problems.gates"
      :empty="visibleItems.length === 0"
      :empty-title="
        $t(
          view === 'PENDING'
            ? 'decisions.emptyTitle'
            : 'decisions.historyEmpty',
        )
      "
      :empty-text="
        $t(
          view === 'PENDING'
            ? 'decisions.emptyText'
            : 'decisions.historyEmptyText',
        )
      "
      @retry="platform.loadGates()"
    >
      <div class="decision-inbox">
        <div class="decision-list">
          <section v-for="group in groups" :key="group.key">
            <header class="decision-group-header">
              <span
                v-if="view === 'PENDING'"
                class="decision-urgency"
                :class="`decision-urgency--${group.urgency.toLowerCase()}`"
              >
                {{ $t(`decisions.urgency.${group.urgency}`) }}
              </span>
              <strong>
                {{ group.project?.name ?? $t("decisions.projectUnavailable") }}
              </strong>
              <span class="decision-group-header__count">
                {{ group.items.length }}
              </span>
            </header>
            <div role="list">
              <button
                v-for="item in group.items"
                :key="item.gate.ref"
                class="decision-row"
                :class="{
                  'decision-row--selected': selectedRef === item.gate.ref,
                }"
                type="button"
                role="listitem"
                @click="selectedRef = item.gate.ref"
              >
                <span class="decision-row__icon">
                  <ShieldQuestion :size="18" aria-hidden="true" />
                </span>
                <span class="decision-row__copy">
                  <strong>{{ item.gate.title }}</strong>
                  <span>{{
                    item.hasQuestion
                      ? item.gate.contextSummary
                      : $t("decisions.questionUnavailable")
                  }}</span>
                  <small>
                    {{ item.gate.requestedBy.displayName }} ·
                    {{ formatDate(item.gate.openedAt) }}
                  </small>
                  <small class="decision-row__route">
                    {{ item.run?.target.displayName }} ·
                    {{ item.run?.title ?? $t("decisions.runUnavailable") }} ·
                    {{ item.gate.nodeRef }}
                  </small>
                </span>
                <span class="decision-row__status">
                  <StatusBadge :state="item.gate.state" />
                </span>
              </button>
            </div>
          </section>
        </div>

        <aside v-if="selected" class="decision-detail">
          <header class="decision-detail__header">
            <div>
              <p class="eyebrow">{{ $t("decisions.question") }}</p>
              <h2>{{ selected.gate.title }}</h2>
            </div>
            <StatusBadge :state="selected.gate.state" />
          </header>

          <dl class="decision-meta">
            <div>
              <dt>
                <FolderKanban :size="15" aria-hidden="true" />{{
                  $t("app.project")
                }}
              </dt>
              <dd>
                <RouterLink :to="projectPath(selected)">
                  {{
                    selected.project?.name ?? $t("decisions.projectUnavailable")
                  }}
                </RouterLink>
              </dd>
            </div>
            <div>
              <dt>
                <ExternalLink :size="15" aria-hidden="true" />{{
                  $t("decisions.process")
                }}
              </dt>
              <dd>
                <span v-if="selected.run" class="decision-target">
                  {{ selected.run.target.displayName }}
                </span>
                <RouterLink :to="runNodePath(selected)">
                  {{ selected.run?.title ?? $t("decisions.runUnavailable") }}
                  · {{ selected.gate.nodeRef }}
                </RouterLink>
              </dd>
            </div>
            <div>
              <dt>
                <UserRound :size="15" aria-hidden="true" />{{
                  $t("decisions.requestedBy")
                }}
              </dt>
              <dd>{{ selected.gate.requestedBy.displayName }}</dd>
            </div>
            <div>
              <dt>
                <CalendarClock :size="15" aria-hidden="true" />{{
                  $t("decisions.openedAt")
                }}
              </dt>
              <dd>{{ formatDate(selected.gate.openedAt) }}</dd>
            </div>
            <div>
              <dt>
                <CalendarClock :size="15" aria-hidden="true" />{{
                  $t("decisions.deadline")
                }}
              </dt>
              <dd>
                <span>
                  {{
                    selected.gate.expiresAt
                      ? formatDate(selected.gate.expiresAt)
                      : $t("decisions.noDeadline")
                  }}
                </span>
                <span
                  v-if="view === 'PENDING'"
                  class="decision-deadline-urgency"
                  :class="`decision-urgency--${selected.urgency.toLowerCase()}`"
                >
                  {{ $t(`decisions.urgency.${selected.urgency}`) }}
                </span>
              </dd>
            </div>
            <div>
              <dt>
                <FileStack :size="15" aria-hidden="true" />{{
                  $t("decisions.evidence")
                }}
              </dt>
              <dd>
                <RouterLink
                  v-if="selected.gate.artifactRefs.length"
                  :to="artifactPath(selected.gate)"
                >
                  {{
                    $t("decisions.evidenceCount", {
                      count: selected.gate.artifactRefs.length,
                    })
                  }}
                </RouterLink>
                <span v-else>{{ $t("decisions.noEvidence") }}</span>
              </dd>
            </div>
          </dl>

          <section class="decision-copy">
            <h3>{{ $t("decisions.fullQuestion") }}</h3>
            <p>
              {{
                selected.hasQuestion
                  ? selected.gate.contextSummary
                  : $t("decisions.questionUnavailable")
              }}
            </p>
          </section>
          <section class="decision-copy decision-copy--consequences">
            <h3>{{ $t("decisions.consequences") }}</h3>
            <p>
              {{
                selected.hasConsequences
                  ? selected.gate.consequencesSummary
                  : $t("decisions.consequencesUnavailable")
              }}
            </p>
          </section>

          <section v-if="view === 'HISTORY'" class="decision-copy">
            <h3>{{ $t("decisions.outcome") }}</h3>
            <p>
              <StatusBadge :state="selected.gate.state" />
              <template v-if="selected.gate.decidedBy">
                · {{ selected.gate.decidedBy.displayName }}
              </template>
              <template v-if="selected.gate.decidedAt">
                · {{ formatDate(selected.gate.decidedAt) }}
              </template>
            </p>
            <p v-if="selected.gate.decisionComment">
              {{ selected.gate.decisionComment }}
            </p>
          </section>

          <template v-if="selected.canResolve">
            <label class="field decision-comment">
              <span>{{ $t("decisions.comment") }}</span>
              <textarea
                v-model="comments[selected.gate.ref]"
                maxlength="2000"
                :placeholder="$t('decisions.commentPlaceholder')"
              />
            </label>
            <AttachmentComposer
              :upload="uploadSelectedAttachment"
              :project-ref="selected.gate.projectRef"
              :disabled="busyRef === selected.gate.ref"
              @change="attachmentStates[selected.gate.ref] = $event"
            />
            <div class="decision-actions">
              <button
                v-if="selectedActions.primary"
                class="button button--primary"
                type="button"
                :disabled="
                  busyRef === selected.gate.ref ||
                  !attachmentsReady(selected.gate.ref)
                "
                @click="decide(selected.gate, selectedActions.primary)"
              >
                {{ decisionLabel(selectedActions.primary) }}
              </button>
              <button
                v-for="decision in selectedActions.secondary"
                :key="decision"
                :class="secondaryActionClass(decision)"
                type="button"
                :disabled="
                  busyRef === selected.gate.ref ||
                  !attachmentsReady(selected.gate.ref)
                "
                @click="decide(selected.gate, decision)"
              >
                {{ decisionLabel(decision) }}
              </button>
            </div>
          </template>
          <div v-else class="decision-unavailable" role="status">
            <strong>{{ $t("decisions.actionsUnavailable") }}</strong>
            <p>{{ $t("decisions.actionsUnavailableText") }}</p>
          </div>
        </aside>
      </div>
    </AsyncState>
  </PageFrame>
</template>

<style scoped>
.decision-toolbar {
  display: flex;
  align-items: end;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 14px;
}
.decision-toolbar__filters {
  display: flex;
  min-width: 0;
  align-items: end;
  gap: 12px;
}
.decision-toolbar label {
  display: grid;
  gap: 5px;
  min-width: min(320px, 100%);
  font-size: 0.78rem;
  font-weight: 600;
}
.decision-view-switch {
  display: flex;
}
.decision-view-switch .button {
  min-width: 112px;
  border-radius: 0;
}
.decision-view-switch .button:first-child {
  border-radius: 7px 0 0 7px;
}
.decision-view-switch .button:last-child {
  border-radius: 0 7px 7px 0;
}
.decision-view-switch .button[aria-pressed="true"] {
  border-color: var(--accent);
  background: var(--accent-soft);
  color: var(--accent-strong);
}
.decision-toolbar select {
  min-height: 38px;
}
.decision-toolbar__count {
  color: var(--muted);
}
.decision-inbox {
  display: grid;
  min-height: min(720px, calc(100vh - 230px));
  grid-template-columns: minmax(360px, 440px) minmax(0, 1fr);
  overflow: hidden;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--surface);
}
.decision-list {
  max-height: 72vh;
  overflow: auto;
  border-right: 1px solid var(--border);
}
.decision-list > section + section {
  border-top: 1px solid var(--border-strong);
}
.decision-group-header {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr) auto;
  align-items: center;
  gap: 5px 8px;
  padding: 11px 14px;
  border-bottom: 1px solid var(--hairline);
  background: var(--panel);
}
.decision-group-header__count {
  color: var(--muted);
  font-size: 0.78rem;
  font-variant-numeric: tabular-nums;
}
.decision-row {
  display: grid;
  width: 100%;
  grid-template-columns: auto minmax(0, 1fr) auto;
  align-items: start;
  gap: 11px;
  min-height: 112px;
  padding: 14px;
  border: 0;
  border-bottom: 1px solid var(--hairline);
  color: inherit;
  background: var(--surface);
  text-align: left;
  cursor: pointer;
}
.decision-row:hover,
.decision-row--selected {
  background: var(--accent-soft);
}
.decision-row__icon {
  display: grid;
  place-items: center;
  width: 34px;
  height: 34px;
  border-radius: 8px;
  color: var(--warning);
  background: var(--warning-soft);
}
.decision-row__copy {
  display: grid;
  min-width: 0;
  gap: 4px;
}
.decision-row__copy > span {
  display: -webkit-box;
  overflow: hidden;
  color: var(--muted);
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 2;
}
.decision-row__copy small {
  color: var(--subtle);
}
.decision-row__copy .decision-row__route {
  overflow: hidden;
  color: var(--muted);
  text-overflow: ellipsis;
  white-space: nowrap;
}
.decision-row__status {
  display: grid;
  justify-items: end;
  gap: 7px;
}
.decision-urgency {
  padding: 3px 7px;
  border-radius: 999px;
  color: var(--warning);
  background: var(--warning-soft);
  font-size: 0.7rem;
  font-weight: 700;
}
.decision-urgency--overdue {
  color: var(--danger);
  background: var(--danger-soft);
}
.decision-detail {
  min-width: 0;
  max-height: 72vh;
  overflow: auto;
  padding: 20px;
}
.decision-detail__header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
  padding-bottom: 16px;
  border-bottom: 1px solid var(--border);
}
.decision-detail__header h2,
.decision-detail__header p {
  margin: 0;
}
.decision-detail__header h2 {
  margin-top: 4px;
  font-size: 1.2rem;
}
.decision-meta {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  margin: 0;
  border-bottom: 1px solid var(--border);
}
.decision-meta > div {
  min-width: 0;
  padding: 12px 10px 12px 0;
}
.decision-meta dt {
  display: flex;
  align-items: center;
  gap: 6px;
  color: var(--subtle);
  font-size: 0.74rem;
}
.decision-meta dd {
  display: grid;
  gap: 4px;
  margin: 5px 0 0;
  overflow-wrap: anywhere;
}
.decision-target {
  color: var(--muted);
  font-size: 0.82rem;
}
.decision-deadline-urgency {
  width: fit-content;
  color: var(--warning);
  font-size: 0.76rem;
  font-weight: 700;
}
.decision-copy {
  padding: 16px 0 0;
}
.decision-copy h3,
.decision-copy p {
  margin: 0;
}
.decision-copy p {
  margin-top: 7px;
  line-height: 1.5;
}
.decision-copy--consequences {
  margin-top: 16px;
  padding: 14px;
  border-left: 3px solid var(--warning);
  background: var(--warning-soft);
}
.decision-comment {
  margin-top: 18px;
}
.decision-comment textarea {
  min-height: 92px;
}
.decision-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: 12px;
}
.decision-unavailable {
  margin-top: 18px;
  padding: 14px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--panel);
}
.decision-unavailable p {
  margin: 5px 0 0;
  color: var(--muted);
}
@media (max-width: 900px) {
  .decision-inbox {
    grid-template-columns: 1fr;
    min-height: 0;
  }
  .decision-list {
    max-height: 360px;
    border-right: 0;
    border-bottom: 1px solid var(--border);
  }
  .decision-detail {
    max-height: none;
  }
}
@media (max-width: 620px) {
  .decision-toolbar {
    align-items: stretch;
    flex-direction: column;
  }
  .decision-toolbar__filters {
    align-items: stretch;
    flex-direction: column;
  }
  .decision-view-switch .button {
    flex: 1 1 50%;
  }
  .decision-row {
    grid-template-columns: auto minmax(0, 1fr);
  }
  .decision-row__status {
    grid-column: 2;
    justify-items: start;
  }
  .decision-row__copy .decision-row__route {
    white-space: normal;
  }
  .decision-detail {
    padding: 16px;
  }
  .decision-meta {
    grid-template-columns: 1fr;
  }
}
</style>
