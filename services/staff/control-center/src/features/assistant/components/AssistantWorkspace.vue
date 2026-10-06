<script setup lang="ts">
import { createRuntimeResourceCatalogs } from "@/features/runtime/resource-catalog-api";
import {
  Activity,
  ArrowUp,
  Bot,
  Check,
  ChevronDown,
  EllipsisVertical,
  History,
  KeyRound,
  ListChecks,
  Pencil,
  Plus,
  RotateCcw,
  Send,
  Settings,
  Sparkles,
  Square,
  Zap,
  Trash2,
  X,
} from "@lucide/vue";
import {
  computed,
  nextTick,
  onBeforeUnmount,
  onMounted,
  ref,
  useId,
  watch,
} from "vue";
import { useI18n } from "vue-i18n";
import { useRoute, useRouter } from "vue-router";

import AssistantPlanEditor from "@/features/assistant/components/AssistantPlanEditor.vue";
import AssistantPlanRecord from "./AssistantPlanRecord.vue";
import AgentRuntimePanel from "@/features/agents/detail/AgentRuntimePanel.vue";
import AssistantEnvironmentSettingsPanel from "@/features/assistant/components/AssistantEnvironmentSettingsPanel.vue";
import SystemAssistantIntegrationGrantsPanel from "./SystemAssistantIntegrationGrantsPanel.vue";
import AssistantProjectProfileSetup from "./AssistantProjectProfileSetup.vue";
import AssistantCreatedScheduleCard from "@/features/assistant/components/AssistantCreatedScheduleCard.vue";
import AssistantCreatedEntityCard from "@/features/assistant/components/AssistantCreatedEntityCard.vue";
import AssistantCreatedProjectFileCard from "@/features/assistant/components/AssistantCreatedProjectFileCard.vue";
import AssistantInstructionDraftCard from "@/features/assistant/components/AssistantInstructionDraftCard.vue";
import AssistantAgentEnvironmentBindingCard from "@/features/assistant/components/AssistantAgentEnvironmentBindingCard.vue";
import AssistantCreatedWorkflowCard from "@/features/assistant/components/AssistantCreatedWorkflowCard.vue";
import AssistantEnvironmentDraftCard from "@/features/assistant/components/AssistantEnvironmentDraftCard.vue";
import AssistantIntegrationConnectionCard from "@/features/assistant/components/AssistantIntegrationConnectionCard.vue";
import AssistantIntegrationCredentialDialog from "@/features/assistant/components/AssistantIntegrationCredentialDialog.vue";
import AssistantLaunchedRunCard from "@/features/assistant/components/AssistantLaunchedRunCard.vue";
import AssistantRoleImageBuildCard from "@/features/assistant/components/AssistantRoleImageBuildCard.vue";
import { OpenAPIImportDialog } from "@/features/managed-configurations";
import AssistantHistoryFilter from "./AssistantHistoryFilter.vue";
import { useAdaptiveCursorPageSize } from "@/shared/ui/cursor-list";
import {
  assistantContextIdentity,
  assistantContextRouteLabelKey,
  assistantContextTitle,
  conversationMatchesContext,
  readableContextOperations,
  readableContextKind,
} from "@/features/assistant/context";
import {
  isAssistantRunDebugRequest,
  isAssistantRoleImageBuildDebugRequest,
  isAssistantSetupRequest,
  isAssistantSettingsRequest,
  notifyAssistantPlanApplied,
  openAssistantEvent,
  type AssistantIntegrationPublicationRequest,
} from "@/features/assistant/events";
import {
  assistantActiveUserTurn,
  assistantAwaitingReply,
  assistantEffectiveRuntimeState,
  assistantRequiresProviderAccount,
  operationActionLabel,
  operationSupportingTitle,
  operationTargetLabel,
} from "@/features/assistant/model";
import { useAssistantStore } from "@/features/assistant/store";
import { usePlatformStore } from "@/features/platform/store";
import { useRealtimeStore } from "@/features/realtime/store";
import { organizationRuntimeResourceScope } from "@/features/runtime/resource-scope";
import {
  persistAssistantConversationRef,
  persistAssistantWorkspaceOpen,
  restoreAssistantWorkspaceOpen,
} from "@/features/assistant/workspace-state";
import RunActivityView from "@/features/runs/RunActivityView.vue";
import {
  buildAssistantChatTimeline,
  activeAssistantChatItemId,
} from "@/features/assistant/chat-timeline";
import {
  assistantTurnHasAuthoritativeActivity,
  assistantTurnIsEmptyTerminalReceipt,
  assistantTurnIsDuplicateFailureReceipt,
  assistantTerminalTranscriptScopes,
  assistantTranscriptReplacesWorkingFallback,
  assistantFailureMessageKey,
  isTranscriptNearBottom,
} from "@/features/runs/run-activity";
import { runtimeProgressKey } from "@/features/runs/runtime-text";
import RuntimeSecretDraftDialog from "@/features/runtime-secrets/RuntimeSecretDraftDialog.vue";
import type { RuntimeSecretDraftSuggestion } from "@/features/runtime-secrets/model";
import { consumeRuntimeSecretReauthSuggestion } from "@/features/runtime-secrets/reauth-suggestion";
import type {
  AssistantContextDescriptor,
  AssistantPlan,
  AssistantPlanReceipt,
  RunEvent,
  AssistantScope,
  AssistantTurn,
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
import { requestConfirmation } from "@/shared/ui/confirmation";
import { useCursorInfiniteScroll } from "@/shared/ui/async-entity-picker";
import SafeMarkdown from "@/shared/ui/SafeMarkdown.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";
import VoiceTextarea from "@/shared/ui/VoiceTextarea.vue";
import { runPath } from "@/shared/routes";

const props = withDefaults(
  defineProps<{
    context: AssistantContextDescriptor;
    projectRef?: string;
    live?: boolean;
    runEvents?: readonly RunEvent[];
  }>(),
  { live: false, runEvents: () => [] },
);

const planTargetKindTranslationKeys: Readonly<Record<string, string>> = {
  PROJECT: "assistant.planEditor.targetKinds.PROJECT",
  AGENT: "assistant.planEditor.targetKinds.AGENT",
  WORKFLOW: "assistant.planEditor.targetKinds.WORKFLOW",
  SCHEDULE: "assistant.planEditor.targetKinds.SCHEDULE",
  EXECUTION: "assistant.planEditor.targetKinds.EXECUTION",
  ENVIRONMENT: "assistant.planEditor.targetKinds.ENVIRONMENT",
  RUNTIME_ENVIRONMENT_DRAFT:
    "assistant.planEditor.targetKinds.RUNTIME_ENVIRONMENT_DRAFT",
  ROLE_IMAGE_RECIPE: "assistant.planEditor.targetKinds.ROLE_IMAGE_RECIPE",
  PROJECT_ASSISTANT: "assistant.projectProfile.title",
  INTEGRATION_CONNECTION:
    "assistant.planEditor.targetKinds.INTEGRATION_CONNECTION",
  INTEGRATION_DEFINITION:
    "assistant.planEditor.targetKinds.INTEGRATION_DEFINITION",
};

function operationTargetKindLabel(kind: string): string {
  const key = planTargetKindTranslationKeys[kind];
  return key ? t(key) : kind;
}

const { t } = useI18n();
const route = useRoute();
const router = useRouter();
const assistantFormActive = computed(() => route.query.assistantForm === "1");
const store = useAssistantStore();
const titleFieldName = `assistant-conversation-title-${useId()}`;
const platform = usePlatformStore();
const realtime = useRealtimeStore();
const conversationRunRefs = computed(() => [
  ...new Set(
    store.selectedConversation?.turns.flatMap((turn) =>
      turn.runRef ? [turn.runRef] : [],
    ) ?? [],
  ),
]);
const conversationRunEvents = computed(() =>
  conversationRunRefs.value.flatMap((runRef) =>
    Object.values(platform.events[runRef] ?? {}),
  ),
);
const closedTranscriptExecutionKeys = computed(() =>
  conversationRunRefs.value.flatMap((runRef) => {
    const run = platform.runs[runRef];
    const graph = run
      ? (platform.graphs[run.rootRunRef] ?? platform.graphs[run.ref])
      : undefined;
    return assistantTerminalTranscriptScopes(
      store.selectedConversation,
      platform.bootstrap?.organizationRef,
      run,
      graph?.nodes ?? [],
      conversationRunEvents.value,
    );
  }),
);
function turnHasPublishedMessage(turn: AssistantTurn): boolean {
  const run = turn.runRef ? platform.runs[turn.runRef] : undefined;
  const graph = run
    ? (platform.graphs[run.rootRunRef] ?? platform.graphs[run.ref])
    : undefined;
  return assistantTurnHasAuthoritativeActivity(
    turn,
    store.selectedConversation?.turns ?? [],
    run,
    graph?.nodes ?? [],
    conversationRunEvents.value,
  );
}
function transcriptTurnContent(turn: AssistantTurn): string {
  const failureKey =
    turn.role === "ASSISTANT"
      ? assistantFailureMessageKey(turn.content, turn.state)
      : undefined;
  if (failureKey) return t(failureKey);
  const key =
    turn.role !== "USER" ? runtimeProgressKey(turn.content) : undefined;
  return key ? t(key) : turn.content;
}
function turnIsEmptyTerminalReceipt(turn: AssistantTurn): boolean {
  const run = turn.runRef ? platform.runs[turn.runRef] : undefined;
  const graph = run
    ? (platform.graphs[run.rootRunRef] ?? platform.graphs[run.ref])
    : undefined;
  const args = [
    turn,
    store.selectedConversation,
    platform.bootstrap?.organizationRef,
    run,
    graph?.nodes ?? [],
    conversationRunEvents.value,
  ] as const;
  return (
    assistantTurnIsEmptyTerminalReceipt(...args) ||
    assistantTurnIsDuplicateFailureReceipt(...args)
  );
}
const transcriptTurns = computed(() =>
  (store.selectedConversation?.turns ?? []).filter(
    (turn) =>
      turn.plan ||
      (!turnHasPublishedMessage(turn) && !turnIsEmptyTerminalReceipt(turn)),
  ),
);
const chatTimeline = computed(() =>
  buildAssistantChatTimeline(
    store.selectedConversation,
    transcriptTurns.value,
    platform.bootstrap?.organizationRef,
    platform.runs,
    platform.graphs,
    conversationRunEvents.value,
  ),
);
const chatActiveItemId = computed(() =>
  activeAssistantChatItemId(
    chatTimeline.value,
    closedTranscriptExecutionKeys.value,
  ),
);
const transcriptLeases = new Map<string, () => void>();
let transcriptReadGeneration = 0;
const systemResourceScope = computed(() =>
  organizationRuntimeResourceScope(platform.bootstrap),
);
const systemResourceCatalogs = computed(() =>
  systemResourceScope.value
    ? createRuntimeResourceCatalogs(systemResourceScope.value.organizationRef)
    : undefined,
);
const open = ref(
  restoreAssistantWorkspaceOpen() || route.query.assistantForm === "1",
);
const historyOpen = ref(false);
const contextOpen = ref(false);
const integrationImportOpen = ref(false);
const secretDialogOpen = ref(false);
const credentialConnectionRef = ref("");
const connectionRefreshToken = ref(0);
const secretInitialDraftRef = ref<string>();
const secretSuggestion = ref<RuntimeSecretDraftSuggestion>();
const pendingProjectMove = ref<{ projectRef: string; path: string }>();
const projectMoveDestination = computed(() => {
  const projectRef = pendingProjectMove.value?.projectRef;
  return projectRef ? platform.projects[projectRef]?.name : undefined;
});
const projectMoveDialog = ref<HTMLElement>();
const projectMoveTrigger = ref<HTMLAnchorElement>();
let workspaceMounted = false;
let secretResumePending = false;
const createdDefinitionRef = ref<string>();
const desktopHistory = ref<HTMLElement>();
const desktopHistorySentinel = ref<HTMLElement>();
const mobileHistory = ref<HTMLElement>();
const mobileHistorySentinel = ref<HTMLElement>();
const desktopHistoryPageSize = useAdaptiveCursorPageSize({
  container: desktopHistory,
  itemSelector: ".assistant-conversation-entry",
  itemCount: () => store.sortedConversations.length,
  estimatedViewportHeight: 720,
  estimatedItemHeight: 58,
  minimum: 8,
  maximum: 100,
});
const mobileHistoryPageSize = useAdaptiveCursorPageSize({
  container: mobileHistory,
  itemSelector: ".assistant-history__menu > button",
  itemCount: () => store.sortedConversations.length,
  estimatedViewportHeight: 420,
  estimatedItemHeight: 58,
  minimum: 6,
  maximum: 100,
});
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
const messageDrafts = new Map<string, string>();
const titleDraft = ref("");
const titleEditing = ref(false);
const settingsOpen = ref(false);
const settingsTab = ref<
  "RUNTIME" | "ENVIRONMENT" | "INSTRUCTIONS" | "INTEGRATIONS"
>("RUNTIME");
watch(
  () => store.assistantScope,
  (scope) => {
    if (scope !== "SYSTEM" && settingsTab.value === "INTEGRATIONS")
      settingsTab.value = "RUNTIME";
  },
);
const assistantInstructions = ref("");
const settingsBusy = ref(false);
const settingsProblem = ref<AppProblem>();
const projectProfileName = ref("");
const projectProfilePurpose = ref("");
const projectProfileInstructions = ref("");
const canCreateProjectProfile = computed(() =>
  Boolean(
    props.projectRef &&
    platform.projects[props.projectRef]?.nextActions.includes(
      "CREATE_PROJECT_ASSISTANT",
    ),
  ),
);
const activeSettingsAgentRef = computed(() => store.activeAssistantRef);
const activeSettingsCanEdit = computed(() =>
  store.assistantScope === "PROJECT"
    ? Boolean(store.projectAssistantAgent?.nextActions.includes("EDIT"))
    : Boolean(store.assistant?.nextActions.includes("EDIT")),
);
const projectAssistantCanRun = computed(() =>
  Boolean(
    store.projectAssistantAgent?.enabled &&
    store.projectAssistantAgent.runtimeReady &&
    store.projectAssistantAgent.nextActions.includes("LAUNCH"),
  ),
);
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
const planDialog = ref<HTMLElement>();
const formSlot = ref<HTMLElement>();
const composer = ref<{ focus(): void }>();
const chatLog = ref<HTMLElement>();
const chatFollowing = ref(true);
const chatUnread = ref(false);
const expandedHistoryMessages = ref<Record<string, boolean>>({});
let followLatestAfterLoad = false;
let chatResizeObserver: ResizeObserver | undefined;
let latestRestoreTimer: number | undefined;
const historyMenu = ref<HTMLElement>();
const openConversationMenu = ref<string>();
const fab = ref<HTMLButtonElement>();
const planTrigger = ref<HTMLButtonElement>();

const checkedContext = computed(() => {
  const conversation = store.selectedConversation;
  return !store.loading &&
    !store.problem &&
    conversation &&
    conversationMatchesContext(conversation, props.context)
    ? conversation.context
    : undefined;
});
const configurationKinds = new Set([
  "PROMPT_TEMPLATE",
  "ROLE_IMAGE",
  "INTEGRATION_DEFINITION",
  "SYSTEM_STT",
]);
const contextTitle = computed(() => {
  if (
    route.name === "home" &&
    !props.context.entityRef &&
    !checkedContext.value?.entityRef
  ) {
    return t("nav.home");
  }
  const kind = route.params.kind;
  if (
    !props.context.entityName &&
    !checkedContext.value?.entityName &&
    (route.name === "configuration" ||
      route.name === "configuration-catalog") &&
    typeof kind === "string" &&
    configurationKinds.has(kind)
  ) {
    return t(`managed.kinds.${kind}`);
  }
  const routeLabelKey = assistantContextRouteLabelKey(route.name);
  return assistantContextTitle(
    props.context,
    checkedContext.value,
    routeLabelKey ? t(routeLabelKey) : undefined,
  );
});
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
  store.assistantScope === "PROJECT"
    ? projectAssistantCanRun.value
      ? "READY"
      : "NOT_CONFIGURED"
    : store.assistant
      ? assistantEffectiveRuntimeState(store.assistant)
      : "RECOVERING",
);
const awaitingReply = computed(() =>
  assistantAwaitingReply(store.selectedConversation),
);
const showWorkingFallback = computed(() => {
  if (!awaitingReply.value) return false;
  const runRef = assistantActiveUserTurn(store.selectedConversation)?.runRef;
  const run = runRef ? platform.runs[runRef] : undefined;
  const graph = run
    ? (platform.graphs[run.rootRunRef] ?? platform.graphs[run.ref])
    : undefined;
  return !assistantTranscriptReplacesWorkingFallback(
    store.selectedConversation,
    platform.bootstrap?.organizationRef,
    run,
    graph?.nodes ?? [],
    conversationRunEvents.value,
  );
});
const providerAccountRequired = computed(
  () =>
    store.assistant !== undefined &&
    store.assistantScope === "SYSTEM" &&
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
  !props.live
    ? t("app.reconnecting")
    : providerAccountRequired.value
      ? t("assistant.providerAccountRequired")
      : store.assistantScope === "PROJECT"
        ? t(
            projectAssistantCanRun.value
              ? "assistant.projectProfile.ready"
              : "assistant.projectProfile.needsSetupBadge",
          )
        : store.assistant?.readinessSummary,
);
const canCreateConversation = computed(() =>
  store.assistantScope === "PROJECT"
    ? Boolean(
        store.projectAssistant && store.projectAssistant.state === "ACTIVE",
      )
    : assistantRuntimeState.value === "READY" &&
      Boolean(store.assistant?.nextActions.includes("CREATE_CONVERSATION")),
);
const canSend = computed(
  () =>
    props.live &&
    !store.loading &&
    !store.busy &&
    assistantRuntimeState.value === "READY" &&
    (store.assistantScope === "PROJECT"
      ? projectAssistantCanRun.value
      : Boolean(store.assistant?.nextActions.includes("ADD_TURN"))) &&
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
    loadMore: () =>
      store.loadMoreHistory(
        desktopHistoryVisible.value
          ? desktopHistoryPageSize.value
          : mobileHistoryPageSize.value,
      ),
  });
}

