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
import { useRoute, useRouter } from "vue-router";

import AssistantPlanEditor from "@/features/assistant/components/AssistantPlanEditor.vue";
import AssistantCreatedScheduleCard from "@/features/assistant/components/AssistantCreatedScheduleCard.vue";
import AssistantCreatedEntityCard from "@/features/assistant/components/AssistantCreatedEntityCard.vue";
import AssistantAgentEnvironmentBindingCard from "@/features/assistant/components/AssistantAgentEnvironmentBindingCard.vue";
import AssistantCreatedWorkflowCard from "@/features/assistant/components/AssistantCreatedWorkflowCard.vue";
import AssistantEnvironmentDraftCard from "@/features/assistant/components/AssistantEnvironmentDraftCard.vue";
import AssistantIntegrationConnectionCard from "@/features/assistant/components/AssistantIntegrationConnectionCard.vue";
import AssistantLaunchedRunCard from "@/features/assistant/components/AssistantLaunchedRunCard.vue";
import AssistantRoleImageBuildCard from "@/features/assistant/components/AssistantRoleImageBuildCard.vue";
import { OpenAPIImportDialog } from "@/features/managed-configurations";
import AssistantHistoryFilter from "./AssistantHistoryFilter.vue";
import {
  assistantContextIdentity,
  assistantContextTitle,
  conversationMatchesContext,
  readableContextOperations,
  readableContextKind,
} from "@/features/assistant/context";
import {
  openAssistantEvent,
  type AssistantIntegrationPublicationRequest,
} from "@/features/assistant/events";
import {
  assistantAwaitingReply,
  assistantEffectiveRuntimeState,
  assistantRequiresProviderAccount,
  operationActionLabel,
  operationTargetLabel,
} from "@/features/assistant/model";
import { useAssistantStore } from "@/features/assistant/store";
import { usePlatformStore } from "@/features/platform/store";
import {
  persistAssistantWorkspaceOpen,
  restoreAssistantWorkspaceOpen,
} from "@/features/assistant/workspace-state";
import RunActivityView from "@/features/runs/RunActivityView.vue";
import RuntimeSecretDraftDialog from "@/features/runtime-secrets/RuntimeSecretDraftDialog.vue";
import type { RuntimeSecretDraftSuggestion } from "@/features/runtime-secrets/model";
import type {
  AssistantContextDescriptor,
  AssistantPlan,
  AssistantPlanReceipt,
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
import OverlayPanel from "@/shared/ui/OverlayPanel.vue";
import { useCursorInfiniteScroll } from "@/shared/ui/async-entity-picker";
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
const route = useRoute();
const router = useRouter();
const assistantFormActive = computed(() => route.query.assistantForm === "1");
const store = useAssistantStore();
const platform = usePlatformStore();
const open = ref(
  restoreAssistantWorkspaceOpen() || route.query.assistantForm === "1",
);
const historyOpen = ref(false);
const contextOpen = ref(false);
const integrationImportOpen = ref(false);
const secretDialogOpen = ref(false);
const secretInitialDraftRef = ref<string>();
const secretSuggestion = ref<RuntimeSecretDraftSuggestion>();
const createdDefinitionRef = ref<string>();
const desktopHistory = ref<HTMLElement>();
const desktopHistorySentinel = ref<HTMLElement>();
const mobileHistory = ref<HTMLElement>();
const mobileHistorySentinel = ref<HTMLElement>();
const desktopHistoryVisible = ref(false);
const historyMedia =
  typeof window === "undefined"
    ? undefined
    : window.matchMedia("(min-width: 1001px)");
function syncHistoryViewport(): void {
  desktopHistoryVisible.value = historyMedia?.matches ?? false;
}
syncHistoryViewport();
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
const formSlot = ref<HTMLElement>();
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
const contextTitle = computed(() =>
  assistantContextTitle(props.context, checkedContext.value),
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
const awaitingReply = computed(() =>
  assistantAwaitingReply(store.selectedConversation),
);
const providerAccountRequired = computed(
  () =>
    store.assistant !== undefined &&
    assistantRequiresProviderAccount(store.assistant),
);
const setupSuggestions = computed(() =>
  (props.projectRef
    ? ["agent", "environment", "integration", "launch"]
    : ["project"]
  ).map((step) => ({
    step,
    title: t(`assistant.setup.${step}.title`),
    prompt: t(`assistant.setup.${step}.prompt`),
  })),
);
const assistantReadinessLabel = computed(() =>
  providerAccountRequired.value
    ? t("assistant.providerAccountRequired")
    : store.assistant?.readinessSummary,
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
for (const [root, sentinel, visible] of [
  [desktopHistory, desktopHistorySentinel, () => desktopHistoryVisible.value],
  [
    mobileHistory,
    mobileHistorySentinel,
    () => !desktopHistoryVisible.value && historyOpen.value,
  ],
] as const) {
  useCursorInfiniteScroll({
    root,
    sentinel,
    enabled: () =>
      open.value &&
      !currentPlan.value &&
      visible() &&
      Boolean(store.nextPageToken) &&
      !store.loading &&
      !store.loadingMore &&
      !store.busy &&
      !store.historyProblem,
    loadMore: () => store.loadMoreHistory(),
  });
}

const contextIdentity = computed(() =>
  assistantContextIdentity(props.context, props.projectRef),
);

function handleOpenAssistant(event: Event): void {
  void (async () => {
    const request =
      event instanceof CustomEvent
        ? (event.detail as AssistantIntegrationPublicationRequest | undefined)
        : undefined;
    await show();
    if (
      !request ||
      !/^mcfg_[A-Za-z0-9_-]{1,91}$/.test(request.configurationRef) ||
      !/^mrev_[A-Za-z0-9_-]{1,91}$/.test(request.revisionRef)
    )
      return;
    if (
      message.value.trim() &&
      !window.confirm(t("assistant.replaceDraftConfirm"))
    )
      return;
    message.value = t("assistant.publishIntegrationRequest", {
      configurationRef: request.configurationRef,
      revisionRef: request.revisionRef,
    });
    await nextTick();
    composer.value?.focus();
  })();
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
  if (secretDialogOpen.value) return;
  if (assistantFormActive.value) {
    void closeAssistantForm();
    return;
  }
  if (
    (message.value.trim() ||
      attachmentState.value.count > 0 ||
      attachmentState.value.busy) &&
    !window.confirm(t("assistant.closeWithDraftConfirm"))
  )
    return;
  store.cancelReads();
  integrationImportOpen.value = false;
  createdDefinitionRef.value = undefined;
  open.value = false;
  persistAssistantWorkspaceOpen(false);
  historyOpen.value = false;
  contextOpen.value = false;
  openPlanRef.value = undefined;
  store.clearReceipt();
  void nextTick(() => fab.value?.focus());
}

async function closeAssistantForm(): Promise<void> {
  if (store.busy) return;
  const origin = store.context?.route;
  if (origin?.startsWith("/") && !origin.startsWith("//")) {
    const resolved = router.resolve(origin);
    if (
      resolved.params.projectRef === props.projectRef &&
      resolved.query.assistantForm !== "1"
    ) {
      await router.replace(origin);
      return;
    }
  }
  await router.replace({ query: { ...route.query, assistantForm: undefined } });
}

async function resumeAssistantSecretForm(): Promise<void> {
  if (!props.projectRef || route.params.projectRef !== props.projectRef) return;
  const creating = route.query.assistantCreateSecret === "1";
  const draftRef = route.query.assistantSecretDraftRef;
  const resuming =
    typeof draftRef === "string" && /^[-_A-Za-z0-9]{8,128}$/.test(draftRef);
  if (!creating && !resuming) return;
  await show();
  secretInitialDraftRef.value = resuming ? draftRef : undefined;
  secretSuggestion.value = undefined;
  secretDialogOpen.value = true;
  await router.replace({
    query: {
      ...route.query,
      assistantCreateSecret: undefined,
      assistantSecretDraftRef: undefined,
    },
  });
}

function openPlainSecretForm(): void {
  secretInitialDraftRef.value = undefined;
  secretSuggestion.value = undefined;
  secretDialogOpen.value = true;
}

function openSuggestedSecretForm(
  suggestion: RuntimeSecretDraftSuggestion,
): void {
  if (!props.projectRef || currentPlan.value?.projectRef !== props.projectRef)
    return;
  secretInitialDraftRef.value = undefined;
  secretSuggestion.value = suggestion;
  secretDialogOpen.value = true;
}

function handleKeydown(event: KeyboardEvent): void {
  if ((event.target as HTMLElement).closest(".modal")) return;
  if (event.key === "Escape") {
    if (assistantFormActive.value) {
      void closeAssistantForm();
      return;
    }
    if (historyOpen.value) historyOpen.value = false;
    else if (openPlanRef.value) void closePlan();
    else close();
    return;
  }
  if (event.key !== "Tab" || !panel.value) return;
  const target = trappedFocusTarget(
    [
      ...focusableElements(panel.value),
      ...(formSlot.value ? focusableElements(formSlot.value) : []),
    ],
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

function suggestSetup(prompt: string): void {
  if (!canSend.value || message.value.trim()) return;
  message.value = prompt;
  void nextTick(() => composer.value?.focus());
}

function handleAssistantLink(event: MouseEvent): void {
  if (
    event.button !== 0 ||
    event.altKey ||
    event.ctrlKey ||
    event.metaKey ||
    event.shiftKey ||
    !(event.target instanceof Element)
  )
    return;
  const link = event.target.closest("a[href]");
  if (link?.getAttribute("href") !== "/configurations/INTEGRATION_DEFINITION")
    return;
  event.preventDefault();
  integrationImportOpen.value = true;
}

function integrationDraftCreated(configurationRef: string): void {
  integrationImportOpen.value = false;
  createdDefinitionRef.value = configurationRef;
}

function openCreatedDefinition(): void {
  if (!createdDefinitionRef.value) return;
  const configurationRef = createdDefinitionRef.value;
  close();
  if (!open.value)
    void router.push({
      name: "configuration",
      params: { kind: "INTEGRATION_DEFINITION", configurationRef },
    });
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

async function closePlan(): Promise<void> {
  if (store.busy) return;
  const refresh = ["APPLIED", "REJECTED"].includes(
    currentPlan.value?.state ?? "",
  );
  openPlanRef.value = undefined;
  store.clearReceipt();
  await nextTick();
  scrollToLatest();
  if (refresh && open.value) await store.load(props.context, props.projectRef);
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
  if (!plan) return;
  let receipt: AssistantPlanReceipt | undefined;
  if (
    !(await handleStoreMutation(async () => {
      receipt = await store.apply(plan);
    })) ||
    receipt?.outcome !== "APPLIED"
  )
    return;
  const applied = new Set(
    receipt.operationReceipts.map((item) => item.operationRef),
  );
  const kinds = new Set<string>();
  for (const operation of plan.operations) {
    if (!applied.has(operation.ref)) continue;
    switch (operation.type) {
      case "CREATE_PROJECT":
      case "UPDATE_PROJECT":
        kinds.add("PROJECT");
        break;
      case "CREATE_AGENT":
      case "UPDATE_AGENT":
      case "ARCHIVE_AGENT":
      case "CHANGE_CAPABILITY":
      case "BIND_AGENT_RUNTIME_ENVIRONMENT":
        kinds.add("AGENT");
        break;
      case "CREATE_WORKFLOW":
      case "UPDATE_WORKFLOW":
      case "ARCHIVE_WORKFLOW":
        kinds.add("WORKFLOW");
        break;
      case "CREATE_ROLE_IMAGE_RECIPE":
      case "UPDATE_ROLE_IMAGE_RECIPE":
        kinds.add("ROLE_IMAGE_RECIPE");
        break;
      case "CREATE_SCHEDULE":
      case "UPDATE_SCHEDULE":
        kinds.add("SCHEDULE");
        break;
      case "LAUNCH_RUN":
        kinds.add("RUN");
        break;
      case "CREATE_INTEGRATION_CONNECTION":
      case "UPDATE_INTEGRATION_CONNECTION":
      case "TEST_INTEGRATION_CONNECTION":
        kinds.add("INTEGRATION_CONNECTION");
        break;
      case "PUBLISH_INTEGRATION_DEFINITION":
        kinds.add("INTEGRATION_DEFINITION");
        break;
      case "CHANGE_INTEGRATION_GRANT":
        kinds.add("INTEGRATION_GRANT");
        break;
    }
  }
  // Квитанция уже применена; ошибка вторичного чтения не меняет её исход.
  await Promise.allSettled(
    [...kinds].map((kind) => platform.reloadPlatformKind(kind)),
  );
}

async function rejectPlan(): Promise<void> {
  const plan = currentPlan.value;
  if (plan) await handleStoreMutation(() => store.reject(plan));
}

async function requestPlanChanges(): Promise<void> {
  const plan = currentPlan.value;
  if (!plan || store.busy || store.selectedConversation?.state !== "ACTIVE")
    return;
  await closePlan();
  if (!message.value.trim())
    message.value = t("assistant.planEditor.revisionRequest", {
      revision: plan.revision,
      summary: plan.auditSummary.slice(0, 160),
    });
  await nextTick();
  composer.value?.focus();
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
  contextOpen.value = false;
  integrationImportOpen.value = false;
  createdDefinitionRef.value = undefined;
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
  () => props.projectRef,
  () => {
    secretDialogOpen.value = false;
    secretInitialDraftRef.value = undefined;
    secretSuggestion.value = undefined;
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
  historyMedia?.addEventListener("change", syncHistoryViewport);
  document.addEventListener("pointerdown", documentPointerDown);
  window.addEventListener(openAssistantEvent, handleOpenAssistant);
  if (route.query.assistantCreateSecret || route.query.assistantSecretDraftRef)
    void resumeAssistantSecretForm();
  else if (assistantFormActive.value) void show();
  else if (open.value) void show();
});
onBeforeUnmount(() => {
  historyMedia?.removeEventListener("change", syncHistoryViewport);
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

  <div
    v-if="open"
    class="assistant-overlay"
    :class="{ 'assistant-overlay--with-form': assistantFormActive }"
    :role="assistantFormActive ? 'dialog' : 'presentation'"
    :aria-modal="assistantFormActive || undefined"
    :aria-label="assistantFormActive ? $t('assistant.title') : undefined"
    :inert="integrationImportOpen || secretDialogOpen"
    :aria-hidden="integrationImportOpen || secretDialogOpen || undefined"
  >
    <button
      class="assistant-overlay__backdrop"
      type="button"
      :aria-label="$t('common.close')"
      :disabled="store.busy"
      @click="close"
    />
    <aside
      :key="currentPlan ? 'PLAN' : 'CHAT'"
      id="assistant-workspace"
      ref="panel"
      class="assistant-drawer"
      :class="{
        'assistant-drawer--plan': currentPlan,
        'assistant-drawer--with-form': assistantFormActive,
      }"
      :role="assistantFormActive ? 'region' : 'dialog'"
      :aria-modal="!assistantFormActive || undefined"
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
          :label="assistantReadinessLabel"
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
            ref="mobileHistory"
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
              :data-conversation-ref="conversation.ref"
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
            <div
              ref="mobileHistorySentinel"
              class="assistant-history-sentinel"
              aria-hidden="true"
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
        ref="desktopHistory"
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
          :data-conversation-ref="conversation.ref"
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
        <div
          ref="desktopHistorySentinel"
          class="assistant-history-sentinel"
          aria-hidden="true"
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
        :can-request-changes="
          props.live && store.selectedConversation?.state === 'ACTIVE'
        "
        :problem="store.problem"
        @close="closePlan"
        @save="savePlan"
        @validate="validatePlan"
        @apply="applyPlan"
        @reject="rejectPlan"
        @request-changes="requestPlanChanges"
        @prepare-secret="openSuggestedSecretForm"
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
            <button
              type="button"
              class="assistant-context-strip"
              :aria-label="$t('assistant.context')"
              :aria-expanded="contextOpen"
              @click="contextOpen = true"
            >
              <span>{{ $t("assistant.context") }}</span>
              <strong>{{ contextTitle }}</strong>
              <small>{{ context.route }}</small>
            </button>
            <section
              v-if="createdDefinitionRef"
              class="assistant-integration-draft"
              role="status"
            >
              <span>{{ $t("assistant.integrationDraftCreated") }}</span>
              <button
                class="button button--primary"
                type="button"
                @click="openCreatedDefinition"
              >
                {{ $t("assistant.openIntegrationDraft") }}
              </button>
            </section>
            <OverlayPanel
              v-if="contextOpen"
              v-model:open="contextOpen"
              mode="drawer"
              :ariaLabel="$t('assistant.context')"
              :close-label="$t('common.close')"
              teleport-to="#assistant-workspace"
              @keydown.stop
            >
              <template #header
                ><strong>{{ contextTitle }}</strong></template
              >
              <p>{{ context.route }}</p>
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
            </OverlayPanel>

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
                <template v-if="providerAccountRequired">
                  <h2>{{ $t("assistant.providerAccountRequired") }}</h2>
                  <p>{{ $t("assistant.providerAccountRequiredHelp") }}</p>
                  <RouterLink
                    class="button button--primary"
                    :to="{ name: 'provider-accounts' }"
                    @click="close"
                    >{{ $t("assistant.openProviderAccounts") }}</RouterLink
                  >
                </template>
                <template v-else>
                  <h2>{{ $t("assistant.ready") }}</h2>
                  <p>{{ $t("assistant.contextHelp") }}</p>
                  <div class="assistant-setup-guide">
                    <strong>{{ $t("assistant.setup.title") }}</strong>
                    <p>{{ $t("assistant.setup.help") }}</p>
                    <ol>
                      <li
                        v-for="suggestion in setupSuggestions"
                        :key="suggestion.step"
                      >
                        <button
                          class="button"
                          type="button"
                          :disabled="!canSend || !!message.trim()"
                          @click="suggestSetup(suggestion.prompt)"
                        >
                          {{ suggestion.title }}
                        </button>
                      </li>
                    </ol>
                  </div>
                </template>
              </div>
              <article
                v-for="turn in store.selectedConversation?.turns ?? []"
                v-else
                :key="turn.ref"
                class="assistant-message"
                :class="`assistant-message--${turn.role.toLowerCase()}`"
                :data-turn-ref="turn.ref"
                :data-turn-sequence="turn.sequence"
                @click.capture="handleAssistantLink"
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
                  <AssistantRoleImageBuildCard
                    v-for="operation in turn.plan.operations.filter(
                      (item) =>
                        item.type === 'CREATE_ROLE_IMAGE_RECIPE' ||
                        item.type === 'UPDATE_ROLE_IMAGE_RECIPE',
                    )"
                    :key="`build-${operation.ref}`"
                    :plan="turn.plan"
                    :operation-ref="operation.ref"
                  />
                  <AssistantCreatedEntityCard
                    v-for="operation in turn.plan.operations.filter(
                      (item) =>
                        item.type === 'CREATE_PROJECT' ||
                        item.type === 'CREATE_AGENT',
                    )"
                    :key="`entity-${operation.ref}`"
                    :plan="turn.plan"
                    :operation-ref="operation.ref"
                    @navigate="close"
                  />
                  <AssistantEnvironmentDraftCard
                    v-for="operation in turn.plan.operations.filter(
                      (item) =>
                        item.type === 'CREATE_RUNTIME_ENVIRONMENT_DRAFT' ||
                        item.type === 'PREPARE_RUNTIME_ENVIRONMENT_REVISION',
                    )"
                    :key="`environment-${operation.ref}`"
                    :plan="turn.plan"
                    :operation-ref="operation.ref"
                  />
                  <AssistantAgentEnvironmentBindingCard
                    v-for="operation in turn.plan.operations.filter(
                      (item) => item.type === 'BIND_AGENT_RUNTIME_ENVIRONMENT',
                    )"
                    :key="`binding-${operation.ref}`"
                    :plan="turn.plan"
                    :operation-ref="operation.ref"
                    @navigate="close"
                  />
                  <AssistantIntegrationConnectionCard
                    v-for="operation in turn.plan.operations.filter(
                      (item) => item.type === 'CREATE_INTEGRATION_CONNECTION',
                    )"
                    :key="`connection-${operation.ref}`"
                    :plan="turn.plan"
                    :operation-ref="operation.ref"
                    @navigate="close"
                  />
                  <AssistantCreatedScheduleCard
                    v-for="operation in turn.plan.operations.filter(
                      (item) =>
                        item.type === 'CREATE_SCHEDULE' ||
                        item.type === 'UPDATE_SCHEDULE',
                    )"
                    :key="`schedule-${operation.ref}`"
                    :plan="turn.plan"
                    :operation-ref="operation.ref"
                    @navigate="close"
                  />
                  <AssistantCreatedWorkflowCard
                    v-for="operation in turn.plan.operations.filter(
                      (item) =>
                        item.type === 'CREATE_WORKFLOW' ||
                        item.type === 'UPDATE_WORKFLOW',
                    )"
                    :key="`workflow-${operation.ref}`"
                    :plan="turn.plan"
                    :operation-ref="operation.ref"
                    @navigate="close"
                  />
                  <AssistantLaunchedRunCard
                    v-for="operation in turn.plan.operations.filter(
                      (item) => item.type === 'LAUNCH_RUN',
                    )"
                    :key="`run-${operation.ref}`"
                    :plan="turn.plan"
                    :operation-ref="operation.ref"
                    @navigate="close"
                  />
                  <button
                    class="button button--primary"
                    type="button"
                    @click="openPlan(turn.plan)"
                  >
                    {{ $t("assistant.openPlan") }}
                  </button>
                </section>
              </article>
              <div
                v-if="awaitingReply && !store.loading && !store.problem"
                class="assistant-message assistant-message--typing"
                role="status"
                :aria-label="$t('assistant.working')"
              >
                <strong>{{ $t("assistant.working") }}</strong>
                <span class="assistant-typing-dots" aria-hidden="true">
                  <i></i><i></i><i></i>
                </span>
              </div>
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
              <button
                v-if="projectRef"
                class="assistant-composer__protected-link"
                type="button"
                @click="openPlainSecretForm"
              >
                {{ $t("assistant.openSecretForm") }}
              </button>
              <small>{{ $t("assistant.audit") }}</small>
            </footer>
          </div>
        </div>
      </template>
    </aside>
    <section
      v-if="assistantFormActive"
      id="assistant-form-slot"
      ref="formSlot"
      class="assistant-form-slot"
      :aria-label="$t('assistant.planEditor.parametersTitle')"
      @keydown="handleKeydown"
    >
      <button
        class="assistant-form-slot__close icon-button"
        type="button"
        :aria-label="$t('common.close')"
        @click="closeAssistantForm"
      >
        <X :size="18" aria-hidden="true" />
      </button>
    </section>
  </div>
  <OpenAPIImportDialog
    v-if="open && integrationImportOpen"
    @close="integrationImportOpen = false"
    @created="integrationDraftCreated"
  />
  <Teleport to="body">
    <div
      v-if="open && secretDialogOpen && projectRef"
      class="assistant-secret-layer"
    >
      <RuntimeSecretDraftDialog
        :project-ref="projectRef"
        :initial-draft-ref="secretInitialDraftRef"
        :suggestion="secretSuggestion"
        assistant
        @close="
          secretDialogOpen = false;
          secretInitialDraftRef = undefined;
          secretSuggestion = undefined;
        "
      />
    </div>
  </Teleport>
