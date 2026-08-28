<script setup lang="ts">
import {
  Bot,
  CalendarClock,
  CircleHelp,
  FolderPlus,
  ListChecks,
  Pencil,
  Play,
  PlugZap,
  ShieldCheck,
  UserPlus,
  Workflow,
  X,
} from "@lucide/vue";
import { computed, nextTick, onMounted, ref, type Component } from "vue";
import { useRoute } from "vue-router";
import { useI18n } from "vue-i18n";

import { usePlatformStore } from "@/features/platform/store";
import { useRealtimeStore } from "@/features/realtime/store";
import { selectedConversation } from "@/features/platform/assistant-selection";
import type {
  AssistantPlan,
  AssistantPlanOperation,
} from "@/shared/api/generated/openapi";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import AsyncState from "@/shared/ui/AsyncState.vue";
import PageFrame from "@/shared/ui/PageFrame.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import SafeMarkdown from "@/shared/ui/SafeMarkdown.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

const platform = usePlatformStore();
const realtime = useRealtimeStore();
const route = useRoute();
const { t } = useI18n();
const selectedRef = ref<string | null>();
const message = ref("");
const busy = ref(false);
const problem = ref<AppProblem>();
const log = ref<HTMLElement>();
const messageInput = ref<HTMLTextAreaElement>();
const rejectedPlanRefs = ref<ReadonlySet<string>>(new Set());
const projectRef = computed(() =>
  typeof route.query.projectRef === "string"
    ? route.query.projectRef
    : undefined,
);
const conversationList = computed(() =>
  Object.values(platform.conversations).sort((a, b) =>
    b.updatedAt.localeCompare(a.updatedAt),
  ),
);
const conversation = computed(() =>
  selectedConversation(conversationList.value, selectedRef.value),
);
const canCreateConversation = computed(() =>
  platform.assistant?.nextActions.includes("CREATE_CONVERSATION"),
);
const canSend = computed(() =>
  Boolean(
    platform.assistant?.nextActions.includes("ADD_TURN") &&
    realtime.platformState.state === "live",
  ),
);
const canApply = computed(() => realtime.platformState.state === "live");
const examples = computed(() => [
  {
    label: t("projects.new"),
    prompt: t("projects.emptyTitle"),
    icon: FolderPlus,
  },
  {
    label: t("agents.new"),
    prompt: t("agents.emptyTitle"),
    icon: UserPlus,
  },
  {
    label: t("workflows.new"),
    prompt: t("workflows.emptyTitle"),
    icon: Workflow,
  },
]);

const operationIcons: Record<AssistantPlanOperation["type"], Component> = {
  CREATE_PROJECT: FolderPlus,
  CREATE_AGENT: Bot,
  CREATE_WORKFLOW: Workflow,
  CHANGE_CAPABILITY: ShieldCheck,
  CHANGE_INTEGRATION_GRANT: ShieldCheck,
  CREATE_SCHEDULE: CalendarClock,
  LAUNCH_RUN: Play,
  CREATE_INTEGRATION_CONNECTION: PlugZap,
  TEST_INTEGRATION_CONNECTION: ListChecks,
};

function operationIcon(type: AssistantPlanOperation["type"]): Component {
  return operationIcons[type];
}

function planState(plan: AssistantPlan): string {
  if (plan.applied) return "APPLIED";
  if (rejectedPlanRefs.value.has(plan.ref)) return "REJECTED";
  if (plan.nextActions.includes("APPLY_PLAN")) return "WAITING_HUMAN";
  return "UNAVAILABLE";
}

function chooseExample(prompt: string): void {
  message.value = prompt;
  void nextTick(() => messageInput.value?.focus());
}

function requestPlanChange(): void {
  message.value = t("common.requestChanges");
  void nextTick(() => messageInput.value?.focus());
}

function rejectPlan(planRef: string): void {
  rejectedPlanRefs.value = new Set([...rejectedPlanRefs.value, planRef]);
}

async function ensureConversation(): Promise<string | undefined> {
  if (conversation.value) return conversation.value.ref;
  if (!canCreateConversation.value) return undefined;
  const created = await platform.newConversation(
    t("assistant.newConversation"),
    projectRef.value,
  );
  selectedRef.value = created.ref;
  return created.ref;
}

