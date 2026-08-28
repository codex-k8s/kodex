<script setup lang="ts">
import {
  Bot,
  Check,
  ChevronDown,
  ListChecks,
  Mic,
  Pencil,
  Send,
} from "@lucide/vue";
import { computed, nextTick, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

import { usePlatformStore } from "@/features/platform/store";
import { useRealtimeStore } from "@/features/realtime/store";
import type {
  AssistantConversation,
  AssistantPlan,
} from "@/shared/api/generated/openapi";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import OverlayPanel from "@/shared/ui/OverlayPanel.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import SafeMarkdown from "@/shared/ui/SafeMarkdown.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";
import { useDismissibleLayer } from "@/shared/ui/useDismissibleLayer";

const props = defineProps<{
  open: boolean;
  screenTitle: string;
  contextSummary: string;
  projectRef?: string;
}>();
const emit = defineEmits<{ close: [] }>();
const platform = usePlatformStore();
const realtime = useRealtimeStore();
const { t } = useI18n();
const log = ref<HTMLElement>();
const input = ref<HTMLTextAreaElement>();
const selectedRef = ref<string>();
const message = ref("");
const busy = ref(false);
const problem = ref<AppProblem>();
const conversationMenuOpen = ref(false);
const conversationPicker = ref<HTMLElement>();
const rejectedPlans = ref<ReadonlySet<string>>(new Set());

const conversations = computed(() =>
  Object.values(platform.conversations)
    .filter((item) => !props.projectRef || item.projectRef === props.projectRef)
    .sort((left, right) => right.updatedAt.localeCompare(left.updatedAt)),
);
const conversation = computed<AssistantConversation | undefined>(() =>
  selectedRef.value ? platform.conversations[selectedRef.value] : undefined,
);
const canCreate = computed(() =>
  platform.assistant?.nextActions.includes("CREATE_CONVERSATION"),
);
const canSend = computed(
  () =>
    platform.assistant?.nextActions.includes("ADD_TURN") === true &&
    realtime.platformState.state === "live",
);

function close(): void {
  conversationMenuOpen.value = false;
  emit("close");
}

function chooseConversation(ref?: string): void {
  selectedRef.value = ref;
  conversationMenuOpen.value = false;
}

function planState(plan: AssistantPlan): string {
  if (plan.applied) return "APPLIED";
  if (rejectedPlans.value.has(plan.ref)) return "REJECTED";
  return plan.nextActions.includes("APPLY_PLAN")
    ? "WAITING_HUMAN"
    : "UNAVAILABLE";
}

function requestChanges(): void {
  message.value = t("common.requestChanges");
  void nextTick(() => input.value?.focus());
}

async function ensureConversation(): Promise<string | undefined> {
  if (conversation.value) return conversation.value.ref;
  if (!canCreate.value) return undefined;
  const created = await platform.newConversation(
    t("assistant.contextConversation", { context: props.screenTitle }),
    props.projectRef,
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
    const isFirstTurn = (platform.conversations[ref]?.turns.length ?? 0) === 0;
    const contextualContent = isFirstTurn
      ? `${t("assistant.contextPrefix", { context: props.contextSummary })}\n\n${content}`
      : content;
    const updated = await platform.sendAssistantTurn(ref, contextualContent);
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

async function apply(plan: AssistantPlan): Promise<void> {
  busy.value = true;
  problem.value = undefined;
  try {
    await platform.applyPlan(plan.ref, plan.version);
  } catch (error) {
    problem.value = asProblem(error);
  } finally {
    busy.value = false;
  }
}

watch(
  () => props.open,
  async (value) => {
    if (!value) return;
    await platform.loadAssistant();
    selectedRef.value = conversations.value[0]?.ref;
    await nextTick();
    input.value?.focus();
  },
);
watch(
  () => props.contextSummary,
  () => {
    selectedRef.value = undefined;
    conversationMenuOpen.value = false;
  },
);
onMounted(() => {
  if (props.open) void platform.loadAssistant();
});
useDismissibleLayer(conversationPicker, () => {
  conversationMenuOpen.value = false;
});
</script>

<template>
  <OverlayPanel
    :open="open"
    mode="responsive"
    :ariaLabel="$t('assistant.title')"
    :close-label="$t('common.close')"
    :busy="busy"
    @update:open="(value) => !value && close()"
  >
    <template #header>
      <div class="kodex-drawer__header">
        <span class="kodex-drawer__mark" aria-hidden="true">
          <Bot :size="20" />
        </span>
        <div>
          <strong>Kodex</strong>
          <small>{{ screenTitle }}</small>
        </div>
        <StatusBadge
          v-if="platform.assistant"
          :state="platform.assistant.runtimeState"
        />
      </div>
    </template>

    <div class="kodex-drawer">
      <div class="kodex-drawer__context">
        <span>{{ $t("assistant.context") }}</span>
        <strong>{{ contextSummary }}</strong>
      </div>

      <div ref="conversationPicker" class="kodex-conversation-picker">
        <button
          type="button"
          :aria-expanded="conversationMenuOpen"
          aria-haspopup="listbox"
          @click="conversationMenuOpen = !conversationMenuOpen"
        >
          <span>{{
            conversation?.title ?? $t("assistant.newConversation")
          }}</span>
          <ChevronDown :size="16" aria-hidden="true" />
        </button>
        <div
          v-if="conversationMenuOpen"
          class="kodex-conversation-picker__menu"
          role="listbox"
        >
          <button type="button" role="option" @click="chooseConversation()">
            <span>{{ $t("assistant.newConversation") }}</span>
          </button>
          <button
            v-for="item in conversations"
            :key="item.ref"
            type="button"
            role="option"
            :aria-selected="item.ref === conversation?.ref"
            @click="chooseConversation(item.ref)"
          >
            <span>
              <strong>{{ item.title }}</strong>
              <small>{{ new Date(item.updatedAt).toLocaleString() }}</small>
            </span>
            <Check
              v-if="item.ref === conversation?.ref"
              :size="15"
              aria-hidden="true"
            />
          </button>
        </div>
      </div>

      <div ref="log" class="kodex-drawer__log" role="log" aria-live="polite">
        <div v-if="!conversation?.turns.length" class="kodex-drawer__empty">
          <Bot :size="28" aria-hidden="true" />
          <strong>{{ $t("assistant.contextReady") }}</strong>
          <p>{{ $t("assistant.contextHelp") }}</p>
        </div>
        <article
          v-for="turn in conversation?.turns ?? []"
          :key="turn.ref"
          class="kodex-turn"
          :class="`kodex-turn--${turn.role.toLowerCase()}`"
        >
          <header>
            <strong>{{
              turn.role === "USER" ? $t("common.input") : "Kodex"
            }}</strong>
            <StatusBadge :state="turn.state" />
          </header>
          <SafeMarkdown v-if="!turn.plan" :content="turn.content" />
          <section v-else class="kodex-plan">
            <header>
              <ListChecks :size="18" aria-hidden="true" />
              <div>
                <strong>{{ $t("assistant.plan") }}</strong>
                <SafeMarkdown :content="turn.plan.auditSummary" />
              </div>
              <StatusBadge :state="planState(turn.plan)" />
            </header>
            <ul>
              <li
                v-for="operation in turn.plan.operations"
                :key="operation.ref"
              >
                <div>
                  <strong>{{ operation.title }}</strong>
                  <SafeMarkdown :content="operation.summary" />
                  <SafeMarkdown
                    v-if="operation.unavailableReason"
                    class="kodex-plan__problem"
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
            >
              <button
                class="button button--primary"
                type="button"
                :disabled="busy || realtime.platformState.state !== 'live'"
                @click="apply(turn.plan!)"
              >
                <Check :size="16" aria-hidden="true" />
                {{ $t("assistant.applyPlan") }}
              </button>
              <button class="button" type="button" @click="requestChanges">
                <Pencil :size="16" aria-hidden="true" />
                {{ $t("common.requestChanges") }}
              </button>
            </footer>
          </section>
        </article>
      </div>

      <ProblemNotice v-if="problem" :problem="problem" compact />
      <form class="kodex-composer" @submit.prevent="send">
        <textarea
          ref="input"
          v-model="message"
          :placeholder="$t('assistant.message')"
          maxlength="8000"
          :disabled="!canSend"
          @keydown.ctrl.enter="send"
        />
        <button
          class="kodex-composer__mic"
          type="button"
          disabled
          :aria-label="$t('assistant.voiceUnavailable')"
          :title="$t('assistant.voiceUnavailable')"
        >
          <Mic :size="18" aria-hidden="true" />
        </button>
        <button
          class="kodex-composer__send"
          type="submit"
          :disabled="busy || !canSend || !message.trim()"
          :aria-label="$t('assistant.send')"
        >
          <Send :size="18" aria-hidden="true" />
        </button>
      </form>
    </div>
  </OverlayPanel>
</template>

<style scoped>
.kodex-drawer {
  display: grid;
  grid-template-rows: auto auto minmax(0, 1fr) auto auto;
  width: 100%;
  min-height: 0;
  height: 100%;
}
.kodex-drawer__header {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr) auto;
  align-items: center;
  gap: 10px;
}
.kodex-drawer__header > div {
  display: grid;
  min-width: 0;
}
.kodex-drawer__header small,
.kodex-conversation-picker small {
  overflow: hidden;
  color: var(--muted);
  text-overflow: ellipsis;
  white-space: nowrap;
}
.kodex-drawer__mark {
  display: inline-grid;
  width: 34px;
  height: 34px;
  place-items: center;
  color: var(--accent-strong);
  border-radius: 8px;
  background: var(--accent-soft);
}
.kodex-drawer__context {
  display: grid;
  gap: 2px;
  padding: 10px 16px;
  border-bottom: 1px solid var(--border);
  background: var(--panel);
  font-size: 0.84rem;
}
.kodex-drawer__context span {
  color: var(--muted);
}
.kodex-conversation-picker {
  position: relative;
  padding: 10px 16px 0;
}
.kodex-conversation-picker > button {
  display: flex;
  align-items: center;
  justify-content: space-between;
  width: 100%;
  min-height: 40px;
  gap: 10px;
  padding: 7px 10px;
  border: 1px solid var(--border);
  border-radius: 7px;
  color: var(--text);
  background: var(--surface);
  text-align: left;
  cursor: pointer;
}
.kodex-conversation-picker__menu {
  position: absolute;
  z-index: 4;
  top: calc(100% + 5px);
  right: 16px;
  left: 16px;
  max-height: 280px;
  overflow-y: auto;
  padding: 5px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--surface);
  box-shadow: 0 14px 38px rgb(16 22 30 / 0.16);
}
.kodex-conversation-picker__menu button {
  display: flex;
  align-items: center;
  justify-content: space-between;
  width: 100%;
  min-height: 44px;
  gap: 10px;
  padding: 8px;
  border: 0;
  border-radius: 6px;
  color: inherit;
  background: transparent;
  text-align: left;
  cursor: pointer;
}
.kodex-conversation-picker__menu button:hover,
.kodex-conversation-picker__menu button[aria-selected="true"] {
  background: var(--accent-soft);
}
.kodex-conversation-picker__menu button > span {
  display: grid;
  min-width: 0;
}
.kodex-drawer__log {
  overflow-y: auto;
  padding: 16px;
}
.kodex-drawer__empty {
  display: grid;
  justify-items: center;
  max-width: 380px;
  gap: 8px;
  margin: 72px auto;
  color: var(--muted);
  text-align: center;
}
.kodex-drawer__empty p {
  margin: 0;
}
.kodex-turn {
  max-width: 92%;
  margin-bottom: 12px;
  padding: 11px 12px;
  border: 1px solid var(--border);
  border-radius: 9px;
}
.kodex-turn--user {
  margin-left: auto;
  background: var(--accent-soft);
}
.kodex-turn > header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  margin-bottom: 7px;
}
.kodex-plan {
  overflow: hidden;
  border: 1px solid var(--border);
  border-radius: 7px;
  background: var(--surface);
}
.kodex-plan > header {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr) auto;
  align-items: start;
  gap: 8px;
  padding: 10px;
  border-bottom: 1px solid var(--border);
}
.kodex-plan ul {
  margin: 0;
  padding: 0;
  list-style: none;
}
.kodex-plan li {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 8px;
  padding: 10px;
  border-bottom: 1px solid var(--border);
}
.kodex-plan__problem {
  color: var(--danger);
}
.kodex-plan footer {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  padding: 10px;
  background: var(--panel);
}
.kodex-composer {
  position: relative;
  padding: 12px 66px 12px 50px;
  border-top: 1px solid var(--border);
}
.kodex-composer textarea {
  width: 100%;
  min-height: 62px;
  max-height: 160px;
  padding-right: 8px;
  resize: vertical;
}
.kodex-composer__mic,
.kodex-composer__send {
  position: absolute;
  bottom: 20px;
  display: inline-grid;
  width: 36px;
  height: 36px;
  place-items: center;
  border-radius: 50%;
}
.kodex-composer__mic {
  left: 8px;
  color: var(--muted);
  border: 1px solid var(--border);
  background: var(--panel);
}
.kodex-composer__send {
  right: 18px;
  color: #fff;
  border: 0;
  background: var(--accent);
}
.kodex-composer__send:disabled {
  opacity: 0.5;
}
@media (max-width: 680px) {
  .kodex-drawer {
    min-height: min(72vh, 680px);
  }
  .kodex-turn {
    max-width: 100%;
  }
}
:deep(.overlay-panel--responsive) {
  width: min(620px, 100%);
}
:deep(.overlay-panel__body) {
  display: flex;
  padding: 0;
}
</style>