</template>

<style scoped>
.assistant-secret-layer {
  position: fixed;
  z-index: 90;
  inset: 0;
  pointer-events: none;
}
.assistant-secret-layer :deep(.modal-backdrop) {
  pointer-events: auto;
}
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
.assistant-integration-draft {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 12px 16px;
  border-bottom: 1px solid var(--border);
  background: var(--accent-soft);
}
@media (max-width: 720px) {
  .assistant-integration-draft {
    align-items: stretch;
    flex-direction: column;
  }
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
  border: 1px solid var(--border);
  border-radius: 12px;
  background: var(--surface);
  box-shadow: 0 18px 48px rgb(15 23 42 / 20%);
  outline: 0;
}
.assistant-drawer--plan {
  inset: 4dvh 4vw;
  width: 92vw;
  max-width: 92vw;
  height: 92dvh;
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
    display: -webkit-box;
    overflow: hidden;
    -webkit-box-orient: vertical;
    -webkit-line-clamp: 2;
    overflow-wrap: anywhere;
    line-height: 1.3;
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
  flex: 0 0 72px;
  align-items: center;
  gap: 10px;
  height: 72px;
  min-height: 72px;
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
.assistant-history-sentinel {
  min-height: 1px;
  flex: 0 0 1px;
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
  width: 100%;
  border: 0;
  border-bottom: 1px solid var(--border);
  color: var(--text);
  text-align: left;
  cursor: pointer;
}
.assistant-context-strip > strong {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
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
.assistant-setup-guide {
  display: grid;
  width: min(100%, 640px);
  gap: 8px;
  margin-top: 12px;
  padding: 14px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--panel);
  color: var(--text);
  text-align: left;
}
.assistant-setup-guide ol {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 8px;
  margin: 0;
  padding: 0;
  list-style: none;
}
.assistant-setup-guide li,
.assistant-setup-guide button {
  min-width: 0;
  width: 100%;
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
.assistant-message--typing {
  display: flex;
  align-items: center;
  gap: 10px;
  color: var(--muted);
}
.assistant-typing-dots {
  display: inline-flex;
  align-items: center;
  gap: 4px;
}
.assistant-typing-dots i {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: currentColor;
  animation: assistant-typing 1.2s ease-in-out infinite;
}
.assistant-typing-dots i:nth-child(2) {
  animation-delay: 0.15s;
}
.assistant-typing-dots i:nth-child(3) {
  animation-delay: 0.3s;
}
@keyframes assistant-typing {
  0%,
  60%,
  100% {
    opacity: 0.35;
    transform: translateY(0);
  }
  30% {
    opacity: 1;
    transform: translateY(-4px);
  }
}
@media (prefers-reduced-motion: reduce) {
  .assistant-typing-dots i {
    animation: none;
    opacity: 0.75;
  }
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
.assistant-composer__protected-link {
  width: fit-content;
  color: var(--accent-strong);
  font-size: 0.82rem;
}
@media (max-width: 720px) {
  .assistant-setup-guide ol {
    grid-template-columns: minmax(0, 1fr);
  }
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
    width: 92vw;
    max-width: 92vw;
  }
  .assistant-drawer--plan {
    width: 92vw;
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
.assistant-drawer.assistant-drawer--with-form {
  right: auto;
  width: min(35vw, 620px);
  max-width: none;
}
@media (min-width: 1001px) {
  .assistant-drawer.assistant-drawer--with-form {
    padding-left: 0;
  }
  .assistant-drawer--with-form .assistant-conversation-sidebar {
    display: none;
  }
  .assistant-drawer--with-form .assistant-new-conversation span,
  .assistant-drawer--with-form .assistant-history__toggle span,
  .assistant-drawer--with-form .assistant-history__toggle svg:last-child,
  .assistant-drawer--with-form
    .assistant-drawer__header
    > :deep(.status-badge) {
    display: none;
  }
}
.assistant-form-slot {
  position: fixed;
  z-index: 1;
  top: 4dvh;
  right: 4vw;
  bottom: 4dvh;
  left: min(calc(4vw + min(35vw, 620px) + 12px), 48vw);
  overflow: auto;
  border: 1px solid var(--border);
  border-radius: 12px;
  background: var(--surface);
}
.assistant-form-slot__close {
  position: sticky;
  z-index: 2;
  top: 8px;
  left: calc(100% - 44px);
  margin: 8px 8px -44px auto;
  background: var(--surface);
}
@media (max-width: 1000px) {
  .assistant-drawer.assistant-drawer--with-form {
    inset: 0 0 auto;
    width: 100%;
    height: 42dvh;
  }
  .assistant-form-slot {
    top: 42dvh;
    right: 0;
    bottom: 0;
    left: 0;
    border-radius: 0;
  }
}
</style>
