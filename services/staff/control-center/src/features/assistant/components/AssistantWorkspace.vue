<script setup lang="ts">
import {
  Activity,
  Archive,
  Bot,
  Check,
  ChevronDown,
  History,
  ListChecks,
  Pencil,
  Plus,
  Send,
  Sparkles,
  X,
} from "@lucide/vue";
import {
  computed,
  nextTick,
  onBeforeUnmount,
  onMounted,
  ref,
  watch,
} from "vue";
import { useI18n } from "vue-i18n";

import AssistantPlanEditor from "@/features/assistant/components/AssistantPlanEditor.vue";
import AssistantHistoryFilter from "./AssistantHistoryFilter.vue";
import {
  assistantContextIdentity,
  conversationMatchesContext,
  readableContextOperations,
  readableContextKind,
} from "@/features/assistant/context";
import { openAssistantEvent } from "@/features/assistant/events";
import {
  assistantEffectiveRuntimeState,
  operationActionLabel,
  operationTargetLabel,
} from "@/features/assistant/model";
import { useAssistantStore } from "@/features/assistant/store";
import {
  persistAssistantWorkspaceOpen,
  restoreAssistantWorkspaceOpen,
} from "@/features/assistant/workspace-state";
import RunActivityView from "@/features/runs/RunActivityView.vue";
import type {
  AssistantContextDescriptor,
  AssistantPlan,
  RunEvent,
} from "@/shared/api/generated/openapi/types.gen";
import { AppProblem } from "@/shared/api/problem";
import {
  focusableElements,
  trappedFocusTarget,
} from "@/shared/ui/dialog-focus";
import AttachmentComposer from "@/shared/ui/AttachmentComposer.vue";
import type {
  AttachmentComposerHandle,
  AttachmentComposerState,
} from "@/shared/ui/attachment-composer";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import SafeMarkdown from "@/shared/ui/SafeMarkdown.vue";
import SafeStructuredData from "@/shared/ui/SafeStructuredData.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";
import VoiceTextarea from "@/shared/ui/VoiceTextarea.vue";

const props = withDefaults(
  defineProps<{
    context: AssistantContextDescriptor;
    projectRef?: string;
    live?: boolean;
    runEvents?: readonly RunEvent[];
    refreshRevision?: string;
  }>(),
  { live: false, runEvents: () => [], refreshRevision: "" },
);
const { t } = useI18n();
const store = useAssistantStore();
const open = ref(restoreAssistantWorkspaceOpen());
const historyOpen = ref(false);
const message = ref("");
const titleDraft = ref("");
const titleEditing = ref(false);
const openPlanRef = ref<string>();
const activeView = ref<"CHAT" | "ACTIVITY">("CHAT");
const attachmentComposer = ref<AttachmentComposerHandle>();
const attachmentState = ref<AttachmentComposerState>({
  count: 0,
  uploadedCount: 0,
  totalBytes: 0,
  busy: false,
  hasErrors: false,
  overLimit: false,
  ready: true,
});
const panel = ref<HTMLElement>();
const composer = ref<{ focus(): void }>();
const chatLog = ref<HTMLElement>();
const historyMenu = ref<HTMLElement>();
const fab = ref<HTMLButtonElement>();

const checkedContext = computed(() => {
  const conversation = store.selectedConversation;
  return !store.loading &&
    !store.problem &&
    conversation &&
    conversationMatchesContext(conversation, props.context)
    ? conversation.context
    : undefined;
});
const contextTitle = computed(
  () => checkedContext.value?.entityName || props.context.route || "Kodex",
);
const checkedOperations = computed(() =>
  checkedContext.value
    ? readableContextOperations(checkedContext.value.allowedOperations)
    : undefined,
);
const checkedContextKind = computed(() =>
  readableContextKind(checkedContext.value?.entityKind ?? ""),
);
const currentPlan = computed<AssistantPlan | undefined>(() => {
  if (!openPlanRef.value) return undefined;
  for (const conversation of store.conversations) {
    for (const turn of conversation.turns) {
      if (turn.plan?.ref === openPlanRef.value) return turn.plan;
    }
  }
  return undefined;
});
const assistantRuntimeState = computed(() =>
  store.assistant
    ? assistantEffectiveRuntimeState(store.assistant)
    : "RECOVERING",
);
const canCreateConversation = computed(
  () =>
    assistantRuntimeState.value === "READY" &&
    Boolean(store.assistant?.nextActions.includes("CREATE_CONVERSATION")),
);
const canSend = computed(
  () =>
    props.live &&
    !store.loading &&
    !store.busy &&
    assistantRuntimeState.value === "READY" &&
    Boolean(store.assistant?.nextActions.includes("ADD_TURN")) &&
    (!store.selectedConversation ||
      store.selectedConversation.state === "ACTIVE") &&
    (Boolean(store.selectedConversation) ||
      (!store.historyQuery && store.historyState === "ACTIVE")) &&
    Boolean(store.selectedConversation || canCreateConversation.value) &&
    attachmentState.value.ready,
);
const canStartConversation = computed(
  () =>
    props.live && !store.loading && !store.busy && canCreateConversation.value,
);
const isRunContext = computed(() => props.context.entityKind === "RUN");

