<script setup lang="ts">
import {
  CalendarClock,
  CheckCircle2,
  ExternalLink,
  FileStack,
  FolderKanban,
  History,
  LoaderCircle,
  MessageSquareWarning,
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
import { readAttachmentSet } from "@/shared/api/attachment-sets";
import type {
  AttachmentSet,
  OwnerGate,
} from "@/shared/api/generated/openapi/types.gen";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import { runPath } from "@/shared/routes";
import AsyncState from "@/shared/ui/AsyncState.vue";
import AttachmentComposer from "@/shared/ui/AttachmentComposer.vue";
import type {
  AttachmentComposerHandle,
  AttachmentComposerState,
} from "@/shared/ui/attachment-composer";
import PageFrame from "@/shared/ui/PageFrame.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

const platform = usePlatformStore();
const route = useRoute();
const { locale, t } = useI18n();
const projectFilter = ref(
  typeof route.query.projectRef === "string" ? route.query.projectRef : "",
);
let preferredGateRef =
  typeof route.query.gateRef === "string" ? route.query.gateRef : "";
const view = ref<"PENDING" | "HISTORY">("PENDING");
const selectedRef = ref(preferredGateRef);
const comments = ref<Record<string, string>>({});
const decisionDrafts = ref<Record<string, DecisionAction>>({});
const validationMessages = ref<Record<string, string>>({});
const attachmentStates = ref<Record<string, AttachmentComposerState>>({});
const selectedAttachmentComposer = ref<AttachmentComposerHandle>();
const resolutionAttachmentSets = ref<Record<string, AttachmentSet>>({});
const busyRef = ref("");
const problem = ref<AppProblem>();
const successMessage = ref("");
let pageMounted = false;

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
const selectedDecision = computed(() => {
  if (!selected.value) return undefined;
  return (
    decisionDrafts.value[selected.value.gate.ref] ??
    selectedActions.value.primary
  );
});
const selectedAuditEvents = computed(() => {
  if (!selected.value) return [];
  return platform.auditEvents
    .filter((event) => event.resourceRef === selected.value?.gate.ref)
    .sort((left, right) => right.occurredAt.localeCompare(left.occurredAt));
});
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
    if (items.some((item) => item.gate.ref === selectedRef.value)) return;
    const preferred = items.find((item) => item.gate.ref === preferredGateRef);
    selectedRef.value = preferred?.gate.ref ?? items[0]?.gate.ref ?? "";
    if (items.length) preferredGateRef = "";
  },
  { immediate: true },
);

watch(projectFilter, (value) => {
  if (!pageMounted) return;
  preferredGateRef = "";
  selectedRef.value = "";
  void platform.loadGates(value || undefined);
});

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

let attachmentLoadGeneration = 0;
async function loadResolutionAttachments(gate?: OwnerGate): Promise<void> {
  const generation = ++attachmentLoadGeneration;
  if (!gate?.resolutionAttachmentSetRef) return;
  try {
    const attachmentSet = await readAttachmentSet(
      gate.resolutionAttachmentSetRef,
    );
    if (generation === attachmentLoadGeneration)
      resolutionAttachmentSets.value[gate.ref] = attachmentSet;
  } catch (error) {
    if (generation === attachmentLoadGeneration)
      problem.value = asProblem(error);
  }
}

async function loadGateAudit(gate?: OwnerGate): Promise<void> {
  if (!gate) return;
  await platform.loadAudit(gate.projectRef, gate.ref);
}

function decisionOutcomeState(decision: DecisionAction): OwnerGate["state"] {
  if (decision === "APPROVE") return "APPROVED";
  if (decision === "REQUEST_CHANGES") return "CHANGES_REQUESTED";
  if (decision === "REJECT") return "REJECTED";
  return "CANCELLED";
}