watch(
  [desktopHistoryPageSize, mobileHistoryPageSize, desktopHistoryVisible],
  ([desktopSize, mobileSize, desktopVisible]) =>
    store.setHistoryPageSize(desktopVisible ? desktopSize : mobileSize),
  { immediate: true },
);

const contextIdentity = computed(() =>
  assistantContextIdentity(props.context, props.projectRef),
);
const currentDraftKey = computed(
  () =>
    store.selectedRef ??
    `${store.assistantScope}:${store.activeAssistantRef ?? "unconfigured"}:context:${contextIdentity.value}`,
);

function handleOpenAssistant(event: Event): void {
  void (async () => {
    const request =
      event instanceof CustomEvent ? (event.detail as unknown) : undefined;
    await show();
    if (isAssistantSettingsRequest(request)) {
      await store.selectAssistantScope("SYSTEM");
      openAssistantSettings();
      return;
    }
    if (isAssistantSetupRequest(request)) {
      if (
        message.value.trim() &&
        !(await requestConfirmation(t("assistant.replaceDraftConfirm")))
      )
        return;
      message.value = t(`onboarding.assistantPrompts.${request.step}`);
      await nextTick();
      composer.value?.focus();
      return;
    }
    if (isAssistantRoleImageBuildDebugRequest(request)) {
      if (
        message.value.trim() &&
        !(await requestConfirmation(
          t("assistant.replaceDraftWithBuildDebugConfirm"),
        ))
      )
        return;
      message.value = t("assistant.roleImageBuild.debugPrompt", {
        recipeRef: request.recipeRef,
        buildRef: request.buildRef,
        attempt: request.attempt,
        stage: request.stage,
        safeErrorCode: request.safeErrorCode || "NONE",
        diagnosticCode: request.diagnosticCode || "NONE",
        diagnosticSummary: request.diagnosticSummary || "NONE",
      });
      await nextTick();
      composer.value?.focus();
      return;
    }
    if (isAssistantRunDebugRequest(request)) {
      if (
        message.value.trim() &&
        !(await requestConfirmation(
          t("assistant.replaceDraftWithRunDebugConfirm"),
        ))
      )
        return;
      message.value = t("assistant.runDebug.prompt", {
        runRef: request.runRef,
        rootRunRef: request.rootRunRef,
        targetType: request.targetType,
        attempt: request.attempt,
        safeErrorCode: request.safeErrorCode || "NONE",
        failedNodes: request.failedNodes
          .map(
            (node) =>
              `nodeRef=${node.nodeRef},type=${node.type},agentRef=${node.agentRef || "NONE"},safeErrorCode=${node.safeErrorCode || "NONE"}`,
          )
          .join(" | "),
      });
      await nextTick();
      composer.value?.focus();
      return;
    }
    const publication = request as
      | AssistantIntegrationPublicationRequest
      | undefined;
    if (
      !publication ||
      !/^mcfg_[A-Za-z0-9_-]{1,91}$/.test(publication.configurationRef) ||
      !/^mrev_[A-Za-z0-9_-]{1,91}$/.test(publication.revisionRef)
    )
      return;
    if (
      message.value.trim() &&
      !(await requestConfirmation(t("assistant.replaceDraftConfirm")))
    )
      return;
    message.value = t("assistant.publishIntegrationRequest", {
      configurationRef: publication.configurationRef,
      revisionRef: publication.revisionRef,
    });
    await nextTick();
    composer.value?.focus();
  })();
}