const contextIdentity = computed(() =>
  assistantContextIdentity(props.context, props.projectRef),
);

function handleOpenAssistant(): void {
  void show();
}

async function show(): Promise<void> {
  open.value = true;
  persistAssistantWorkspaceOpen(true);
  historyOpen.value = false;
  openPlanRef.value = undefined;
  activeView.value = "CHAT";
  await store.load(props.context, props.projectRef);
  await nextTick();
  panel.value?.focus();
}

function close(): void {
  if (store.busy) return;
  if (
    (message.value.trim() ||
      attachmentState.value.count > 0 ||
      attachmentState.value.busy) &&
    !window.confirm(t("assistant.closeWithDraftConfirm"))
  )
    return;
  store.cancelReads();
  open.value = false;
  persistAssistantWorkspaceOpen(false);
  historyOpen.value = false;
  openPlanRef.value = undefined;
  store.clearReceipt();
  void nextTick(() => fab.value?.focus());
}

function handleKeydown(event: KeyboardEvent): void {
  if (event.key === "Escape") {
    if (historyOpen.value) historyOpen.value = false;
    else if (openPlanRef.value) openPlanRef.value = undefined;
    else close();
    return;
  }
  if (event.key !== "Tab" || !panel.value) return;
  const target = trappedFocusTarget(
    focusableElements(panel.value),
    document.activeElement,
    event.shiftKey,
  );
  if (!target) return;
  event.preventDefault();
  target.focus();
}

function chooseConversation(ref?: string): void {
  store.selectedRef = ref;
  historyOpen.value = false;
  titleEditing.value = false;
  attachmentComposer.value?.clear();
}

async function startConversation(): Promise<void> {
  historyOpen.value = false;
  titleEditing.value = false;
  openPlanRef.value = undefined;
  if (!(await handleStoreMutation(() => store.startConversation()))) return;
  attachmentComposer.value?.clear();
  await nextTick();
  composer.value?.focus();
}

async function handleStoreMutation(
  operation: () => Promise<unknown>,
): Promise<boolean> {
  try {
    await operation();
    return true;
  } catch (error) {
    if (!(error instanceof AppProblem)) throw error;
    return false;
  }
}

function conversationDisplayTitle(title: string): string {
  return (
    title.trim() ||
    t("assistant.contextConversation", { context: contextTitle.value })
  );
}

function startTitleEdit(): void {
  if (
    !store.selectedConversation ||
    store.selectedConversation.state === "ARCHIVED"
  )
    return;
  titleDraft.value = store.selectedConversation.title;
  titleEditing.value = true;
}
async function archiveSelected(): Promise<void> {
  if (
    !props.live ||
    store.busy ||
    store.loading ||
    !store.selectedConversation ||
    store.selectedConversation.state === "ARCHIVED"
  )
    return;
  if (!window.confirm(t("assistant.archiveConfirm"))) return;
  if (await handleStoreMutation(() => store.archiveSelected())) {
    titleEditing.value = false;
    openPlanRef.value = undefined;
    attachmentComposer.value?.clear();
  }
}

async function saveTitle(): Promise<void> {
  if (!(await handleStoreMutation(() => store.changeTitle(titleDraft.value))))
    return;
  titleEditing.value = false;
}

async function send(): Promise<void> {
  const value = message.value.trim();
  if (!value || !canSend.value) return;
  const attachmentSetRef = await attachmentComposer.value?.finalize();
  if (!(await handleStoreMutation(() => store.send(value, attachmentSetRef))))
    return;
  message.value = "";
  attachmentComposer.value?.clear();
  await nextTick();
  scrollToLatest();
  composer.value?.focus();
}

function scrollToLatest(): void {
  chatLog.value?.scrollTo({ top: chatLog.value.scrollHeight });
}

function handleComposerKeydown(event: KeyboardEvent): void {
  if (event.key !== "Enter" || event.shiftKey) return;
  event.preventDefault();
  void send();
}

function openPlan(plan: AssistantPlan): void {
  store.clearReceipt();
  openPlanRef.value = plan.ref;
}

async function savePlan(
  summary: string,
  operations: Parameters<typeof store.saveDraft>[2],
): Promise<void> {
  const plan = currentPlan.value;
  if (!plan) return;
  await handleStoreMutation(() => store.saveDraft(plan, summary, operations));
}

async function validatePlan(): Promise<void> {
  const plan = currentPlan.value;
  if (plan) await handleStoreMutation(() => store.validate(plan));
}

