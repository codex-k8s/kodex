<script setup lang="ts">
import {
  Activity,
  Bot,
  Check,
  ChevronDown,
  History,
  ListChecks,
  Mic,
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

import AssistantPlanEditor from "@/features/assistant/components/AssistantPlanEditor.vue";
import { openAssistantEvent } from "@/features/assistant/events";
import { useAssistantStore } from "@/features/assistant/store";
import RunActivityView from "@/features/runs/RunActivityView.vue";
import type {
  AssistantContextDescriptor,
  AssistantPlan,
  RunEvent,
} from "@/shared/api/generated/openapi/types.gen";
import {
  focusableElements,
  trappedFocusTarget,
} from "@/shared/ui/dialog-focus";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import SafeMarkdown from "@/shared/ui/SafeMarkdown.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

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
const store = useAssistantStore();
const open = ref(false);
const historyOpen = ref(false);
const message = ref("");
const titleDraft = ref("");
const titleEditing = ref(false);
const openPlanRef = ref<string>();
const activeView = ref<"CHAT" | "ACTIVITY">("CHAT");
const panel = ref<HTMLElement>();
const composer = ref<HTMLTextAreaElement>();
const historyMenu = ref<HTMLElement>();
const fab = ref<HTMLButtonElement>();

const contextTitle = computed(
  () => props.context.entityName || props.context.route || "Kodex",
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
const canSend = computed(
  () =>
    props.live &&
    !store.busy &&
    Boolean(store.assistant?.nextActions.includes("ADD_TURN")) &&
    Boolean(
      store.selectedConversation ||
      store.assistant?.nextActions.includes("CREATE_CONVERSATION"),
    ),
);
const isRunContext = computed(() => props.context.entityKind === "RUN");

function contextKey(): string {
  return [
    props.projectRef ?? "",
    props.context.route,
    props.context.entityKind,
    props.context.entityRef,
    props.context.entityVersion ?? "",
  ].join(":");
}

function handleOpenAssistant(): void {
  void show();
}

async function show(): Promise<void> {
  open.value = true;
  historyOpen.value = false;
  openPlanRef.value = undefined;
  activeView.value = "CHAT";
  await store.load(props.context, props.projectRef);
  await nextTick();
  panel.value?.focus();
}

function close(): void {
  if (store.busy) return;
  open.value = false;
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
}

async function startConversation(): Promise<void> {
  historyOpen.value = false;
  titleEditing.value = false;
  openPlanRef.value = undefined;
  await store.startConversation();
}

function startTitleEdit(): void {
  if (!store.selectedConversation) return;
  titleDraft.value = store.selectedConversation.title;
  titleEditing.value = true;
}

async function saveTitle(): Promise<void> {
  await store.changeTitle(titleDraft.value);
  titleEditing.value = false;
}

async function send(): Promise<void> {
  const value = message.value.trim();
  if (!value || !canSend.value) return;
  await store.send(value);
  message.value = "";
  await nextTick();
  composer.value?.focus();
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
  if (!currentPlan.value) return;
  await store.saveDraft(currentPlan.value, summary, operations);
}

async function validatePlan(): Promise<void> {
  if (currentPlan.value) await store.validate(currentPlan.value);
}

async function applyPlan(): Promise<void> {
  if (currentPlan.value) await store.apply(currentPlan.value);
}

async function rejectPlan(): Promise<void> {
  if (currentPlan.value) await store.reject(currentPlan.value);
}

function documentPointerDown(event: PointerEvent): void {
  if (
    historyOpen.value &&
    historyMenu.value &&
    !historyMenu.value.contains(event.target as Node)
  )
    historyOpen.value = false;
}

watch(contextKey, () => {
  store.setContext(props.context, props.projectRef);
  openPlanRef.value = undefined;
  activeView.value = "CHAT";
  if (open.value) void store.load(props.context, props.projectRef);
});
watch(
  () => props.refreshRevision,
  (value, previous) => {
    if (open.value && value !== previous)
      void store.load(props.context, props.projectRef);
  },
);
watch(
  () => store.selectedConversation?.ref,
  () => {
    titleEditing.value = false;
    openPlanRef.value = undefined;
    store.clearReceipt();
  },
);

onMounted(() => {
  document.addEventListener("pointerdown", documentPointerDown);
  window.addEventListener(openAssistantEvent, handleOpenAssistant);
});
onBeforeUnmount(() => {
  document.removeEventListener("pointerdown", documentPointerDown);
  window.removeEventListener(openAssistantEvent, handleOpenAssistant);
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
      role="dialog"
      aria-modal="true"
      :aria-label="$t('assistant.title')"
      tabindex="-1"
      @keydown="handleKeydown"
    >
      <AssistantPlanEditor
        v-if="currentPlan"
        :plan="currentPlan"
        :receipt="store.receipt"
        :busy="store.busy"
        :problem="store.problem"
        @close="openPlanRef = undefined"
        @save="savePlan"
        @validate="validatePlan"
        @apply="applyPlan"
        @reject="rejectPlan"
      />
      <template v-else>
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
            :state="store.assistant.runtimeState"
          />
          <div ref="historyMenu" class="assistant-history">
            <button
              class="icon-button"
              type="button"
              :aria-label="$t('assistant.history')"
              :aria-expanded="historyOpen"
              @click="historyOpen = !historyOpen"
            >
              <History :size="18" aria-hidden="true" />
              <ChevronDown :size="14" aria-hidden="true" />
            </button>
            <section
              v-if="historyOpen"
              class="assistant-history__menu"
              :aria-label="$t('assistant.history')"
            >
              <header>{{ $t("assistant.history") }}</header>
              <button
                type="button"
                :disabled="store.busy"
                @click="startConversation"
              >
                <Plus :size="17" aria-hidden="true" />
                <span>{{ $t("assistant.newConversation") }}</span>
              </button>
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
                  <strong>{{ conversation.title }}</strong>
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

        <RunActivityView v-if="activeView === 'ACTIVITY'" :events="runEvents" />
        <template v-else>
          <section class="assistant-context-strip">
            <span>{{ $t("assistant.context") }}</span>
            <strong>{{ contextTitle }}</strong>
            <small>{{ context.route }}</small>
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
              <strong>{{ store.selectedConversation.title }}</strong>
              <button
                class="icon-button"
                type="button"
                :aria-label="$t('assistant.renameConversation')"
                @click="startTitleEdit"
              >
                <Pencil :size="16" aria-hidden="true" />
              </button>
            </template>
          </section>

          <section class="assistant-chat-log" role="log" aria-live="polite">
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
                <ul class="assistant-plan-card__operations">
                  <li
                    v-for="operation in turn.plan.operations.slice(0, 3)"
                    :key="operation.ref"
                  >
                    {{ operation.title }}
                  </li>
                  <li v-if="turn.plan.operations.length > 3">
                    +{{ turn.plan.operations.length - 3 }}
                  </li>
                </ul>
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
            <div class="assistant-composer__field">
              <textarea
                ref="composer"
                v-model="message"
                rows="2"
                maxlength="32768"
                :aria-label="$t('assistant.message')"
                :placeholder="$t('assistant.message')"
                :disabled="store.busy || !live"
                @keydown="handleComposerKeydown"
              />
              <div>
                <button
                  class="assistant-composer__icon"
                  type="button"
                  disabled
                  :aria-label="$t('assistant.microphoneUnavailable')"
                  :title="$t('assistant.microphoneUnavailable')"
                >
                  <Mic :size="18" aria-hidden="true" />
                </button>
                <button
                  class="assistant-composer__send"
                  type="button"
                  :aria-label="$t('assistant.send')"
                  :disabled="!canSend || !message.trim()"
                  @click="send"
                >
                  <Send :size="19" aria-hidden="true" />
                </button>
              </div>
            </div>
            <small>{{ $t("assistant.audit") }}</small>
          </footer>
        </template>
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
  position: absolute;
  inset: 0 0 0 auto;
  display: grid;
  grid-template-rows: auto auto auto minmax(0, 1fr) auto;
  width: min(760px, calc(100vw - 64px));
  min-width: 0;
  background: var(--surface);
  box-shadow: -18px 0 48px rgb(15 23 42 / 20%);
  outline: 0;
}
.assistant-drawer__header {
  display: flex;
  align-items: center;
  gap: 10px;
  min-height: 64px;
  padding: 10px 14px;
  border-bottom: 1px solid var(--border);
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
}
.assistant-history > .icon-button {
  width: auto;
  padding-inline: 8px;
}
.assistant-history__menu {
  position: absolute;
  z-index: 3;
  top: calc(100% + 8px);
  right: 0;
  width: 360px;
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
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
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
  overflow: auto;
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
  width: min(86%, 620px);
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
  gap: 4px;
  margin: 0;
  padding-left: 20px;
  color: var(--text);
  font-size: 0.84rem;
}
.assistant-plan-card .button {
  justify-self: start;
}
.assistant-composer {
  display: grid;
  gap: 6px;
  padding: 12px 16px 14px;
  border-top: 1px solid var(--border);
  background: var(--surface);
}
.assistant-composer__field {
  position: relative;
}
.assistant-composer textarea {
  width: 100%;
  min-height: 72px;
  max-height: 180px;
  resize: vertical;
  padding: 11px 104px 11px 12px;
  border: 1px solid var(--border-strong);
  border-radius: 10px;
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
    inset: auto 0 0;
    width: 100%;
    height: min(88vh, 900px);
    border-radius: 14px 14px 0 0;
    box-shadow: 0 -16px 40px rgb(15 23 42 / 22%);
  }
  .assistant-drawer::before {
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
    padding-top: 14px;
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
}
</style>