async function send(): Promise<void> {
  const content = message.value.trim();
  if (!content || !canSend.value) return;
  busy.value = true;
  problem.value = undefined;
  try {
    const ref = await ensureConversation();
    if (!ref) return;
    const updated = await platform.sendAssistantTurn(ref, content);
    selectedRef.value = updated.ref;
    message.value = "";
    await nextTick();
    log.value?.scrollTo({ top: log.value.scrollHeight, behavior: "smooth" });
  } catch (error) {
    problem.value = asProblem(error);
  } finally {
    busy.value = false;
  }
}

async function apply(planRef: string, version: number): Promise<void> {
  busy.value = true;
  problem.value = undefined;
  try {
    await platform.applyPlan(planRef, version);
  } catch (error) {
    problem.value = asProblem(error);
  } finally {
    busy.value = false;
  }
}

onMounted(() => void platform.loadAssistant());
</script>

<template>
  <PageFrame
    :title="$t('assistant.title')"
    :subtitle="$t('assistant.subtitle')"
  >
    <template #actions
      ><StatusBadge
        v-if="platform.assistant"
        :state="platform.assistant.runtimeState"
    /></template>
    <AsyncState
      :loading="platform.loading.assistant"
      :problem="platform.problems.assistant"
      @retry="platform.loadAssistant()"
    >
      <div class="assistant-workspace">
        <aside class="conversation-list">
          <button
            v-if="canCreateConversation"
            class="button button--primary"
            type="button"
            @click="selectedRef = null"
          >
            {{ $t("assistant.newConversation") }}
          </button>
          <button
            v-for="item in conversationList"
            :key="item.ref"
            type="button"
            :class="{ selected: item.ref === conversation?.ref }"
            @click="selectedRef = item.ref"
          >
            <strong>{{ item.title }}</strong
            ><small>{{ new Date(item.updatedAt).toLocaleString() }}</small>
          </button>
        </aside>
        <section class="assistant-chat">
          <header>
            <div>
              <strong>{{ $t("assistant.ready") }}</strong>
              <p>{{ $t("assistant.system") }}</p>
            </div>
            <span class="audit-note">{{ $t("assistant.audit") }}</span>
          </header>
          <div ref="log" class="chat-log" role="log" aria-live="polite">
            <div v-if="!conversation?.turns.length" class="assistant-empty">
              <CircleHelp :size="28" aria-hidden="true" />
              <h2>{{ $t("assistant.ready") }}</h2>
              <p>{{ $t("assistant.empty") }}</p>
              <div class="assistant-empty__examples">
                <button
                  v-for="example in examples"
                  :key="example.label"
                  class="assistant-example"
                  type="button"
                  @click="chooseExample(example.prompt)"
                >
                  <component :is="example.icon" :size="18" aria-hidden="true" />
                  <span>{{ example.label }}</span>
                </button>
              </div>
            </div>
            <article
              v-for="turn in conversation?.turns ?? []"
              :key="turn.ref"
              class="chat-turn"
              :class="[
                `chat-turn--${turn.role.toLowerCase()}`,
                { 'chat-turn--plan': turn.plan },
              ]"
            >
              <div class="chat-turn__meta">
                <span>{{
                  turn.role === "USER"
                    ? $t("common.input")
                    : $t("app.assistantShort")
                }}</span
                ><StatusBadge :state="turn.state" />
              </div>
              <SafeMarkdown v-if="!turn.plan" :content="turn.content" />
              <section v-if="turn.plan" class="assistant-plan">
                <header class="assistant-plan__header">
                  <span class="assistant-plan__icon" aria-hidden="true">
                    <ListChecks :size="20" />
                  </span>
                  <div>
                    <h3>{{ $t("assistant.plan") }}</h3>
                    <SafeMarkdown
                      class="assistant-plan__summary"
                      :content="turn.plan.auditSummary"
                    />
                  </div>
                  <StatusBadge :state="planState(turn.plan)" />
                </header>
                <ul class="assistant-plan__operations">
                  <li
                    v-for="operation in turn.plan.operations"
                    :key="operation.ref"
                    class="assistant-operation"
                  >
                    <span class="assistant-operation__icon" aria-hidden="true">
                      <component
                        :is="operationIcon(operation.type)"
                        :size="18"
                      />
                    </span>
                    <div class="assistant-operation__copy">
                      <strong>{{ operation.title }}</strong>
                      <SafeMarkdown :content="operation.summary" />
                      <SafeMarkdown
                        v-if="
                          !operation.permitted && operation.unavailableReason
                        "
                        class="assistant-operation__reason"
                        :content="operation.unavailableReason"
                      />
                    </div>
                    <StatusBadge
                      :state="operation.permitted ? 'READY' : 'UNAVAILABLE'"
                    />
                  </li>
                </ul>
                <footer
                  v-if="
                    !turn.plan.applied &&
                    turn.plan.nextActions.includes('APPLY_PLAN')
                  "
                  class="assistant-plan__footer"
                >
                  <template v-if="!rejectedPlanRefs.has(turn.plan.ref)">
                    <button
                      class="button button--primary"
                      type="button"
                      :disabled="busy || !canApply"
                      @click="apply(turn.plan!.ref, turn.plan!.version)"
                    >
                      <ListChecks :size="17" aria-hidden="true" />
                      {{ $t("assistant.applyPlan") }}
                    </button>
                    <button
                      class="button"
                      type="button"
                      :disabled="busy"
                      @click="requestPlanChange"
                    >
                      <Pencil :size="17" aria-hidden="true" />
                      {{ $t("common.edit") }}
                    </button>
                    <button
                      class="button button--ghost"
                      type="button"
                      :disabled="busy"
                      @click="rejectPlan(turn.plan!.ref)"
                    >
                      <X :size="17" aria-hidden="true" />
                      {{ $t("common.reject") }}
                    </button>
                  </template>
                  <button
                    v-else
                    class="button"
                    type="button"
                    @click="requestPlanChange"
                  >
                    <Pencil :size="17" aria-hidden="true" />
                    {{ $t("common.requestChanges") }}
                  </button>
                  <span class="assistant-plan__audit">{{
                    $t("assistant.audit")
                  }}</span>
                </footer>
              </section>
            </article>
          </div>
          <ProblemNotice v-if="problem" :problem="problem" compact />
          <form class="composer" @submit.prevent="send">
            <label class="sr-only" for="assistant-message">{{
              $t("assistant.message")
            }}</label
            ><textarea
              id="assistant-message"
              ref="messageInput"
              v-model="message"
              :placeholder="$t('assistant.message')"
              maxlength="8000"
              :disabled="!canSend"
              @keydown.ctrl.enter="send"
            /><button
              v-if="canSend"
              class="button button--primary"
              type="submit"
              :disabled="busy || !message.trim()"
            >
              {{ $t("assistant.send") }}
            </button>
          </form>
        </section>
      </div>
    </AsyncState>
  </PageFrame>