async function applyPlan(): Promise<void> {
  const plan = currentPlan.value;
  if (plan) await handleStoreMutation(() => store.apply(plan));
}

async function rejectPlan(): Promise<void> {
  const plan = currentPlan.value;
  if (plan) await handleStoreMutation(() => store.reject(plan));
}

function documentPointerDown(event: PointerEvent): void {
  if (
    historyOpen.value &&
    historyMenu.value &&
    !historyMenu.value.contains(event.target as Node)
  )
    historyOpen.value = false;
}

watch(contextIdentity, () => {
  store.setContext(props.context, props.projectRef);
  openPlanRef.value = undefined;
  activeView.value = "CHAT";
  attachmentComposer.value?.clear();
  if (open.value) void store.load(props.context, props.projectRef);
});
watch(
  [() => props.context, () => props.projectRef] as const,
  ([nextContext, nextProjectRef], [previousContext, previousProjectRef]) => {
    if (
      assistantContextIdentity(nextContext, nextProjectRef) !==
      assistantContextIdentity(previousContext, previousProjectRef)
    )
      return;
    store.setContext(nextContext, nextProjectRef);
  },
);
watch(
  () => props.refreshRevision,
  (value, previous) => {
    if (open.value && value !== previous)
      void store.load(props.context, props.projectRef);
  },
);
watch(
  () => store.selectedConversation?.ref,
  async () => {
    titleEditing.value = false;
    openPlanRef.value = undefined;
    store.clearReceipt();
    await nextTick();
    scrollToLatest();
  },
);
watch(
  () => store.selectedConversation?.turns.length,
  async () => {
    if (!open.value || openPlanRef.value) return;
    await nextTick();
    scrollToLatest();
  },
);

onMounted(() => {
  document.addEventListener("pointerdown", documentPointerDown);
  window.addEventListener(openAssistantEvent, handleOpenAssistant);
  if (open.value) void show();
});
onBeforeUnmount(() => {
  store.cancelReads();
  document.removeEventListener("pointerdown", documentPointerDown);
  window.removeEventListener(openAssistantEvent, handleOpenAssistant);
  persistAssistantWorkspaceOpen(false);
});
</script>