function hydrateFromRealtimeSnapshot(): boolean {
  if (
    !platform.assistant ||
    platform.assistantRealtimeScopeKey !== (props.projectRef ?? "")
  )
    return false;
  store.setContext(props.context, props.projectRef);
  store.applyRealtimeSnapshot(
    platform.assistant,
    Object.values(platform.conversations),
    props.projectRef,
    platform.assistantConversationNextPageToken,
  );
  return true;
}

function loadWorkspace(): void {
  store.setContext(props.context, props.projectRef);
  if (store.assistantScope === "PROJECT") {
    void store.load(props.context, props.projectRef);
  } else hydrateFromRealtimeSnapshot();
}

async function show(): Promise<void> {
  open.value = true;
  persistAssistantWorkspaceOpen(true);
  historyOpen.value = false;
  openPlanRef.value = undefined;
  activeView.value = "CHAT";
  loadWorkspace();
  await nextTick();
  panel.value?.focus({ preventScroll: true });
  restoreLatestAfterRender();
}

async function close(): Promise<void> {
  if (store.busy) return;
  if (pendingProjectMove.value) {
    cancelProjectMove();
    return;
  }
  if (secretDialogOpen.value) return;
  if (credentialConnectionRef.value) return;
  if (assistantFormActive.value) {
    void closeAssistantForm();
    return;
  }
  if (
    (message.value.trim() ||
      attachmentState.value.count > 0 ||
      attachmentState.value.busy) &&
    !(await requestConfirmation(t("assistant.closeWithDraftConfirm")))
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
  if (
    secretResumePending ||
    !props.projectRef ||
    route.params.projectRef !== props.projectRef
  )
    return;
  const creating = route.query.assistantCreateSecret === "1";
  const draftRef = route.query.assistantSecretDraftRef;
  const resuming =
    typeof draftRef === "string" && /^[-_A-Za-z0-9]{8,128}$/.test(draftRef);
  if (!creating && !resuming) return;
  secretResumePending = true;
  try {
    await show();
    secretInitialDraftRef.value = resuming ? draftRef : undefined;
    secretSuggestion.value = creating
      ? consumeRuntimeSecretReauthSuggestion(window.sessionStorage, {
          projectRef: props.projectRef,
          surface: "assistant",
        })
      : undefined;
    secretDialogOpen.value = true;
    await router.replace({
      query: {
        ...route.query,
        assistantCreateSecret: undefined,
        assistantSecretDraftRef: undefined,
      },
    });
  } finally {
    secretResumePending = false;
  }
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
    else void close();
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

function temporaryConversationTitle(conversationRef?: string): string {
  const draft =
    conversationRef === store.selectedRef
      ? message.value
      : conversationRef
        ? messageDrafts.get(conversationRef)
        : undefined;
  const normalized = draft?.replace(/\s+/g, " ").trim() ?? "";
  if (normalized.length < 12) return "";
  return normalized.length > 58
    ? `${normalized.slice(0, 57).trimEnd()}…`
    : normalized;
}

function conversationDisplayTitle(
  title: string,
  conversationRef?: string,
): string {
  const normalized = title.trim();
  if (normalized && normalized !== t("assistant.newConversation"))
    return normalized;
  return (
    temporaryConversationTitle(conversationRef) ||
    normalized ||
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
  const conversation = store.selectedConversation;
  if (!props.live || !conversation || conversation.state === "ARCHIVED") return;
  await moveConversationToTrash(conversation);
}

function toggleConversationMenu(ref: string): void {
  openConversationMenu.value =
    openConversationMenu.value === ref ? undefined : ref;
}

async function moveConversationToTrash(
  conversation: NonNullable<typeof store.selectedConversation>,
): Promise<void> {
  openConversationMenu.value = undefined;
  if (
    !(await requestConfirmation({
      message: t("assistant.trashConfirm"),
      confirmLabel: t("assistant.deleteConversation"),
      tone: "danger",
    }))
  )
    return;
  if (await handleStoreMutation(() => store.moveToTrash(conversation))) {
    messageDrafts.delete(conversation.ref);
    titleEditing.value = false;
    openPlanRef.value = undefined;
    attachmentComposer.value?.clear();
  }
}

async function restoreConversationFromTrash(
  conversation: NonNullable<typeof store.selectedConversation>,
): Promise<void> {
  openConversationMenu.value = undefined;
  await handleStoreMutation(() => store.restoreFromTrash(conversation));
}

async function purgeConversationFromTrash(
  conversation: NonNullable<typeof store.selectedConversation>,
): Promise<void> {
  openConversationMenu.value = undefined;
  if (
    !(await requestConfirmation({
      message: t("assistant.purgeConfirm"),
      confirmLabel: t("assistant.purgeConversation"),
      tone: "danger",
    }))
  )
    return;
  if (await handleStoreMutation(() => store.purgeFromTrash(conversation)))
    messageDrafts.delete(conversation.ref);
}

async function emptyConversationTrash(): Promise<void> {
  openConversationMenu.value = undefined;
  if (
    !(await requestConfirmation({
      message: t("assistant.emptyTrashConfirm"),
      confirmLabel: t("assistant.emptyTrash"),
      tone: "danger",
    }))
  )
    return;
  if (await handleStoreMutation(() => store.emptyTrash()))
    messageDrafts.clear();
}

async function saveTitle(): Promise<void> {
  if (!(await handleStoreMutation(() => store.changeTitle(titleDraft.value))))
    return;
  titleEditing.value = false;
}

async function send(
  deliveryMode: "QUEUE" | "INTERRUPT_ACTIVE" = "QUEUE",
): Promise<void> {
  const value = message.value.trim();
  if (!value || !canSend.value) return;
  const attachmentSetRef = await attachmentComposer.value?.finalize();
  if (
    !(await handleStoreMutation(() =>
      store.send(value, attachmentSetRef, deliveryMode),
    ))
  )
    return;
  messageDrafts.delete(currentDraftKey.value);
  message.value = "";
  attachmentComposer.value?.clear();
  await nextTick();
  scrollToLatest();
  composer.value?.focus();
}

async function stopActiveTurn(): Promise<void> {
  await handleStoreMutation(() => store.stopActiveTurn());
}

function openAssistantSettings(): void {
  assistantInstructions.value = store.assistant?.ownerInstructions ?? "";
  settingsProblem.value = undefined;
  settingsOpen.value = true;
}

async function selectAssistantScope(event: Event): Promise<void> {
  const scope = (event.target as HTMLSelectElement).value;
  if (scope !== "SYSTEM" && scope !== "PROJECT") return;
  settingsOpen.value = false;
  await store.selectAssistantScope(scope as AssistantScope);
}

async function createProjectProfile(): Promise<void> {
  if (!canCreateProjectProfile.value) return;
  if (
    !projectProfileName.value.trim() ||
    !projectProfilePurpose.value.trim() ||
    !projectProfileInstructions.value.trim()
  )
    return;
  await handleStoreMutation(() =>
    store.createProjectProfile({
      name: projectProfileName.value.trim(),
      purpose: projectProfilePurpose.value.trim(),
      instructions: projectProfileInstructions.value,
    }),
  );
  if (store.projectAssistant) openAssistantSettings();
}

async function openProjectAssistantEditor(tab = "instructions"): Promise<void> {
  const profile = store.projectAssistant;
  if (!profile) return;
  settingsOpen.value = false;
  await close();
  if (open.value) return;
  void router.push({
    name: "agent",
    params: { projectRef: profile.projectRef, agentRef: profile.agentRef },
    query: { tab },
  });
}

async function openAssistantTurnRun(runRef: string): Promise<void> {
  const conversation = store.selectedConversation;
  if (!conversation?.turns.some((turn) => turn.runRef === runRef)) return;
  const target = runPath(runRef, conversation.projectRef);
  await close();
  if (open.value) return;
  await router.push(target);
}

async function saveAssistantInstructions(): Promise<void> {
  if (store.assistantScope !== "SYSTEM") return;
  if (!store.assistant?.nextActions.includes("EDIT")) return;
  settingsBusy.value = true;
  settingsProblem.value = undefined;
  try {
    await platform.updateAssistantInstructions(assistantInstructions.value);
    await store.load(props.context, props.projectRef, false);
  } catch (error) {
    settingsProblem.value = error instanceof AppProblem ? error : undefined;
    if (!(error instanceof AppProblem)) throw error;
  } finally {
    settingsBusy.value = false;
  }
}

function scrollToLatest(): void {
  chatFollowing.value = true;
  chatUnread.value = false;
  chatLog.value?.scrollTo({ top: chatLog.value.scrollHeight });
}

function onChatScroll(): void {
  const log = chatLog.value;
  if (!log) return;
  chatFollowing.value = isTranscriptNearBottom(log);
  if (chatFollowing.value) chatUnread.value = false;
  else cancelLatestRestore();
}

function cancelLatestRestore(): void {
  if (latestRestoreTimer !== undefined) window.clearTimeout(latestRestoreTimer);
  latestRestoreTimer = undefined;
}

function restoreLatestAfterRender(): void {
  cancelLatestRestore();
  let remaining = 5;
  const advance = () => {
    if (!open.value || !chatLog.value || !chatFollowing.value) return;
    scrollToLatest();
    if (remaining-- > 0) latestRestoreTimer = window.setTimeout(advance, 160);
    else latestRestoreTimer = undefined;
  };
  advance();
}

function suggestSetup(prompt: string): void {
  if (!canSend.value || message.value.trim()) return;
  message.value = prompt;
  void nextTick(() => composer.value?.focus());
}
function prepareAssistantSettings(
  kind: "RUNTIME" | "ENVIRONMENT" | "IMAGE" | "INSTRUCTIONS",
): void {
  const agentRef = activeSettingsAgentRef.value;
  if (
    !agentRef ||
    !activeSettingsCanEdit.value ||
    !canSend.value ||
    message.value.trim()
  )
    return;
  const scope = store.assistantScope;
  const profile = store.projectAssistant;
  if (scope === "PROJECT" && (!profile || profile.agentRef !== agentRef))
    return;
  const prompt = t(`assistant.settings.preparePrompts.${kind}`, {
    agentRef,
    scope,
    projectRef: scope === "PROJECT" ? (profile?.projectRef ?? "") : "",
    profileRef: scope === "PROJECT" ? (profile?.ref ?? "") : "",
  });
  settingsOpen.value = false;
  suggestSetup(prompt);
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
  if (!(link instanceof HTMLAnchorElement)) return;
  const target = new URL(link.href, window.location.origin);
  const projectRef = /^\/projects\/([A-Za-z0-9_-]{8,96})(?:\/|$)/.exec(
    target.pathname,
  )?.[1];
  if (
    target.origin === window.location.origin &&
    projectRef &&
    store.selectedConversation?.projectRef === projectRef
  ) {
    persistAssistantConversationRef(
      projectRef,
      store.selectedConversation.ref,
      undefined,
      store.assistantScope,
    );
  }
  if (
    !props.projectRef &&
    store.selectedConversation?.state === "ACTIVE" &&
    !store.selectedConversation.projectRef &&
    target.origin === window.location.origin &&
    projectRef
  ) {
    event.preventDefault();
    projectMoveTrigger.value = link;
    pendingProjectMove.value = {
      projectRef,
      path: target.pathname + target.search + target.hash,
    };
    if (!platform.projects[projectRef]) void platform.loadProject(projectRef);
    void nextTick(() => projectMoveDialog.value?.focus());
    return;
  }
  if (link.getAttribute("href") !== "/configurations/INTEGRATION_DEFINITION")
    return;
  event.preventDefault();
  integrationImportOpen.value = true;
}

function cancelProjectMove(): void {
  pendingProjectMove.value = undefined;
  void nextTick(() => projectMoveTrigger.value?.focus());
}

async function confirmProjectMove(): Promise<void> {
  const target = pendingProjectMove.value;
  if (!target || !store.selectedConversation || store.busy) return;
  try {
    const moved = await store.moveSelectedToProject(target.projectRef);
    persistAssistantConversationRef(
      target.projectRef,
      moved.ref,
      undefined,
      store.assistantScope,
    );
    pendingProjectMove.value = undefined;
    await router.push(target.path);
  } catch (error) {
    if (!(error instanceof AppProblem)) throw error;
  }
}

function handleProjectMoveKeydown(event: KeyboardEvent): void {
  if (event.key === "Escape" && !store.busy) {
    event.preventDefault();
    cancelProjectMove();
  }
  if (event.key !== "Tab" || !projectMoveDialog.value) return;
  const target = trappedFocusTarget(
    focusableElements(projectMoveDialog.value),
    document.activeElement,
    event.shiftKey,
  );
  if (!target) return;
  event.preventDefault();
  target.focus();
}

function integrationDraftCreated(configurationRef: string): void {
  integrationImportOpen.value = false;
  createdDefinitionRef.value = configurationRef;
}

function openCreatedDefinition(): void {
  if (!createdDefinitionRef.value) return;
  const configurationRef = createdDefinitionRef.value;
  void router.push({
    name: "configuration",
    params: { kind: "INTEGRATION_DEFINITION", configurationRef },
    query: { assistantForm: "1" },
  });
}

function handleComposerKeydown(event: KeyboardEvent): void {
  if (event.key !== "Enter" || event.shiftKey) return;
  event.preventDefault();
  void send("QUEUE");
}

async function openPlan(plan: AssistantPlan, event: MouseEvent): Promise<void> {
  store.clearReceipt();
  planTrigger.value = event.currentTarget as HTMLButtonElement;
  openPlanRef.value = plan.ref;
  await nextTick();
  const initialTarget = planDialog.value
    ? focusableElements(planDialog.value)[0]
    : undefined;
  (initialTarget ?? planDialog.value)?.focus();
}

function planVariantNumber(planRef: string): number {
  const variants = (store.selectedConversation?.turns ?? []).flatMap((turn) =>
    turn.plan ? [turn.plan.ref] : [],
  );
  const index = variants.indexOf(planRef);
  return index >= 0 ? index + 1 : 1;
}

async function closePlan(): Promise<void> {
  if (store.busy) return;
  const trigger = planTrigger.value;
  openPlanRef.value = undefined;
  planTrigger.value = undefined;
  store.clearReceipt();
  await nextTick();
  scrollToLatest();
  if (trigger?.isConnected) trigger.focus();
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
      case "CREATE_PROJECT_FILE":
        kinds.add("ARTIFACT");
        break;
      case "CREATE_AGENT":
      case "UPDATE_AGENT":
      case "PREPARE_ASSISTANT_RUNTIME_CONFIGURATION":
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
      case "CREATE_SYSTEM_ASSISTANT_ROLE_IMAGE_RECIPE":
      case "UPDATE_SYSTEM_ASSISTANT_ROLE_IMAGE_RECIPE":
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
      case "PREPARE_PROJECT_ASSISTANT_INTEGRATION_CONNECTION":
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
  notifyAssistantPlanApplied({
    projectRef: plan.projectRef,
    kinds: [...kinds],
  });
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
      variant: planVariantNumber(plan.ref),
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
  if (
    openConversationMenu.value &&
    (!(event.target instanceof Element) ||
      !event.target.closest(".assistant-conversation-actions"))
  )
    openConversationMenu.value = undefined;
}

watch(contextIdentity, () => {
  pendingProjectMove.value = undefined;
  contextOpen.value = false;
  integrationImportOpen.value = false;
  createdDefinitionRef.value = undefined;
  openPlanRef.value = undefined;
  activeView.value = "CHAT";
  attachmentComposer.value?.clear();
  if (open.value) loadWorkspace();
  else store.setContext(props.context, props.projectRef);
});
watch(assistantFormActive, (active) => {
  if (active && !open.value) void show();
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
  () => store.loading,
  async (loading, wasLoading) => {
    if (loading) {
      const log = chatLog.value;
      followLatestAfterLoad =
        !log || log.scrollHeight - log.clientHeight - log.scrollTop < 80;
      return;
    }
    if (!wasLoading || !followLatestAfterLoad || !open.value) return;
    followLatestAfterLoad = false;
    await nextTick();
    await new Promise<void>((resolve) =>
      window.requestAnimationFrame(() => resolve()),
    );
    restoreLatestAfterRender();
  },
);
watch(
  chatLog,
  (element) => {
    chatResizeObserver?.disconnect();
    if (!element || typeof ResizeObserver === "undefined") return;
    chatResizeObserver ??= new ResizeObserver(() => {
      const log = chatLog.value;
      if (
        open.value &&
        log &&
        log.scrollHeight - log.clientHeight - log.scrollTop < 96
      ) {
        scrollToLatest();
      }
    });
    chatResizeObserver.observe(element);
  },
  { flush: "post" },
);
watch(
  currentDraftKey,
  (next, previous) => {
    if (previous) messageDrafts.set(previous, message.value);
    message.value = messageDrafts.get(next) ?? "";
  },
  { flush: "sync" },
);
watch(
  () => store.selectedConversation?.ref,
  async () => {
    chatFollowing.value = true;
    chatUnread.value = false;
    expandedHistoryMessages.value = {};
    if (store.selectedConversation?.ref)
      persistAssistantConversationRef(
        props.projectRef,
        store.selectedConversation.ref,
        undefined,
        store.assistantScope,
      );
    titleEditing.value = false;
    openPlanRef.value = undefined;
    store.clearReceipt();
    await nextTick();
    restoreLatestAfterRender();
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
  () => [
    store.selectedConversation?.turns.length,
    ...conversationRunEvents.value.map((event) => event.ref),
  ],
  async () => {
    if (!open.value || openPlanRef.value) return;
    if (!chatFollowing.value) {
      chatUnread.value = true;
      return;
    }
    await nextTick();
    restoreLatestAfterRender();
  },
);
watch(
  [
    () => props.projectRef,
    () => route.params.projectRef,
    () => route.query.assistantCreateSecret,
    () => route.query.assistantSecretDraftRef,
  ],
  () => {
    if (
      workspaceMounted &&
      (route.query.assistantCreateSecret || route.query.assistantSecretDraftRef)
    )
      void resumeAssistantSecretForm();
  },
);

watch(
  [
    open,
    () => props.live,
    () => store.selectedConversation?.ref,
    conversationRunRefs,
  ],
  async () => {
    const generation = ++transcriptReadGeneration;
    const wanted =
      open.value && props.live
        ? new Set(conversationRunRefs.value)
        : new Set<string>();
    for (const [runRef, release] of transcriptLeases) {
      if (wanted.has(runRef)) continue;
      release();
      transcriptLeases.delete(runRef);
    }
    // Общий owner-checked history path; чтения пока последовательны.
    for (const runRef of wanted) {
      if (transcriptLeases.has(runRef)) continue;
      await platform.loadRun(runRef);
      if (generation !== transcriptReadGeneration) return;
      if (!platform.runProblems[runRef] && platform.graphs[runRef])
        transcriptLeases.set(runRef, realtime.acquireRun(runRef));
    }
  },
  { immediate: true },
);

onMounted(() => {
  workspaceMounted = true;
  historyMedia?.addEventListener("change", syncHistoryViewport);
  document.addEventListener("pointerdown", documentPointerDown);
  window.addEventListener(openAssistantEvent, handleOpenAssistant);
  if (route.query.assistantCreateSecret || route.query.assistantSecretDraftRef)
    void resumeAssistantSecretForm();
  else if (assistantFormActive.value) void show();
  else if (open.value) void show();
});
onBeforeUnmount(() => {
  ++transcriptReadGeneration;
  for (const release of transcriptLeases.values()) release();
  transcriptLeases.clear();
  workspaceMounted = false;
  cancelLatestRestore();
  chatResizeObserver?.disconnect();
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
    role="presentation"
    :inert="
      integrationImportOpen ||
      secretDialogOpen ||
      Boolean(credentialConnectionRef)
    "
  >
    <button
      class="assistant-overlay__backdrop"
      type="button"
      :aria-label="$t('common.close')"
      :disabled="store.busy"
      @click="close"
    />
    <aside
      key="CHAT"
      id="assistant-workspace"
      ref="panel"
      class="assistant-drawer"
      role="dialog"
      :aria-modal="
        !currentPlan && !assistantFormActive && !pendingProjectMove
          ? true
          : undefined
      "
      :aria-label="$t('assistant.title')"
      :aria-busy="store.busy || store.loading"
      :inert="
        Boolean(currentPlan) ||
        assistantFormActive ||
        Boolean(pendingProjectMove) ||
        undefined
      "
      :data-conversation-ref="store.selectedConversation?.ref"
      tabindex="-1"
      @keydown="handleKeydown"
    >
      <header class="assistant-drawer__header">
        <span class="assistant-drawer__mark" aria-hidden="true">
          <Bot :size="21" />
        </span>
        <div class="assistant-drawer__identity">
          <strong>{{
            store.assistantScope === "PROJECT"
              ? (store.projectAssistant?.name ??
                $t("assistant.projectProfile.title"))
              : "Kodex"
          }}</strong>
          <span>{{ contextTitle }}</span>
        </div>
        <label v-if="projectRef" class="assistant-scope-selector">
          <span class="sr-only">{{
            $t("assistant.projectProfile.scopeLabel")
          }}</span>
          <select
            :value="store.assistantScope"
            :disabled="store.busy || store.loading"
            @change="selectAssistantScope"
          >
            <option value="SYSTEM">
              {{ $t("assistant.settings.systemScope") }}
            </option>
            <option value="PROJECT">
              {{ $t("assistant.settings.projectScope") }}
            </option>
          </select>
        </label>
        <StatusBadge
          v-if="activeSettingsAgentRef"
          :state="live ? assistantRuntimeState : 'RECOVERING'"
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
                  conversationDisplayTitle(conversation.title, conversation.ref)
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
            <span v-if="store.loadingMore" role="status">{{
              $t("common.loading")
            }}</span>
          </section>
        </div>
        <button
          v-if="activeSettingsAgentRef"
          class="icon-button"
          type="button"
          :aria-label="$t('assistant.settings.title')"
          :title="$t('assistant.settings.title')"
          :disabled="store.busy"
          @click="openAssistantSettings"
        >
          <Settings :size="19" aria-hidden="true" />
        </button>
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
          v-if="
            store.historyState === 'ARCHIVED' &&
            store.sortedConversations.length
          "
          class="button assistant-empty-trash"
          type="button"
          :disabled="store.busy || store.loading"
          @click="emptyConversationTrash"
        >
          <Trash2 :size="16" aria-hidden="true" />
          {{ $t("assistant.emptyTrash") }}
        </button>
        <p
          v-if="
            store.historyState === 'ARCHIVED' &&
            !store.loading &&
            !store.sortedConversations.length
          "
          class="assistant-conversation-sidebar__empty"
        >
          {{ $t("assistant.trashEmpty") }}
        </p>
        <div
          v-for="conversation in store.sortedConversations"
          :key="conversation.ref"
          :data-conversation-ref="conversation.ref"
          class="assistant-conversation-entry"
          :class="{ selected: conversation.ref === store.selectedRef }"
        >
          <button
            class="assistant-conversation-entry__select"
            type="button"
            @click="chooseConversation(conversation.ref)"
          >
            <strong>{{
              conversationDisplayTitle(conversation.title, conversation.ref)
            }}</strong>
            <time :datetime="conversation.updatedAt">{{
              new Date(conversation.updatedAt).toLocaleString()
            }}</time>
          </button>
          <div class="assistant-conversation-actions">
            <button
              class="icon-button assistant-conversation-actions__toggle"
              type="button"
              :aria-label="$t('assistant.conversationActions')"
              :aria-expanded="openConversationMenu === conversation.ref"
              aria-haspopup="menu"
              :disabled="store.busy"
              @click="toggleConversationMenu(conversation.ref)"
            >
              <EllipsisVertical :size="18" aria-hidden="true" />
            </button>
            <div
              v-if="openConversationMenu === conversation.ref"
              class="assistant-conversation-actions__menu"
              role="menu"
            >
              <template v-if="conversation.state === 'ARCHIVED'">
                <button
                  type="button"
                  role="menuitem"
                  @click="restoreConversationFromTrash(conversation)"
                >
                  <RotateCcw :size="16" aria-hidden="true" />
                  {{ $t("assistant.restoreConversation") }}
                </button>
                <button
                  class="danger"
                  type="button"
                  role="menuitem"
                  @click="purgeConversationFromTrash(conversation)"
                >
                  <Trash2 :size="16" aria-hidden="true" />
                  {{ $t("assistant.purgeConversation") }}
                </button>
              </template>
              <button
                v-else
                class="danger"
                type="button"
                role="menuitem"
                @click="moveConversationToTrash(conversation)"
              >
                <Trash2 :size="16" aria-hidden="true" />
                {{ $t("assistant.deleteConversation") }}
              </button>
            </div>
          </div>
        </div>
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
        <span v-if="store.loadingMore" role="status">{{
          $t("common.loading")
        }}</span>
      </nav>

      <div class="assistant-workspace-content">
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
          <AssistantProjectProfileSetup
            v-if="
              store.assistantScope === 'PROJECT' &&
              !store.projectAssistant &&
              !store.loading &&
              !store.problem
            "
            v-model:name="projectProfileName"
            v-model:purpose="projectProfilePurpose"
            v-model:instructions="projectProfileInstructions"
            :busy="store.busy || !canCreateProjectProfile"
            @create="createProjectProfile"
          />
          <RunActivityView
            v-else-if="activeView === 'ACTIVITY'"
            :events="runEvents"
          />
          <div v-else class="assistant-chat-view">
            <p
              v-if="
                store.assistantScope === 'PROJECT' &&
                store.projectAssistant &&
                !projectAssistantCanRun
              "
              class="assistant-project-profile"
            >
              {{ $t("assistant.projectProfile.needsSetup") }}
              <button
                class="button"
                type="button"
                @click="openAssistantSettings"
              >
                {{ $t("assistant.projectProfile.configure") }}
              </button>
            </p>
            <button
              type="button"
              class="assistant-context-strip"
              :aria-label="$t('assistant.context')"
              :aria-expanded="contextOpen"
              @click="contextOpen = true"
            >
              <span>{{ $t("assistant.context") }}</span>
              <strong>{{ contextTitle }}</strong>
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
                  :id="titleFieldName"
                  :name="titleFieldName"
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
                  conversationDisplayTitle(
                    store.selectedConversation.title,
                    store.selectedConversation.ref,
                  )
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
                  :aria-label="$t('assistant.deleteConversation')"
                  :title="$t('assistant.deleteConversation')"
                  @click="archiveSelected"
                >
                  <Trash2 :size="16" />
                </button>
                <StatusBadge v-else :state="store.selectedConversation.state" />
              </template>
            </section>

            <section
              ref="chatLog"
              class="assistant-chat-log"
              role="log"
              aria-live="polite"
              @wheel.passive="cancelLatestRestore"
              @touchstart.passive="cancelLatestRestore"
              @scroll.passive="onChatScroll"
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
              <template v-else>
                <template v-for="entry in chatTimeline" :key="entry.id">
                  <RunActivityView
                    v-if="entry.kind === 'ACTIVITY'"
                    :events="entry.events"
                    :closed-execution-keys="closedTranscriptExecutionKeys"
                    :active-item-id="entry.isolated ? null : chatActiveItemId"
                    embedded
                  />
                  <template v-else v-for="turn in [entry.turn]" :key="turn.ref"
                    ><article
                      class="assistant-message"
                      :class="[
                        `assistant-message--${turn.role.toLowerCase()}`,
                        { 'assistant-message--with-plan': Boolean(turn.plan) },
                        {
                          'assistant-message--applied-plan':
                            turn.plan?.state === 'APPLIED',
                        },
                      ]"
                      :data-turn-ref="turn.ref"
                      :data-turn-sequence="turn.sequence"
                      @click.capture="handleAssistantLink"
                    >
                      <header
                        v-if="
                          !turnHasPublishedMessage(turn) &&
                          turn.plan?.state !== 'APPLIED'
                        "
                      >
                        <strong>{{
                          turn.role === "USER"
                            ? $t("common.input")
                            : turn.role === "SYSTEM_RECEIPT"
                              ? $t("assistant.receipt")
                              : "Kodex"
                        }}</strong>
                        <StatusBadge :state="turn.state" />
                        <RouterLink
                          v-if="turn.runRef"
                          class="button assistant-message__run-link"
                          :to="
                            runPath(
                              turn.runRef,
                              store.selectedConversation?.projectRef,
                            )
                          "
                          @click.prevent="openAssistantTurnRun(turn.runRef)"
                        >
                          {{ $t("assistant.launchedRun.open") }}
                        </RouterLink>
                      </header>
                      <SafeMarkdown
                        v-if="
                          !turnHasPublishedMessage(turn) &&
                          turn.plan?.state !== 'APPLIED'
                        "
                        :content="transcriptTurnContent(turn)"
                        :class="{
                          'assistant-transcript-message--collapsed':
                            turn.content.length > 1200 &&
                            !expandedHistoryMessages[turn.ref],
                        }"
                      />
                      <button
                        v-if="
                          !turnHasPublishedMessage(turn) &&
                          turn.plan?.state !== 'APPLIED' &&
                          turn.content.length > 1200
                        "
                        type="button"
                        class="button button--ghost"
                        :aria-expanded="
                          Boolean(expandedHistoryMessages[turn.ref])
                        "
                        @click="
                          expandedHistoryMessages[turn.ref] =
                            !expandedHistoryMessages[turn.ref]
                        "
                      >
                        {{
                          $t(
                            expandedHistoryMessages[turn.ref]
                              ? "runs.collapseMessage"
                              : "runs.expandMessage",
                          )
                        }}
                      </button>
                      <AssistantPlanRecord
                        v-if="turn.plan"
                        :plan="turn.plan"
                        :variant="planVariantNumber(turn.plan.ref)"
                      >
                        <SafeMarkdown
                          v-if="
                            turn.plan.state === 'APPLIED' &&
                            !turnHasPublishedMessage(turn)
                          "
                          :content="transcriptTurnContent(turn)"
                        />
                        <header>
                          <ListChecks :size="19" aria-hidden="true" />
                          <div>
                            <strong>{{
                              $t("assistant.planVariant", {
                                variant: planVariantNumber(turn.plan.ref),
                              })
                            }}</strong>
                            <span>{{
                              $t("assistant.planEditor.revision", {
                                revision: turn.plan.revision,
                                count: turn.plan.operations.length,
                              })
                            }}</span>
                          </div>
                          <StatusBadge :state="turn.plan.state" />
                        </header>
                        <SafeMarkdown
                          v-if="
                            turn.plan.auditSummary.trim() !==
                            turn.content.trim()
                          "
                          :content="turn.plan.auditSummary"
                        />
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
                              <small>{{
                                operationTargetKindLabel(operation.target.kind)
                              }}</small>
                            </header>
                            <strong class="assistant-plan-card__target">
                              {{ operationTargetLabel(operation.target) }}
                            </strong>
                            <span v-if="operationSupportingTitle(operation)">{{
                              operationSupportingTitle(operation)
                            }}</span>
                            <p>{{ operation.summary }}</p>
                          </li>
                        </ol>
                        <AssistantRoleImageBuildCard
                          v-for="operation in turn.plan.operations.filter(
                            (item) =>
                              item.type === 'CREATE_ROLE_IMAGE_RECIPE' ||
                              item.type === 'UPDATE_ROLE_IMAGE_RECIPE' ||
                              item.type ===
                                'CREATE_SYSTEM_ASSISTANT_ROLE_IMAGE_RECIPE' ||
                              item.type ===
                                'UPDATE_SYSTEM_ASSISTANT_ROLE_IMAGE_RECIPE',
                          )"
                          :key="`build-${operation.ref}`"
                          :plan="turn.plan"
                          :operation-ref="operation.ref"
                          @debug="suggestSetup"
                        />
                        <AssistantCreatedEntityCard
                          v-for="operation in turn.plan.operations.filter(
                            (item) =>
                              item.type === 'CREATE_PROJECT' ||
                              item.type === 'UPDATE_PROJECT' ||
                              item.type === 'CREATE_AGENT' ||
                              item.type === 'CREATE_PROJECT_ASSISTANT' ||
                              item.type === 'UPDATE_AGENT',
                          )"
                          :key="`entity-${operation.ref}`"
                          :plan="turn.plan"
                          :operation-ref="operation.ref"
                        />
                        <AssistantCreatedProjectFileCard
                          v-for="operation in turn.plan.operations.filter(
                            (item) => item.type === 'CREATE_PROJECT_FILE',
                          )"
                          :key="`file-${operation.ref}`"
                          :plan="turn.plan"
                          :operation-ref="operation.ref"
                        />
                        <AssistantInstructionDraftCard
                          v-for="operation in turn.plan.operations.filter(
                            (item) => item.type === 'CREATE_INSTRUCTION_DRAFT',
                          )"
                          :key="`instruction-${operation.ref}`"
                          :plan="turn.plan"
                          :operation-ref="operation.ref"
                        />
                        <AssistantEnvironmentDraftCard
                          v-for="operation in turn.plan.operations.filter(
                            (item) =>
                              item.type ===
                                'CREATE_RUNTIME_ENVIRONMENT_DRAFT' ||
                              item.type ===
                                'PREPARE_RUNTIME_ENVIRONMENT_REVISION',
                          )"
                          :key="`environment-${operation.ref}`"
                          :plan="turn.plan"
                          :operation-ref="operation.ref"
                        />
                        <AssistantAgentEnvironmentBindingCard
                          v-for="operation in turn.plan.operations.filter(
                            (item) =>
                              item.type === 'BIND_AGENT_RUNTIME_ENVIRONMENT',
                          )"
                          :key="`binding-${operation.ref}`"
                          :plan="turn.plan"
                          :operation-ref="operation.ref"
                        />
                        <AssistantIntegrationConnectionCard
                          v-for="operation in turn.plan.operations.filter(
                            (item) =>
                              item.type === 'CREATE_INTEGRATION_CONNECTION' ||
                              item.type ===
                                'PREPARE_PROJECT_ASSISTANT_INTEGRATION_CONNECTION' ||
                              item.type === 'UPDATE_INTEGRATION_CONNECTION' ||
                              item.type === 'TEST_INTEGRATION_CONNECTION',
                          )"
                          :key="`connection-${operation.ref}`"
                          :plan="turn.plan"
                          :operation-ref="operation.ref"
                          :refresh-token="connectionRefreshToken"
                          @prepare-credential="credentialConnectionRef = $event"
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
                          @click="openPlan(turn.plan, $event)"
                        >
                          {{
                            ["APPLIED", "REJECTED"].includes(turn.plan.state)
                              ? $t("assistant.viewPlan")
                              : $t("assistant.openPlan")
                          }}
                        </button>
                      </AssistantPlanRecord>
                    </article></template
                  >
                </template>
              </template>
              <div
                v-if="showWorkingFallback && !store.loading && !store.problem"
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
              <button
                v-if="chatUnread"
                class="button"
                type="button"
                @click="scrollToLatest"
              >
                {{ $t("runs.newMessages") }}
              </button>
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
              <div
                class="assistant-composer__field"
                :class="{
                  'assistant-composer__field--active': awaitingReply,
                }"
              >
                <VoiceTextarea
                  ref="composer"
                  v-model="message"
                  name="assistant-message"
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
                    v-if="awaitingReply"
                    class="assistant-composer__send assistant-composer__send--stop"
                    type="button"
                    :aria-label="$t('assistant.stop')"
                    :disabled="store.busy"
                    :title="$t('assistant.stop')"
                    @click="stopActiveTurn"
                  >
                    <Square :size="17" fill="currentColor" aria-hidden="true" />
                  </button>
                  <button
                    class="assistant-composer__send"
                    type="button"
                    :aria-label="
                      awaitingReply
                        ? $t('assistant.queue')
                        : $t('assistant.send')
                    "
                    :disabled="!canSend || !message.trim()"
                    :title="
                      awaitingReply
                        ? $t('assistant.queue')
                        : $t('assistant.send')
                    "
                    @click="send('QUEUE')"
                  >
                    <Send v-if="!awaitingReply" :size="19" aria-hidden="true" />
                    <ArrowUp
                      v-else
                      :size="20"
                      stroke-width="2.5"
                      aria-hidden="true"
                    />
                  </button>
                  <button
                    v-if="awaitingReply"
                    class="assistant-composer__send assistant-composer__send--immediate"
                    type="button"
                    :aria-label="$t('assistant.sendNow')"
                    :disabled="!canSend || !message.trim()"
                    :title="$t('assistant.sendNow')"
                    @click="send('INTERRUPT_ACTIVE')"
                  >
                    <Zap :size="19" fill="currentColor" aria-hidden="true" />
                  </button>
                </div>
              </div>
              <div class="assistant-composer__meta">
                <button
                  v-if="projectRef"
                  class="assistant-composer__protected-link"
                  type="button"
                  @click="openPlainSecretForm"
                >
                  <KeyRound :size="15" aria-hidden="true" />
                  {{ $t("assistant.openSecretForm") }}
                </button>
                <small>{{ $t("assistant.audit") }}</small>
              </div>
            </footer>
          </div>
        </div>
      </div>
    </aside>
    <div v-if="pendingProjectMove" class="assistant-project-move-layer">
      <button
        class="assistant-project-move-layer__backdrop"
        type="button"
        :aria-label="$t('common.close')"
        :disabled="store.busy"
        @click="cancelProjectMove"
      />
      <section
        ref="projectMoveDialog"
        class="assistant-project-move-dialog"
        role="dialog"
        aria-modal="true"
        :aria-label="$t('assistant.projectMove.title')"
        tabindex="-1"
        @keydown="handleProjectMoveKeydown"
      >
        <h2>{{ $t("assistant.projectMove.title") }}</h2>
        <p>{{ $t("assistant.projectMove.description") }}</p>
        <p class="assistant-project-move-dialog__destination">
          {{ $t("assistant.projectMove.destination") }}
          <strong>{{
            projectMoveDestination || $t("assistant.projectMove.loadingProject")
          }}</strong>
        </p>
        <ProblemNotice v-if="store.problem" :problem="store.problem" />
        <footer>
          <button
            class="button"
            type="button"
            :disabled="store.busy"
            @click="cancelProjectMove"
          >
            {{ $t("common.cancel") }}
          </button>
          <button
            class="button button--primary"
            type="button"
            :disabled="store.busy"
            @click="confirmProjectMove"
          >
            {{ $t("assistant.projectMove.confirm") }}
          </button>
        </footer>
      </section>
    </div>
    <button
      v-if="currentPlan && !assistantFormActive"
      class="assistant-detail-backdrop"
      type="button"
      :aria-label="$t('assistant.planEditor.back')"
      :disabled="store.busy"
      @click="closePlan"
    />
    <section
      v-if="currentPlan && !assistantFormActive"
      ref="planDialog"
      class="assistant-plan-dialog"
      role="dialog"
      aria-modal="true"
      :aria-label="$t('assistant.plan')"
      tabindex="-1"
    >
      <AssistantPlanEditor
        :plan="currentPlan"
        :variant="planVariantNumber(currentPlan.ref)"
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
    </section>
    <button
      v-if="assistantFormActive"
      class="assistant-detail-backdrop"
      type="button"
      :aria-label="$t('common.close')"
      @click="closeAssistantForm"
    />
  </div>
  <Teleport to="body">
    <div v-if="open && settingsOpen" class="assistant-settings-layer">
      <button
        class="assistant-settings-layer__backdrop"
        type="button"
        :aria-label="$t('common.close')"
        @click="settingsOpen = false"
      />
      <section
        class="assistant-settings-dialog"
        role="dialog"
        aria-modal="true"
        :aria-label="$t('assistant.settings.title')"
      >
        <header>
          <div>
            <h2>{{ $t("assistant.settings.title") }}</h2>
            <p>{{ $t("assistant.settings.description") }}</p>
          </div>
          <button
            class="icon-button"
            type="button"
            :aria-label="$t('common.close')"
            @click="settingsOpen = false"
          >
            <X :size="20" aria-hidden="true" />
          </button>
        </header>
        <nav class="assistant-settings-dialog__tabs" aria-label="">
          <button
            type="button"
            :class="{ selected: settingsTab === 'RUNTIME' }"
            @click="settingsTab = 'RUNTIME'"
          >
            {{ $t("assistant.settings.runtime") }}
          </button>
          <button
            type="button"
            :class="{ selected: settingsTab === 'ENVIRONMENT' }"
            @click="settingsTab = 'ENVIRONMENT'"
          >
            {{ $t("assistant.settings.environment") }}
          </button>
          <button
            type="button"
            :class="{ selected: settingsTab === 'INSTRUCTIONS' }"
            @click="settingsTab = 'INSTRUCTIONS'"
          >
            {{ $t("assistant.settings.instructions") }}
          </button>
          <button
            v-if="store.assistantScope === 'SYSTEM'"
            type="button"
            :class="{ selected: settingsTab === 'INTEGRATIONS' }"
            @click="settingsTab = 'INTEGRATIONS'"
          >
            {{ $t("assistant.settings.integrations") }}
          </button>
        </nav>
        <div class="assistant-settings-dialog__body">
          <section
            v-if="settingsTab !== 'INTEGRATIONS'"
            class="assistant-settings-dialog__prepare"
          >
            <p>{{ $t("assistant.settings.prepareHelp") }}</p>
            <button
              class="button"
              type="button"
              :disabled="
                !activeSettingsCanEdit || !canSend || Boolean(message.trim())
              "
              @click="prepareAssistantSettings(settingsTab)"
            >
              <Sparkles :size="16" />{{ $t("assistant.settings.prepare") }}
            </button>
            <button
              v-if="settingsTab === 'ENVIRONMENT'"
              class="button"
              type="button"
              :disabled="
                !activeSettingsCanEdit || !canSend || Boolean(message.trim())
              "
              @click="prepareAssistantSettings('IMAGE')"
            >
              <Sparkles :size="16" />{{ $t("assistant.settings.prepareImage") }}
            </button>
          </section>
          <SystemAssistantIntegrationGrantsPanel
            v-if="
              settingsTab === 'INTEGRATIONS' &&
              store.assistantScope === 'SYSTEM' &&
              store.assistant
            "
            :assistant="store.assistant"
            :can-edit="store.assistant.nextActions.includes('EDIT')"
          />
          <AgentRuntimePanel
            v-else-if="settingsTab === 'RUNTIME' && activeSettingsAgentRef"
            :agent-ref="activeSettingsAgentRef"
            :can-edit="activeSettingsCanEdit"
            advanced-collapsed
          />
          <section
            v-else-if="store.assistantScope === 'PROJECT'"
            class="assistant-settings-dialog__instructions"
          >
            <p>
              {{
                $t(
                  settingsTab === "ENVIRONMENT"
                    ? "assistant.projectProfile.environmentHelp"
                    : "assistant.projectProfile.instructionsHelp",
                )
              }}
            </p>
            <button
              class="button button--primary"
              type="button"
              @click="
                openProjectAssistantEditor(
                  settingsTab === 'ENVIRONMENT'
                    ? 'environment'
                    : 'instructions',
                )
              "
            >
              {{ $t("assistant.projectProfile.openEditor") }}
            </button>
          </section>
          <AssistantEnvironmentSettingsPanel
            v-else-if="
              settingsTab === 'ENVIRONMENT' &&
              store.assistant &&
              systemResourceScope
            "
            :agent-ref="store.assistant.ref"
            :can-edit="store.assistant.nextActions.includes('EDIT')"
            :resource-scope="systemResourceScope"
            :return-to="route.fullPath"
            :image-catalog="systemResourceCatalogs?.images"
            :secret-catalog="systemResourceCatalogs?.secrets"
          />
          <section v-else-if="settingsTab === 'ENVIRONMENT'" role="status">
            {{ $t("common.loading") }}
          </section>
          <section v-else class="assistant-settings-dialog__instructions">
            <p>{{ $t("assistant.settings.instructionsHelp") }}</p>
            <VoiceTextarea
              v-model="assistantInstructions"
              rows="8"
              maxlength="32768"
              :disabled="settingsBusy"
            />
            <button
              class="button button--primary"
              type="button"
              :disabled="
                settingsBusy || !store.assistant?.nextActions.includes('EDIT')
              "
              @click="saveAssistantInstructions"
            >
              {{ $t("common.save") }}
            </button>
            <ProblemNotice
              v-if="settingsProblem"
              :problem="settingsProblem"
              compact
            />
          </section>
        </div>
      </section>
    </div>
  </Teleport>
  <section
    v-show="open && assistantFormActive"
    id="assistant-form-slot"
    ref="formSlot"
    class="assistant-form-slot"
    role="dialog"
    aria-modal="true"
    :aria-label="$t('assistant.planEditor.parametersTitle')"
    :inert="
      integrationImportOpen ||
      secretDialogOpen ||
      Boolean(credentialConnectionRef) ||
      undefined
    "
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
  <OpenAPIImportDialog
    v-if="open && integrationImportOpen"
    @close="integrationImportOpen = false"
    @created="integrationDraftCreated"
  />
  <Teleport to="body">
    <div
      v-if="open && credentialConnectionRef"
      class="assistant-credential-layer"
    >
      <AssistantIntegrationCredentialDialog
        :connection-ref="credentialConnectionRef"
        @close="credentialConnectionRef = ''"
        @configured="connectionRefreshToken += 1"
      />
    </div>
  </Teleport>
  <Teleport to="body">
    <div
      v-if="open && secretDialogOpen && projectRef"
      class="assistant-secret-layer"
    >
      <RuntimeSecretDraftDialog
        :project-ref="projectRef"
        :initial-draft-ref="secretInitialDraftRef"
        :suggestion="secretSuggestion"
        :assistant-return-path="route.fullPath"
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
.assistant-scope-selector {
  flex: 0 1 240px;
  min-width: 0;
}
.assistant-scope-selector select {
  width: 100%;
  height: 32px;
}
.assistant-project-profile {
  display: grid;
  gap: 12px;
  padding: 12px;
}
.assistant-project-profile h3,
.assistant-project-profile p {
  margin: 0;
}
.assistant-project-profile label {
  display: grid;
  gap: 6px;
}
.assistant-secret-layer {
  position: fixed;
  z-index: 90;
  inset: 0;
  pointer-events: none;
}
.assistant-settings-layer {
  position: fixed;
  z-index: 95;
  inset: 0;
  display: grid;
  place-items: center;
  padding: 32px;
}
.assistant-settings-layer__backdrop {
  position: absolute;
  inset: 0;
  border: 0;
  background: rgb(17 24 39 / 38%);
}
.assistant-settings-dialog {
  position: relative;
  display: grid;
  grid-template-rows: auto auto minmax(0, 1fr);
  width: min(1180px, 94vw);
  height: min(840px, 92dvh);
  border: 1px solid var(--border);
  border-radius: 14px;
  background: var(--surface);
  box-shadow: 0 24px 72px rgb(15 23 42 / 30%);
  overflow: hidden;
}
.assistant-settings-dialog > header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 20px;
  padding: 20px 24px 16px;
  border-bottom: 1px solid var(--border);
}
.assistant-settings-dialog h2,
.assistant-settings-dialog p {
  margin: 0;
}
.assistant-settings-dialog header p {
  margin-top: 4px;
  color: var(--muted);
}
.assistant-settings-dialog__tabs {
  display: flex;
  gap: 4px;
  padding: 8px 24px 0;
  border-bottom: 1px solid var(--border);
}
.assistant-settings-dialog__tabs button {
  padding: 10px 14px;
  border: 0;
  border-bottom: 2px solid transparent;
  background: transparent;
  color: var(--muted);
  cursor: pointer;
}
.assistant-settings-dialog__tabs button.selected {
  border-color: var(--accent);
  color: var(--accent-strong);
  font-weight: 700;
}
.assistant-settings-dialog__body {
  min-height: 0;
  padding: 20px 24px 28px;
  overflow: auto;
}
.assistant-settings-dialog__prepare {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
  margin-bottom: 16px;
}
.assistant-settings-dialog__prepare p {
  flex: 1 1 100%;
}
.assistant-settings-dialog__instructions {
  display: grid;
  gap: 14px;
}
.assistant-settings-dialog__instructions .button {
  justify-self: start;
}
.assistant-credential-layer {
  position: fixed;
  z-index: 90;
  inset: 0;
  pointer-events: none;
}
.assistant-credential-layer :deep(.modal-backdrop) {
  pointer-events: auto;
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
/* Контекстная панель и модалка не должны делить кнопки с помощником. */
:global(body:has([aria-modal="true"], .run-activity-overlay) .assistant-fab) {
  visibility: hidden;
  pointer-events: none;
}
.assistant-overlay {
  position: fixed;
  z-index: 70;
  inset: 0;
}
.assistant-project-move-layer {
  position: fixed;
  z-index: 3;
  inset: 0;
  display: grid;
  place-items: center;
  padding: 24px;
}
.assistant-project-move-layer__backdrop {
  position: absolute;
  inset: 0;
  border: 0;
  background: rgb(17 24 39 / 34%);
}
.assistant-project-move-dialog {
  position: relative;
  width: min(100%, 560px);
  padding: 24px;
  border: 1px solid var(--border);
  border-radius: 12px;
  background: var(--surface);
  box-shadow: 0 24px 64px rgb(15 23 42 / 28%);
  outline: 0;
}
.assistant-project-move-dialog h2 {
  margin: 0 0 12px;
  font-size: 20px;
}
.assistant-project-move-dialog p {
  margin: 0 0 16px;
  line-height: 1.5;
}
.assistant-project-move-dialog__destination {
  color: var(--muted);
}
.assistant-project-move-dialog__destination code {
  overflow-wrap: anywhere;
}
.assistant-project-move-dialog footer {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
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
    position: relative;
    display: grid;
    grid-template-columns: minmax(0, 1fr) 32px;
    align-items: start;
    border: 0;
    border-radius: 6px;
    background: transparent;
    color: var(--text);
  }
  .assistant-conversation-entry.selected {
    background: var(--accent-soft);
  }
  .assistant-conversation-entry__select {
    display: grid;
    gap: 6px;
    min-width: 0;
    padding: 12px 4px 12px 12px;
    border: 0;
    text-align: left;
    background: transparent;
    color: inherit;
    cursor: pointer;
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
  .assistant-conversation-actions {
    position: relative;
    padding-top: 7px;
  }
  .assistant-conversation-actions__toggle {
    width: 32px;
    height: 32px;
    opacity: 0.55;
  }
  .assistant-conversation-entry:hover .assistant-conversation-actions__toggle,
  .assistant-conversation-actions__toggle[aria-expanded="true"],
  .assistant-conversation-actions__toggle:focus-visible {
    opacity: 1;
  }
  .assistant-conversation-actions__menu {
    position: absolute;
    z-index: 12;
    top: 36px;
    right: 0;
    width: max-content;
    min-width: 190px;
    padding: 6px;
    border: 1px solid var(--border);
    border-radius: 8px;
    background: var(--surface);
    box-shadow: var(--shadow-lg);
  }
  .assistant-conversation-actions__menu button {
    display: flex;
    width: 100%;
    align-items: center;
    gap: 8px;
    padding: 8px 10px;
    border: 0;
    border-radius: 6px;
    text-align: left;
    background: transparent;
    color: var(--text);
    white-space: nowrap;
    cursor: pointer;
  }
  .assistant-conversation-actions__menu button:hover,
  .assistant-conversation-actions__menu button:focus-visible {
    background: var(--accent-soft);
  }
  .assistant-conversation-actions__menu button.danger {
    color: var(--danger, #b42318);
  }
  .assistant-empty-trash {
    justify-content: center;
  }
  .assistant-conversation-sidebar__empty {
    margin: 10px 0 0;
    color: var(--text-muted);
    font-size: 13px;
    text-align: center;
  }
}
.assistant-drawer > .assistant-plan-editor,
.assistant-workspace-content,
.assistant-drawer__view,
.assistant-chat-view {
  min-width: 0;
  min-height: 0;
  flex: 1 1 auto;
}
.assistant-workspace-content {
  display: flex;
  flex-direction: column;
  overflow: hidden;
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
.assistant-drawer__identity strong {
  overflow: hidden;
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
  min-height: 32px;
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
.assistant-transcript-message--collapsed {
  display: -webkit-box;
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 8;
  overflow: hidden;
}
.assistant-message {
  box-sizing: border-box;
  width: min(86%, 760px);
  min-width: 0;
  max-width: 100%;
  margin-left: 0;
  margin-right: auto;
  margin-bottom: 14px;
  padding: 12px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--surface);
  overflow-wrap: anywhere;
}
.assistant-message--user {
  margin-left: auto;
  margin-right: 0;
  border-color: var(--accent);
  background: var(--accent-soft);
}
.assistant-message--system_receipt {
  background: var(--panel);
}
.assistant-message--with-plan {
  width: min(96%, 1180px);
}
.assistant-message--applied-plan {
  padding: 0;
  border: 0;
  background: transparent;
}
.assistant-message--typing {
  display: flex;
  align-items: center;
  gap: 10px;
  color: var(--muted);
  padding: 6px 10px;
  border: 0;
  background: transparent;
  font-size: 0.85rem;
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
.assistant-message > header {
  flex-wrap: wrap;
}
.assistant-message__run-link {
  margin-left: auto;
  flex-shrink: 0;
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
  padding: 11px 104px 54px 12px;
  border: 1px solid var(--border-strong);
  border-radius: 10px;
}
.assistant-composer :deep(.voice-textarea__action) {
  right: 58px;
}
.assistant-composer__field--active :deep(.voice-textarea__action) {
  right: 150px;
}
.assistant-composer__field--active :deep(textarea) {
  padding-right: 198px;
}
.assistant-composer :deep(.voice-input[data-state="recording"]) {
  padding-left: 8px;
  border-radius: 24px;
  background: var(--surface);
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
.assistant-composer__send--stop {
  border: 1px solid var(--danger);
  background: var(--danger-soft);
  color: var(--danger);
}
.assistant-composer__send--immediate {
  background: var(--warning);
  color: var(--text);
}
.assistant-composer__meta {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 6px 16px;
  min-width: 0;
}
.assistant-composer__meta > small {
  color: var(--subtle);
}
.assistant-composer__protected-link {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  max-width: 100%;
  padding: 6px 10px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--surface);
  color: var(--accent-strong);
  font-size: 0.82rem;
  font-weight: 600;
  line-height: 1.2;
  text-align: left;
  cursor: pointer;
}
.assistant-composer__protected-link:hover {
  border-color: var(--accent);
  background: var(--panel);
}
.assistant-composer__protected-link:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: 2px;
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
    flex: 0 0 auto;
    flex-wrap: wrap;
    height: auto;
    gap: 8px;
    padding-top: 14px;
  }
  .assistant-drawer__identity {
    flex: 1 1 0;
  }
  .assistant-scope-selector {
    order: 1;
    flex: 1 0 100%;
  }
  .assistant-new-conversation,
  .assistant-history__toggle {
    width: 32px;
    height: 32px;
    min-height: 32px;
    justify-content: center;
    padding: 0;
  }
  .assistant-drawer__header > .icon-button,
  .assistant-history > .icon-button {
    width: 32px;
    min-width: 32px;
    height: 32px;
    min-height: 32px;
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
.assistant-detail-backdrop {
  position: fixed;
  z-index: 1;
  inset: 0;
  width: 100%;
  height: 100%;
  border: 0;
  background: rgb(17 24 39 / 28%);
}
.assistant-plan-dialog,
.assistant-form-slot {
  position: fixed;
  z-index: 2;
  inset: 6dvh 6vw;
  min-width: 0;
  min-height: 0;
  overflow: auto;
  border: 1px solid var(--border);
  border-radius: 12px;
  background: var(--surface);
  box-shadow: 0 24px 64px rgb(15 23 42 / 28%);
}
.assistant-form-slot {
  /* Слот постоянно смонтирован вне overlay для повторного Teleport. */
  z-index: 72;
}
.assistant-plan-dialog {
  top: 50%;
  bottom: auto;
  max-height: 88dvh;
  overflow: hidden;
  transform: translateY(-50%);
}
.assistant-plan-dialog :deep(.assistant-plan-editor) {
  height: auto;
  max-height: 88dvh;
}
.assistant-plan-dialog :deep(.assistant-plan-editor__body) {
  max-height: calc(88dvh - 124px);
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
  .assistant-plan-dialog,
  .assistant-form-slot {
    inset: 0;
    width: 100%;
    height: 100dvh;
    border-radius: 0;
  }
  .assistant-plan-dialog {
    top: 0;
    bottom: 0;
    transform: none;
  }
}
</style>