</template>

<style scoped>
.assistant-workspace {
  display: grid;
  grid-template-columns: 260px minmax(0, 1fr);
  min-height: calc(100vh - 190px);
  border: 1px solid var(--border);
  border-radius: 11px;
  overflow: hidden;
  background: var(--surface);
}
.conversation-list {
  display: flex;
  flex-direction: column;
  gap: 7px;
  padding: 14px;
  border-right: 1px solid var(--border);
  background: var(--panel);
}
.conversation-list > button:not(.button) {
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding: 10px;
  border: 0;
  border-radius: 7px;
  background: transparent;
  text-align: left;
  cursor: pointer;
}
.conversation-list > button.selected {
  background: var(--accent-soft);
}
.conversation-list small {
  color: var(--muted);
}
.assistant-chat {
  display: grid;
  grid-template-rows: auto 1fr auto auto;
  min-width: 0;
}
.assistant-chat > header {
  display: flex;
  justify-content: space-between;
  gap: 18px;
  padding: 15px 18px;
  border-bottom: 1px solid var(--border);
}
.assistant-chat header p {
  margin: 2px 0 0;
  color: var(--muted);
}
.audit-note {
  max-width: 340px;
  color: var(--muted);
  font-size: 0.82rem;
}
.chat-log {
  overflow-y: auto;
  padding: 20px;
}
.assistant-empty {
  display: grid;
  justify-items: center;
  gap: 10px;
  max-width: 620px;
  margin: 80px auto;
  color: var(--muted);
  text-align: center;
}
.assistant-empty h2,
.assistant-empty p {
  margin: 0;
}
.assistant-empty h2 {
  color: var(--text);
  font-size: 1.08rem;
}
.assistant-empty__examples {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 8px;
  width: min(100%, 620px);
  margin-top: 8px;
}
.assistant-example {
  display: grid;
  grid-template-columns: 22px minmax(0, 1fr);
  align-items: center;
  gap: 7px;
  min-height: 52px;
  padding: 9px 10px;
  border: 1px solid var(--border);
  border-radius: 7px;
  background: var(--surface);
  color: var(--text);
  text-align: left;
  cursor: pointer;
}
.assistant-example:hover {
  border-color: var(--accent);
}
.chat-turn {
  max-width: 760px;
  margin: 0 0 16px;
  padding: 13px 15px;
  border: 1px solid var(--border);
  border-radius: 10px;
}
.chat-turn--user {
  margin-left: auto;
  background: var(--accent-soft);
}
.chat-turn--plan {
  max-width: 840px;
  padding: 0;
  border: 0;
}
.chat-turn--plan .chat-turn__meta {
  padding-inline: 2px;
}
.chat-turn__meta {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 7px;
  font-size: 0.82rem;
  font-weight: 600;
}
.chat-turn p {
  white-space: pre-wrap;
}
.assistant-plan {
  overflow: hidden;
  border: 1px solid #cbdff3;
  border-radius: 8px;
  background: var(--surface);
}
.assistant-plan__header {
  display: grid;
  grid-template-columns: 34px minmax(0, 1fr) auto;
  align-items: start;
  gap: 10px;
  padding: 14px;
  border-bottom: 1px solid var(--border);
}
.assistant-plan__header h3 {
  margin: 0 0 3px;
  font-size: 1rem;
}
.assistant-plan__icon,
.assistant-operation__icon {
  display: inline-grid;
  place-items: center;
  color: var(--accent);
}
.assistant-plan__icon {
  width: 32px;
  height: 32px;
  border-radius: 7px;
  background: var(--accent-soft);
}
.assistant-plan__operations {
  display: grid;
  gap: 0;
  margin: 0;
  padding: 0;
  list-style: none;
}
.assistant-plan__summary {
  color: var(--muted);
}
.assistant-plan__summary :deep(p),
.assistant-operation__copy :deep(p) {
  margin: 0;
}
.assistant-operation {
  display: grid;
  grid-template-columns: 24px minmax(0, 1fr) auto;
  align-items: start;
  gap: 10px;
  padding: 12px 14px;
  border-bottom: 1px solid var(--border);
}
.assistant-operation__icon {
  padding-top: 1px;
}
.assistant-operation__copy {
  display: grid;
  gap: 3px;
  min-width: 0;
}
.assistant-operation__copy :deep(.safe-markdown) {
  color: var(--muted);
  font-size: 0.88rem;
}
.assistant-operation__reason {
  color: var(--danger);
  font-size: 0.82rem;
}
.assistant-operation__reason :deep(p) {
  margin: 2px 0 0;
}
.assistant-plan__footer {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
  padding: 12px 14px;
  background: var(--panel);
}
.assistant-plan__footer .button {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}
.assistant-plan__audit {
  margin-left: auto;
  color: var(--muted);
  font-size: 0.78rem;
}
.composer {
  display: grid;
  grid-template-columns: 1fr auto;
  gap: 10px;
  padding: 14px;
  border-top: 1px solid var(--border);
}
.composer textarea {
  min-height: 68px;
}
.composer button {
  align-self: end;
}
@media (max-width: 800px) {
  .assistant-workspace {
    grid-template-columns: 1fr;
  }
  .conversation-list {
    display: none;
  }
  .assistant-chat > header {
    flex-direction: column;
  }
  .composer {
    grid-template-columns: 1fr;
  }
  .composer button {
    width: 100%;
  }
  .chat-log {
    padding: 12px;
  }
  .assistant-empty__examples {
    grid-template-columns: 1fr;
  }
  .assistant-plan__header,
  .assistant-operation {
    grid-template-columns: 28px minmax(0, 1fr);
  }
  .assistant-plan__header > .status-badge,
  .assistant-operation > .status-badge {
    grid-column: 2;
    justify-self: start;
  }
  .assistant-plan__audit {
    width: 100%;
    margin-left: 0;
  }
}
</style>