<template>
  <button
    ref="fab"
    class="assistant-fab"
    type="button"
    :aria-label="$t('assistant.open')"
    :aria-expanded="open"
    aria-controls="assistant-workspace"
    @click="show"
  >
    <Sparkles :size="24" aria-hidden="true" />
  </button>

  <div v-if="open" class="assistant-overlay" role="presentation">
    <button
      class="assistant-overlay__backdrop"
      type="button"
      :aria-label="$t('common.close')"
      :disabled="store.busy"
      @click="close"
    />
    <aside
      id="assistant-workspace"
      ref="panel"
      class="assistant-drawer"
      :class="{ 'assistant-drawer--plan': currentPlan }"
      role="dialog"
      aria-modal="true"
      :aria-label="$t('assistant.title')"
      :aria-busy="store.busy || store.loading"
      :data-conversation-ref="store.selectedConversation?.ref"
      tabindex="-1"
      @keydown="handleKeydown"
    >
      <header class="assistant-drawer__header">
        <span class="assistant-drawer__mark" aria-hidden="true">
          <Bot :size="21" />
        </span>
        <div class="assistant-drawer__identity">
          <strong>Kodex</strong>
          <span>{{ contextTitle }}</span>
        </div>
        <StatusBadge
          v-if="store.assistant"
          :state="assistantRuntimeState"
          :label="store.assistant.readinessSummary"
        />
        <button
          class="assistant-new-conversation"
          type="button"
          :aria-label="$t('assistant.newConversation')"
          :title="$t('assistant.newConversation')"
          :disabled="!canStartConversation"
          @click="startConversation"
        >
          <Plus :size="19" aria-hidden="true" />
          <span>{{ $t("assistant.newConversation") }}</span>
        </button>
        <div ref="historyMenu" class="assistant-history">
          <button
            class="icon-button assistant-history__toggle"
            type="button"
            :aria-label="$t('assistant.history')"
            :aria-expanded="historyOpen"
            aria-haspopup="menu"
            @click="historyOpen = !historyOpen"
          >
            <History :size="18" aria-hidden="true" />
            <span>{{ $t("assistant.history") }}</span>
            <ChevronDown :size="14" aria-hidden="true" />
          </button>
          <section
            v-if="historyOpen"
            class="assistant-history__menu"
            :aria-label="$t('assistant.history')"
          >
            <header>{{ $t("assistant.history") }}</header>
            <AssistantHistoryFilter
              :query="store.historyQuery"
              :state="store.historyState"
              :disabled="store.busy"
              @change="store.filterHistory"
            />
            <button
              v-for="conversation in store.sortedConversations"
              :key="conversation.ref"
              type="button"
              :class="{
                selected: conversation.ref === store.selectedRef,
              }"
              @click="chooseConversation(conversation.ref)"
            >
              <span>
                <strong>{{
                  conversationDisplayTitle(conversation.title)
                }}</strong>
                <small>{{
                  new Date(conversation.updatedAt).toLocaleString()
                }}</small>
              </span>
              <Check
                v-if="conversation.ref === store.selectedRef"
                :size="16"
                aria-hidden="true"
              />
            </button>
            <ProblemNotice
              v-if="store.historyProblem"
              :problem="store.historyProblem"
              @retry="store.loadMoreHistory"
            />
            <button
              v-if="store.nextPageToken"
              type="button"
              :disabled="store.loading || store.loadingMore || store.busy"
              @click="store.loadMoreHistory"
            >
              <ChevronDown :size="16" />{{
                store.loadingMore ? $t("common.loading") : $t("common.loadMore")
              }}
            </button>
          </section>
        </div>
        <button
          class="icon-button"
          type="button"
          :aria-label="$t('common.close')"
          :disabled="store.busy"
          @click="close"
        >
          <X :size="20" aria-hidden="true" />
        </button>
      </header>

      <nav
        v-if="!currentPlan"
        class="assistant-conversation-sidebar"
        :aria-label="$t('assistant.history')"
      >
        <button
          class="button"
          type="button"
          :disabled="!canStartConversation"
          @click="startConversation"
        >
          <Plus :size="18" />{{ $t("assistant.newConversation") }}
        </button>
        <AssistantHistoryFilter
          :query="store.historyQuery"
          :state="store.historyState"
          :disabled="store.busy"
          @change="store.filterHistory"
        />
        <button
          v-for="conversation in store.sortedConversations"
          :key="conversation.ref"
          class="assistant-conversation-entry"
          :class="{ selected: conversation.ref === store.selectedRef }"
          type="button"
          @click="chooseConversation(conversation.ref)"
        >
          <strong>{{ conversationDisplayTitle(conversation.title) }}</strong>
          <time :datetime="conversation.updatedAt">{{
            new Date(conversation.updatedAt).toLocaleString()
          }}</time>
        </button>
        <ProblemNotice
          v-if="store.historyProblem"
          :problem="store.historyProblem"
          @retry="store.loadMoreHistory"
        />
        <button
          v-if="store.nextPageToken"
          class="button"
          type="button"
          :disabled="store.loading || store.loadingMore || store.busy"
          @click="store.loadMoreHistory"
        >
          <ChevronDown :size="16" />{{
            store.loadingMore ? $t("common.loading") : $t("common.loadMore")
          }}
        </button>
      </nav>

      <AssistantPlanEditor
        v-if="currentPlan"
        :plan="currentPlan"
        :receipt="store.receipt"
        :busy="store.busy"
        :readonly="store.selectedConversation?.state === 'ARCHIVED'"
        :problem="store.problem"
        @close="openPlanRef = undefined"
        @save="savePlan"
        @validate="validatePlan"
        @apply="applyPlan"
        @reject="rejectPlan"
      />
      <template v-else>
        <nav v-if="isRunContext" class="assistant-drawer__tabs">
          <button
            type="button"
            :class="{ selected: activeView === 'CHAT' }"
            @click="activeView = 'CHAT'"
          >
            <Sparkles :size="16" aria-hidden="true" />
            {{ $t("assistant.chat") }}
          </button>
          <button
            type="button"
            :class="{ selected: activeView === 'ACTIVITY' }"
            @click="activeView = 'ACTIVITY'"
          >
            <Activity :size="16" aria-hidden="true" />
            {{ $t("runs.activity") }}
          </button>
        </nav>

        <div class="assistant-drawer__view">
          <RunActivityView
            v-if="activeView === 'ACTIVITY'"
            :events="runEvents"
          />
          <div v-else class="assistant-chat-view">
            <section class="assistant-context-strip">
              <span>{{ $t("assistant.context") }}</span>
              <strong>{{ contextTitle }}</strong>
              <small>{{ context.route }}</small>
              <p v-if="checkedContext" class="assistant-context-details">
                <span v-if="checkedContextKind">{{
                  $t(`assistant.contextKind.${checkedContextKind}`)
                }}</span>
                <span v-if="checkedContext.entityVersion">{{
                  $t("assistant.contextVersion", {
                    version: checkedContext.entityVersion,
                  })
                }}</span>
                <span>{{ $t("assistant.contextOperations") }}</span>
                <span v-if="checkedOperations === undefined">{{
                  $t("assistant.contextOperationsUnknown")
                }}</span>
                <span v-else-if="!checkedOperations.length">{{
                  $t("assistant.contextReadOnly")
                }}</span>
                <span v-for="operation in checkedOperations" :key="operation">{{
                  $t(`assistant.contextOperation.${operation}`)
                }}</span>
              </p>
            </section>

            <section
              v-if="store.selectedConversation"
              class="assistant-conversation-title"
            >
              <form v-if="titleEditing" @submit.prevent="saveTitle">
                <input
                  v-model="titleDraft"
                  maxlength="160"
                  :disabled="store.busy"
                  :aria-label="$t('assistant.conversationTitle')"
                />
                <button
                  class="button button--primary"
                  type="submit"
                  :disabled="store.busy || !titleDraft.trim()"
                >
                  {{ $t("common.save") }}
                </button>
                <button
                  class="button"
                  type="button"
                  :disabled="store.busy"
                  @click="titleEditing = false"
                >
                  {{ $t("common.cancel") }}
                </button>
              </form>
              <template v-else>
                <strong>{{
                  conversationDisplayTitle(store.selectedConversation.title)
                }}</strong>
                <button
                  v-if="store.selectedConversation.state !== 'ARCHIVED'"
                  class="icon-button"
                  type="button"
                  :aria-label="$t('assistant.renameConversation')"
                  @click="startTitleEdit"
                >
                  <Pencil :size="16" aria-hidden="true" />
                </button>
                <button
                  v-if="store.selectedConversation.state !== 'ARCHIVED'"
                  class="icon-button"
                  type="button"
                  :disabled="
                    !live || store.busy || store.loading || !!store.problem
                  "
                  :aria-label="$t('assistant.archiveConversation')"
                  :title="$t('assistant.archiveConversation')"
                  @click="archiveSelected"
                >
                  <Archive :size="16" />
                </button>
                <StatusBadge v-else :state="store.selectedConversation.state" />
              </template>
            </section>

            <section
              ref="chatLog"
              class="assistant-chat-log"
              role="log"
              aria-live="polite"
            >
              <ProblemNotice
                v-if="store.problem"
                :problem="store.problem"
                @retry="store.load(context, projectRef)"
              />
              <div
                v-else-if="store.loading"
                class="assistant-empty-state"
                role="status"
              >
                <span class="spinner" aria-hidden="true" />
                <p>{{ $t("common.loading") }}</p>
              </div>
              <div
                v-else-if="!store.selectedConversation?.turns.length"
                class="assistant-empty-state"
              >
                <Sparkles :size="28" aria-hidden="true" />
                <h2>{{ $t("assistant.ready") }}</h2>
                <p>{{ $t("assistant.contextHelp") }}</p>
              </div>
              <article
                v-for="turn in store.selectedConversation?.turns ?? []"
                v-else
                :key="turn.ref"
                class="assistant-message"
                :class="`assistant-message--${turn.role.toLowerCase()}`"
                :data-turn-ref="turn.ref"
                :data-turn-sequence="turn.sequence"
              >
                <header>
                  <strong>{{
                    turn.role === "USER"
                      ? $t("common.input")
                      : turn.role === "SYSTEM_RECEIPT"
                        ? $t("assistant.receipt")
                        : "Kodex"
                  }}</strong>
                  <StatusBadge :state="turn.state" />
                </header>
                <SafeMarkdown :content="turn.content" />
                <section v-if="turn.plan" class="assistant-plan-card">
                  <header>
                    <ListChecks :size="19" aria-hidden="true" />
                    <div>
                      <strong>{{ $t("assistant.plan") }}</strong>
                      <span>{{
                        $t("assistant.planEditor.revision", {
                          revision: turn.plan.revision,
                          count: turn.plan.operations.length,
                        })
                      }}</span>
                    </div>
                    <StatusBadge :state="turn.plan.state" />
                  </header>
                  <SafeMarkdown :content="turn.plan.auditSummary" />
                  <ol class="assistant-plan-card__operations">
                    <li
                      v-for="operation in turn.plan.operations"
                      :key="operation.ref"
                    >
                      <header>
                        <span class="assistant-plan-card__action">
                          {{
                            $t(
                              `assistant.planEditor.actions.${operationActionLabel(operation.action)}`,
                            )
                          }}
                        </span>
                        <small>{{ operation.target.kind }}</small>
                      </header>
                      <strong class="assistant-plan-card__target">
                        {{ operationTargetLabel(operation.target) }}
                      </strong>
                      <span>{{ operation.title }}</span>
                      <p>{{ operation.summary }}</p>
                      <section class="assistant-plan-card__parameters">
                        <strong>{{
                          $t("assistant.planEditor.parametersTitle")
                        }}</strong>
                        <SafeStructuredData :value="operation.parameters" />
                      </section>
                    </li>
                  </ol>
                  <button
                    class="button button--primary"
                    type="button"
                    @click="openPlan(turn.plan)"
                  >
                    {{ $t("assistant.openPlan") }}
                  </button>
                </section>
              </article>
            </section>

            <footer class="assistant-composer">
              <AttachmentComposer
                ref="attachmentComposer"
                compact
                purpose="ASSISTANT_MESSAGE"
                :project-ref="projectRef"
                :disabled="
                  store.busy ||
                  !live ||
                  store.selectedConversation?.state === 'ARCHIVED' ||
                  store.selectedConversation?.state === 'CLOSED'
                "
                @change="attachmentState = $event"
              />
              <div class="assistant-composer__field">
                <VoiceTextarea
                  ref="composer"
                  v-model="message"
                  rows="2"
                  maxlength="32768"
                  :aria-label="$t('assistant.message')"
                  :placeholder="$t('assistant.message')"
                  :disabled="
                    store.busy ||
                    !live ||
                    store.selectedConversation?.state === 'ARCHIVED' ||
                    store.selectedConversation?.state === 'CLOSED'
                  "
                  @keydown="handleComposerKeydown"
                />
                <div>
                  <button
                    class="assistant-composer__send"
                    type="button"
                    :aria-label="$t('assistant.send')"
                    :disabled="!canSend || !message.trim()"
                    :title="$t('assistant.send')"
                    @click="send"
                  >
                    <Send :size="19" aria-hidden="true" />
                  </button>
                </div>
              </div>
              <small>{{ $t("assistant.audit") }}</small>
            </footer>
          </div>
        </div>
      </template>
    </aside>
  </div>