function decisionConsequence(
  gate: OwnerGate,
  decision: DecisionAction,
): string {
  if (decision === "APPROVE")
    return (
      gate.consequencesSummary.trim() || t("decisions.consequencesUnavailable")
    );
  if (decision === "REQUEST_CHANGES")
    return "Gate перейдёт в CHANGES_REQUESTED. Следующий переход Run не представлен текущей проекцией.";
  if (decision === "REJECT")
    return "Gate перейдёт в REJECTED. Закрытие или продолжение Run не представлены текущей проекцией.";
  return "Gate перейдёт в CANCELLED. Дальнейший lifecycle Run не представлен текущей проекцией.";
}

function requiresDecisionComment(decision?: DecisionAction): boolean {
  return decision === "REQUEST_CHANGES" || decision === "REJECT";
}

function decisionCommentLabel(decision?: DecisionAction): string {
  if (decision === "REQUEST_CHANGES") return "Что нужно изменить";
  if (decision === "REJECT") return "Причина отклонения";
  if (decision === "CANCEL") return "Причина отмены";
  return "Комментарий к одобрению";
}

function decisionCommentPlaceholder(decision?: DecisionAction): string {
  if (decision === "REQUEST_CHANGES")
    return "Перечислите проверяемые изменения, необходимые для повторного решения";
  if (decision === "REJECT")
    return "Укажите причину, которая будет записана в аудит";
  return t("decisions.commentPlaceholder");
}

function selectDecision(gate: OwnerGate, decision: DecisionAction): void {
  decisionDrafts.value[gate.ref] = decision;
  Reflect.deleteProperty(validationMessages.value, gate.ref);
}

function clearValidation(gateRef: string): void {
  Reflect.deleteProperty(validationMessages.value, gateRef);
}

async function decide(gate: OwnerGate): Promise<void> {
  const decision =
    decisionDrafts.value[gate.ref] ?? decisionActionLayout(gate).primary;
  if (
    !decision ||
    !gate.nextActions.includes("RESOLVE_GATE") ||
    !gate.allowedDecisions.includes(decision)
  )
    return;
  const comment = comments.value[gate.ref]?.trim() ?? "";
  if (requiresDecisionComment(decision) && !comment) {
    validationMessages.value[gate.ref] =
      decision === "REQUEST_CHANGES"
        ? "Опишите необходимые изменения."
        : "Укажите причину отклонения.";
    return;
  }
  if (!attachmentsReady(gate.ref)) {
    validationMessages.value[gate.ref] =
      "Дождитесь загрузки вложений или исправьте ошибку файла.";
    return;
  }
  busyRef.value = gate.ref;
  problem.value = undefined;
  successMessage.value = "";
  Reflect.deleteProperty(validationMessages.value, gate.ref);
  try {
    const attachmentSetRef = await selectedAttachmentComposer.value?.finalize();
    await platform.decide(gate, {
      decision,
      ...(comment ? { comment } : {}),
      ...(attachmentSetRef ? { attachmentSetRef } : {}),
    });
    selectedAttachmentComposer.value?.clear();
    successMessage.value = `${decisionLabel(decision)}: решение «${gate.title}» применено.`;
    Reflect.deleteProperty(comments.value, gate.ref);
    Reflect.deleteProperty(decisionDrafts.value, gate.ref);
    Reflect.deleteProperty(validationMessages.value, gate.ref);
    Reflect.deleteProperty(attachmentStates.value, gate.ref);
  } catch (error) {
    problem.value = asProblem(error);
    if (problem.value.kind === "conflict") await platform.loadGates();
  } finally {
    busyRef.value = "";
  }
}

function attachmentsReady(gateRef: string): boolean {
  return attachmentStates.value[gateRef]?.ready ?? true;
}

watch(
  () => selected.value?.gate,
  (gate) => {
    if (gate && !decisionDrafts.value[gate.ref]) {
      const primary = decisionActionLayout(gate).primary;
      if (primary) decisionDrafts.value[gate.ref] = primary;
    }
    void loadResolutionAttachments(gate);
    if (pageMounted) void loadGateAudit(gate);
  },
  { immediate: true },
);