</template>

<style scoped>
.assistant-fab {
  position: fixed;
  z-index: 42;
  right: 24px;
  bottom: 24px;
  display: grid;
  width: 54px;
  height: 54px;
  place-items: center;
  border: 0;
  border-radius: 50%;
  background: var(--accent);
  color: #fff;
  box-shadow: 0 10px 28px rgb(24 72 126 / 28%);
  cursor: pointer;
}
.assistant-fab:hover,
.assistant-fab:focus-visible {
  filter: brightness(0.94);
}
.assistant-overlay {
  position: fixed;
  z-index: 70;
  inset: 0;
}
.assistant-overlay__backdrop {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  border: 0;
  background: rgb(17 24 39 / 36%);
}
.assistant-drawer {
  position: fixed;
  inset: 4dvh 4vw;
  display: flex;
  width: 92vw;
  max-width: 92vw;
  height: 92dvh;
  min-width: 0;
  flex-direction: column;
  overflow: hidden;
  background: var(--surface);
  box-shadow: -18px 0 48px rgb(15 23 42 / 20%);
  outline: 0;
}
.assistant-drawer--plan {
  width: 92vw;
}
.assistant-conversation-sidebar {
  display: none;
}
@media (min-width: 1001px) {
  .assistant-drawer:not(.assistant-drawer--plan) {
    padding-left: 280px;
  }
  .assistant-drawer:not(.assistant-drawer--plan) > .assistant-drawer__header {
    margin-left: -280px;
    height: 72px;
  }
  .assistant-conversation-sidebar {
    position: absolute;
    inset: 72px auto 0 0;
    width: 280px;
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding: 16px;
    overflow-y: auto;
    border-right: 1px solid var(--border);
    background: var(--panel);
  }
  .assistant-history {
    display: none;
  }
  .assistant-conversation-entry {
    display: grid;
    gap: 6px;
    padding: 12px;
    border: 0;
    border-radius: 6px;
    text-align: left;
    background: transparent;
    color: var(--text);
    cursor: pointer;
  }
  .assistant-conversation-entry.selected {
    background: var(--accent-soft);
  }
  .assistant-conversation-entry strong {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .assistant-conversation-entry time {
    color: var(--muted);
    font-size: 12px;
  }
}
.assistant-drawer > .assistant-plan-editor,
.assistant-drawer__view,
.assistant-chat-view {
  min-width: 0;
  min-height: 0;
  flex: 1 1 auto;
}
.assistant-drawer__view,
.assistant-chat-view {
  display: flex;
  flex-direction: column;
  overflow: hidden;
}
.assistant-drawer__header {
  position: sticky;
  z-index: 4;
  top: 0;
  display: flex;
  flex: 0 0 auto;
  align-items: center;
  gap: 10px;
  min-height: 64px;
  padding: 10px 14px;
  border-bottom: 1px solid var(--border);
  background: var(--surface);
}
.assistant-drawer__mark {
  display: grid;
  width: 38px;
  height: 38px;
  flex: 0 0 38px;
  place-items: center;
  border-radius: 8px;
  background: var(--accent-soft);
  color: var(--accent);
}
.assistant-drawer__identity {
  display: grid;
  min-width: 0;
  flex: 1;
}
.assistant-drawer__identity span {
  overflow: hidden;
  color: var(--muted);
  font-size: 0.8rem;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.assistant-history {
  position: relative;
  flex: 0 0 auto;
}
.assistant-history > .icon-button {
  width: auto;
  padding-inline: 8px;
}
.assistant-new-conversation {
  display: inline-flex;
  min-height: 38px;
  flex: 0 0 auto;
  align-items: center;
  gap: 7px;
  padding: 0 11px;
  border: 1px solid var(--border);
  border-radius: 7px;
  background: var(--surface);
  color: var(--text);
  cursor: pointer;
}
.assistant-new-conversation:hover:not(:disabled) {
  border-color: var(--accent);
  color: var(--accent);
}
.assistant-new-conversation:disabled {
  cursor: not-allowed;
  opacity: 0.55;
}
.assistant-history__toggle span {
  font-size: 0.82rem;
}
.assistant-history__menu {
  position: absolute;
  z-index: 3;
  top: calc(100% + 8px);
  right: 0;
  width: 420px;
  max-width: calc(100vw - 32px);
  max-height: min(480px, calc(100vh - 120px));
  overflow: auto;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--surface);
  box-shadow: 0 14px 36px rgb(15 23 42 / 18%);
}
.assistant-history__menu header {
  padding: 10px 12px;
  color: var(--muted);
  font-size: 0.78rem;
  font-weight: 600;
  text-transform: uppercase;
}
.assistant-history__menu button {
  display: flex;
  width: 100%;
  min-height: 48px;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  padding: 9px 12px;
  border: 0;
  border-top: 1px solid var(--border);
  background: transparent;
  color: inherit;
  text-align: left;
  cursor: pointer;
}
.assistant-history__menu button:hover,
.assistant-history__menu button.selected {
  background: var(--accent-soft);
}
.assistant-history__menu button > span {
  display: grid;
  min-width: 0;
}
.assistant-history__menu strong {
  display: -webkit-box;
  overflow: hidden;
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 2;
  overflow-wrap: anywhere;
}
.assistant-history__menu small {
  color: var(--subtle);
}
.assistant-drawer__tabs {
  display: flex;
  gap: 4px;
  padding: 8px 14px 0;
  border-bottom: 1px solid var(--border);
}
.assistant-drawer__tabs button {
  display: flex;
  min-height: 38px;
  align-items: center;
  gap: 7px;
  padding: 0 12px;
  border: 0;
  border-bottom: 2px solid transparent;
  background: transparent;
  color: var(--muted);
  cursor: pointer;
}
.assistant-drawer__tabs button.selected {
  border-bottom-color: var(--accent);
  color: var(--accent);
}
.assistant-context-strip {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr);
  gap: 2px 10px;
  padding: 10px 16px;
  border-bottom: 1px solid var(--border);
  background: var(--panel);
}
.assistant-context-strip > span,
.assistant-context-strip > small {
  color: var(--subtle);
  font-size: 0.76rem;
}
.assistant-context-details {
  grid-column: 1 / -1;
  display: flex;
  flex-wrap: wrap;
  gap: 4px 12px;
  margin: 6px 0 0;
  font-size: 0.76rem;
  overflow-wrap: anywhere;
}
.assistant-context-strip > small {
  grid-column: 2;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.assistant-conversation-title {
  display: flex;
  align-items: center;
  gap: 8px;
  min-height: 46px;
  padding: 7px 16px;
  border-bottom: 1px solid var(--border);
}
.assistant-conversation-title > strong {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.assistant-conversation-title form {
  display: grid;
  width: 100%;
  grid-template-columns: minmax(0, 1fr) auto auto;
  gap: 8px;
}
.assistant-chat-log {
  min-height: 0;
  flex: 1 1 auto;
  overflow: auto;
  overscroll-behavior: contain;
  padding: 18px 16px;
}
.assistant-empty-state {
  display: grid;
  min-height: 260px;
  place-items: center;
  align-content: center;
  gap: 8px;
  color: var(--muted);
  text-align: center;
}
.assistant-empty-state h2,
.assistant-empty-state p {
  margin: 0;
}
.assistant-message {
  width: min(86%, 760px);
  margin-bottom: 14px;
  padding: 12px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--surface);
}
.assistant-message--user {
  margin-left: auto;
  border-color: var(--accent);
  background: var(--accent-soft);
}
.assistant-message--system_receipt {
  width: 100%;
  background: var(--panel);
}
.assistant-message > header,
.assistant-plan-card > header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}
.assistant-plan-card {
  display: grid;
  gap: 10px;
  margin-top: 10px;
  padding: 12px;
  border: 1px solid var(--accent);
  border-radius: 8px;
  background: var(--surface);
}
.assistant-plan-card > header {
  justify-content: flex-start;
}
.assistant-plan-card > header > div {
  display: grid;
  min-width: 0;
  flex: 1;
}
.assistant-plan-card > header span {
  color: var(--subtle);
  font-size: 0.76rem;
}
.assistant-plan-card__operations {
  display: grid;
  gap: 8px;
  margin: 0;
  padding: 0;
  color: var(--text);
  font-size: 0.84rem;
  list-style: none;
}
.assistant-plan-card__operations li {
  display: grid;
  gap: 6px;
  padding: 8px 10px;
  border-left: 3px solid var(--accent);
  background: var(--panel);
}
.assistant-plan-card__operations li > header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}
.assistant-plan-card__operations .assistant-plan-card__action {
  color: var(--accent);
  font-size: 0.75rem;
  font-weight: 700;
  text-transform: uppercase;
}
.assistant-plan-card__target {
  overflow-wrap: anywhere;
}
.assistant-plan-card__operations span,
.assistant-plan-card__operations small {
  color: var(--muted);
}
.assistant-plan-card__operations p {
  margin: 0;
}
.assistant-plan-card__parameters {
  display: grid;
  gap: 6px;
  margin-top: 2px;
  padding-top: 8px;
  border-top: 1px solid var(--border);
}
.assistant-plan-card__parameters > strong {
  font-size: 0.78rem;
}
.assistant-plan-card .button {
  justify-self: start;
}
.assistant-composer {
  position: sticky;
  z-index: 2;
  bottom: 0;
  display: grid;
  flex: 0 0 auto;
  gap: 6px;
  padding: 12px 16px 14px;
  border-top: 1px solid var(--border);
  background: var(--surface);
}
.assistant-composer__field {
  position: relative;
}
.assistant-composer :deep(textarea) {
  width: 100%;
  min-height: 72px;
  max-height: 180px;
  resize: vertical;
  padding: 11px 104px 11px 12px;
  border: 1px solid var(--border-strong);
  border-radius: 10px;
}
.assistant-composer :deep(.voice-textarea__action) {
  right: 58px;
}
.assistant-composer__field > div {
  position: absolute;
  right: 8px;
  bottom: 8px;
  display: flex;
  gap: 6px;
}
.assistant-composer__icon,
.assistant-composer__send {
  display: grid;
  width: 40px;
  height: 40px;
  place-items: center;
  border: 0;
  border-radius: 50%;
}
.assistant-composer__icon {
  color: var(--subtle);
}
.assistant-composer__send {
  background: var(--accent);
  color: #fff;
  cursor: pointer;
}
.assistant-composer__send:disabled {
  background: var(--panel);
  color: var(--subtle);
  cursor: not-allowed;
}
.assistant-composer > small {
  color: var(--subtle);
}
@media (max-width: 720px) {
  .assistant-fab {
    right: 16px;
    bottom: calc(76px + env(safe-area-inset-bottom));
  }
  .assistant-drawer {
    left: 0;
    top: auto;
    right: 0;
    bottom: 0;
    width: 100%;
    max-width: none;
    height: 100dvh;
    max-height: 100dvh;
    border: 1px solid var(--border);
    border-bottom: 0;
    border-radius: 0;
    box-shadow: 0 -16px 40px rgb(15 23 42 / 22%);
  }
  .assistant-drawer::before {
    display: none;
    position: absolute;
    top: 6px;
    left: 50%;
    width: 42px;
    height: 4px;
    border-radius: 2px;
    background: var(--border-strong);
    content: "";
    transform: translateX(-50%);
  }
  .assistant-drawer__header {
    gap: 8px;
    padding-top: 14px;
  }
  .assistant-new-conversation,
  .assistant-history__toggle {
    width: 40px;
    height: 40px;
    min-height: 40px;
    justify-content: center;
    padding: 0;
  }
  .assistant-new-conversation span,
  .assistant-history__toggle span,
  .assistant-history__toggle svg:last-child,
  .assistant-drawer__header > :deep(.status-badge) {
    display: none;
  }
  .assistant-history__menu {
    position: fixed;
    inset: auto 8px 8px;
    width: auto;
    max-height: 60vh;
  }
  .assistant-message {
    width: 94%;
  }
  .assistant-composer {
    padding-bottom: max(14px, env(safe-area-inset-bottom));
  }
}
@media (min-width: 721px) and (max-width: 900px) {
  .assistant-drawer {
    width: min(640px, calc(100vw - 24px));
    max-width: calc(100vw - 24px);
  }
  .assistant-drawer--plan {
    width: calc(100vw - 24px);
  }
  .assistant-new-conversation,
  .assistant-history__toggle {
    width: 40px;
    height: 40px;
    min-height: 40px;
    justify-content: center;
    padding: 0;
  }
  .assistant-new-conversation span,
  .assistant-history__toggle span,
  .assistant-history__toggle svg:last-child,
  .assistant-drawer__header > :deep(.status-badge) {
    display: none;
  }
}
</style>