function decisionLabel(decision: DecisionAction): string {
  if (decision === "APPROVE") return t("common.approve");
  if (decision === "REQUEST_CHANGES") return t("common.requestChanges");
  if (decision === "REJECT") return t("common.reject");
  return t("common.cancel");
}

function submitActionClass(decision?: DecisionAction): string[] {
  return [
    "button",
    decision === "REJECT" || decision === "CANCEL"
      ? "button--danger"
      : "button--primary",
  ];
}

onMounted(() => {
  pageMounted = true;
  void Promise.all([
    platform.loadGates(projectFilter.value || undefined),
    platform.loadProjects(),
    platform.loadRuns(),
  ]).then(() => loadGateAudit(selected.value?.gate));
});
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
    <div v-if="successMessage" class="decision-success" role="status">
      <CheckCircle2 :size="18" aria-hidden="true" />
      <span>{{ successMessage }}</span>
    </div>
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
                  <small
                    v-if="item.hasConsequences"
                    class="decision-row__impact"
                  >
                    Последствия: {{ item.gate.consequencesSummary }}
                  </small>
                  <small>
                    {{ item.gate.requestedBy.displayName }} ·
                    {{ formatDate(item.gate.openedAt) }}
                    <template v-if="item.gate.expiresAt">
                      · срок {{ formatDate(item.gate.expiresAt) }}
                    </template>
                  </small>
                  <small class="decision-row__route">
                    {{ item.run?.target.displayName }} ·
                    {{ item.run?.title ?? $t("decisions.runUnavailable") }} ·
                    {{ item.run?.sessionRef ?? "Session недоступна" }} ·
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
                  $t("decisions.run")
                }}
              </dt>
              <dd>
                <span v-if="selected.run" class="decision-target">
                  {{ selected.run.target.displayName }}
                </span>
                <RouterLink :to="runNodePath(selected)">
                  {{ selected.run?.title ?? $t("decisions.runUnavailable") }}
                </RouterLink>
              </dd>
            </div>
            <div>
              <dt>Session</dt>
              <dd>
                <code>{{ selected.run?.sessionRef ?? "не представлена" }}</code>
              </dd>
            </div>
            <div>
              <dt>Node</dt>
              <dd>
                <RouterLink :to="runNodePath(selected)">
                  <code>{{ selected.gate.nodeRef }}</code>
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
              <dt><UserRound :size="15" aria-hidden="true" />Инициатор Run</dt>
              <dd>
                {{ selected.run?.initiator.displayName ?? "не представлен" }}
              </dd>
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
                  view === "HISTORY"
                    ? "Вложения к решению"
                    : "Вложения к запросу"
                }}
              </dt>
              <dd>
                <span
                  v-if="resolutionAttachmentSets[selected.gate.ref]"
                  class="decision-attachment-summary"
                >
                  {{
                    $t("decisions.evidenceCount", {
                      count:
                        resolutionAttachmentSets[selected.gate.ref]
                          ?.itemCount ?? 0,
                    })
                  }}
                </span>
                <span v-else-if="view === 'HISTORY'">{{
                  $t("decisions.noEvidence")
                }}</span>
                <span v-else>Вложения исходного запроса не представлены</span>
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

          <section
            class="decision-audit"
            aria-labelledby="decision-audit-title"
          >
            <header>
              <h3 id="decision-audit-title">
                <History :size="16" aria-hidden="true" /> Аудит
              </h3>
              <span v-if="platform.loading.audit">Загружаем…</span>
              <span v-else>{{ selectedAuditEvents.length }}</span>
            </header>
            <ProblemNotice
              v-if="platform.problems.audit"
              :problem="platform.problems.audit"
              compact
            />
            <ol v-else-if="selectedAuditEvents.length">
              <li v-for="event in selectedAuditEvents" :key="event.ref">
                <time :datetime="event.occurredAt">{{
                  formatDate(event.occurredAt)
                }}</time>
                <strong>{{ event.initiator.displayName }}</strong>
                <span>{{ event.safeSummary }}</span>
                <code>{{ event.action }} · {{ event.outcome }}</code>
              </li>
            </ol>
            <p v-else-if="!platform.loading.audit" class="audit-unavailable">
              События аудита для этого решения не найдены.
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
            <fieldset
              class="decision-options"
              :disabled="busyRef === selected.gate.ref"
            >
              <legend>Разрешённые варианты и последствия</legend>
              <label
                v-for="decision in selected.gate.allowedDecisions"
                :key="decision"
                class="decision-option"
                :class="{
                  'decision-option--selected': selectedDecision === decision,
                  'decision-option--danger':
                    decision === 'REJECT' || decision === 'CANCEL',
                }"
              >
                <input
                  type="radio"
                  :name="`decision-${selected.gate.ref}`"
                  :value="decision"
                  :checked="selectedDecision === decision"
                  @change="selectDecision(selected.gate, decision)"
                />
                <span class="decision-option__copy">
                  <strong>{{ decisionLabel(decision) }}</strong>
                  <span>{{
                    decisionConsequence(selected.gate, decision)
                  }}</span>
                  <small v-if="requiresDecisionComment(decision)">
                    Комментарий обязателен
                  </small>
                </span>
                <StatusBadge :state="decisionOutcomeState(decision)" />
              </label>
            </fieldset>
            <label class="field decision-comment">
              <span>
                {{ decisionCommentLabel(selectedDecision) }}
                <strong v-if="requiresDecisionComment(selectedDecision)">
                  обязательно
                </strong>
              </span>
              <textarea
                v-model="comments[selected.gate.ref]"
                maxlength="4000"
                :required="requiresDecisionComment(selectedDecision)"
                :disabled="busyRef === selected.gate.ref"
                :aria-invalid="Boolean(validationMessages[selected.gate.ref])"
                :placeholder="decisionCommentPlaceholder(selectedDecision)"
                @input="clearValidation(selected.gate.ref)"
              />
            </label>
            <AttachmentComposer
              :key="selected.gate.ref"
              ref="selectedAttachmentComposer"
              purpose="OWNER_GATE_MESSAGE"
              :project-ref="selected.gate.projectRef"
              :disabled="busyRef === selected.gate.ref"
              @change="attachmentStates[selected.gate.ref] = $event"
            />
            <div
              v-if="validationMessages[selected.gate.ref]"
              class="decision-validation"
              role="alert"
            >
              <MessageSquareWarning :size="17" aria-hidden="true" />
              {{ validationMessages[selected.gate.ref] }}
            </div>
            <div class="decision-actions">
              <button
                v-if="selectedDecision"
                :class="submitActionClass(selectedDecision)"
                type="button"
                :disabled="
                  busyRef === selected.gate.ref ||
                  !attachmentsReady(selected.gate.ref)
                "
                :aria-busy="busyRef === selected.gate.ref"
                @click="decide(selected.gate)"
              >
                <LoaderCircle
                  v-if="busyRef === selected.gate.ref"
                  class="decision-spin"
                  :size="16"
                  aria-hidden="true"
                />
                {{
                  busyRef === selected.gate.ref
                    ? "Применяем решение…"
                    : decisionLabel(selectedDecision)
                }}
              </button>
              <span>
                Решение применится с версией {{ selected.gate.version }} и будет
                записано в аудит.
              </span>
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
.decision-success {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 10px;
  padding: 10px 12px;
  border: 1px solid color-mix(in srgb, var(--success) 32%, var(--border));
  border-radius: 8px;
  color: var(--text-secondary);
  background: var(--success-soft);
}
.decision-success svg {
  flex: 0 0 auto;
  color: var(--success);
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
.decision-row__copy .decision-row__impact {
  display: -webkit-box;
  overflow: hidden;
  color: var(--muted);
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 2;
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
.decision-meta code {
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
.decision-audit {
  display: grid;
  gap: 9px;
  margin-top: 16px;
  padding-top: 14px;
  border-top: 1px solid var(--border);
}
.decision-audit > header,
.decision-audit h3 {
  display: flex;
  align-items: center;
  gap: 7px;
}
.decision-audit > header {
  justify-content: space-between;
}
.decision-audit h3,
.decision-audit p {
  margin: 0;
}
.decision-audit > header > span,
.audit-unavailable {
  color: var(--muted);
  font-size: 0.76rem;
}
.decision-audit ol {
  display: grid;
  gap: 7px;
  margin: 0;
  padding: 0;
  list-style: none;
}
.decision-audit li {
  display: grid;
  grid-template-columns: minmax(120px, auto) minmax(120px, 0.4fr) minmax(0, 1fr);
  gap: 5px 10px;
  padding: 9px;
  border: 1px solid var(--border);
  border-radius: 7px;
  background: var(--panel);
  font-size: 0.78rem;
}
.decision-audit li time {
  color: var(--muted);
}
.decision-audit li code {
  grid-column: 1 / -1;
  color: var(--subtle);
  font-size: 0.7rem;
}
.decision-options {
  display: grid;
  gap: 7px;
  margin: 18px 0 0;
  padding: 0;
  border: 0;
}
.decision-options legend {
  margin-bottom: 7px;
  font-weight: 600;
}
.decision-option {
  display: grid;
  grid-template-columns: 18px minmax(0, 1fr) auto;
  align-items: start;
  gap: 9px;
  padding: 10px;
  border: 1px solid var(--border);
  border-radius: 7px;
  background: var(--surface);
  cursor: pointer;
}
.decision-option--selected {
  border-color: var(--accent);
  background: var(--accent-soft);
}
.decision-option--selected.decision-option--danger {
  border-color: var(--danger);
  background: var(--danger-soft);
}
.decision-option input {
  width: 16px;
  height: 16px;
  margin: 2px 0 0;
}
.decision-option__copy {
  display: grid;
  min-width: 0;
  gap: 3px;
}
.decision-option__copy > span {
  color: var(--muted);
  font-size: 0.78rem;
  line-height: 1.4;
}
.decision-option__copy small {
  color: var(--danger);
  font-size: 0.7rem;
}
.decision-comment {
  margin-top: 18px;
}
.decision-comment > span {
  display: flex;
  justify-content: space-between;
  gap: 8px;
}
.decision-comment > span strong {
  color: var(--danger);
  font-size: 0.72rem;
}
.decision-comment textarea {
  min-height: 92px;
}
.decision-validation {
  display: flex;
  align-items: flex-start;
  gap: 7px;
  margin-top: 10px;
  padding: 9px 10px;
  border: 1px solid color-mix(in srgb, var(--danger) 32%, var(--border));
  border-radius: 7px;
  color: var(--danger);
  background: var(--danger-soft);
  font-size: 0.8rem;
}
.decision-validation svg {
  flex: 0 0 auto;
}
.decision-actions {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: 12px;
}
.decision-actions .button {
  min-width: 190px;
}
.decision-actions > span {
  color: var(--muted);
  font-size: 0.76rem;
}
.decision-spin {
  animation: decision-spin 0.8s linear infinite;
}
@keyframes decision-spin {
  to {
    transform: rotate(360deg);
  }
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
  .decision-audit li,
  .decision-option {
    grid-template-columns: 1fr;
  }
  .decision-option input {
    position: absolute;
    opacity: 0;
  }
  .decision-actions {
    align-items: stretch;
    flex-direction: column;
  }
  .decision-actions .button {
    width: 100%;
  }
}
</style>
