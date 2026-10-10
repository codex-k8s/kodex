import { defineStore } from "pinia";
import { computed, ref } from "vue";

import {
  appendTurn,
  cancelAssistantTurn,
  archiveConversation,
  applyPlanDraft,
  createConversation,
  moveConversationToProject,
  purgeConversation,
  readAssistant,
  readProjectAssistant,
  readProjectAssistantAgent,
  createProjectAssistantProfile,
  readConversations,
  rejectPlanDraft,
  renameConversation,
  restoreConversation,
  savePlanDraft,
  validatePlanDraft,
} from "@/features/assistant/api";
import { conversationMatchesContext } from "@/features/assistant/context";
import { assistantConversationStorageBlocker } from "@/features/assistant/model";
import { appliedProjectFileRevision } from "./project-file-plan";
import { usePlatformStore } from "@/features/platform/store";
import {
  persistAssistantScope,
  restoreAssistantConversationRef,
  restoreAssistantScope,
} from "@/features/assistant/workspace-state";
import type {
  AssistantContextDescriptor,
  AssistantConversation,
  AssistantPlan,
  AssistantPlanOperationInput,
  AssistantPlanReceipt,
  SystemAssistant,
  ProjectAssistantProfile,
  Agent,
  AssistantScope,
  ListAssistantConversationsResponse,
} from "@/shared/api/generated/openapi/types.gen";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import { ownerRequestSignal } from "@/shared/api/owner-lifetime";

const realtimeHistoryPageLimit = 10;

type HistoryRequest = {
  projectRef?: string;
  filter: NonNullable<Parameters<typeof readConversations>[3]>;
  pageSize: number;
  assistantPin?: string;
  profileRef?: string;
};

function mergeConversation(
  previous: AssistantConversation | undefined,
  incoming: AssistantConversation,
  authoritativeTurns = false,
): AssistantConversation {
  if (!previous) return incoming;
  if (incoming.version < previous.version) return previous;
  if (authoritativeTurns) return incoming;
  const turns = new Map(previous.turns.map((turn) => [turn.ref, turn]));
  for (const turn of incoming.turns) turns.set(turn.ref, turn);
  return {
    ...incoming,
    turns: [...turns.values()].sort((a, b) => a.sequence - b.sequence),
  };
}

export const useAssistantStore = defineStore("assistant-workspace", () => {
  const assistant = ref<SystemAssistant>();
  const assistantScope = ref<AssistantScope>("SYSTEM");
  const projectAssistant = ref<ProjectAssistantProfile>();
  const projectAssistantAgent = ref<Agent>();
  const activeAssistantRef = computed(() =>
    assistantScope.value === "PROJECT"
      ? projectAssistant.value?.agentRef
      : assistant.value?.ref,
  );
  const conversations = ref<AssistantConversation[]>([]);
  const pendingCreatedConversationVersions = new Map<string, number>();
  const selectedRef = ref<string>();
  const context = ref<AssistantContextDescriptor>();
  const projectRef = ref<string>();
  const loading = ref(false);
  const foregroundLoading = ref(false);
  const historyRefreshing = ref(false);
  const busy = ref(false);
  const problem = ref<AppProblem>();
  const receipt = ref<AssistantPlanReceipt>();
  let generation = 0;
  let controller: AbortController | undefined;
  let profileController: AbortController | undefined;
  let profileReadGeneration = 0;
  const nextPageToken = ref<string>();
  const loadingMore = ref(false);
  const historyProblem = ref<AppProblem>();
  const historyCursors = new Set<string>();
  const historyQuery = ref("");
  const historyState = ref<AssistantConversation["state"]>("ACTIVE");
  let searchTimer: ReturnType<typeof setTimeout> | undefined;
  const retainedSelectedDetail = ref<AssistantConversation>();
  let historyReadDepth = 1;
  let historyOwnerLoaded = false;
  let historyRequest: HistoryRequest | undefined;
  let realtimeReadController: AbortController | undefined;
  let realtimeReadRevision = 0;
  let realtimeReadAgain = false;

  const conversationCreationReady = computed(() => {
    if (
      busy.value ||
      foregroundLoading.value ||
      problem.value?.status === 401 ||
      problem.value?.status === 403 ||
      (loading.value && !historyRefreshing.value)
    )
      return false;
    if (assistantScope.value !== "PROJECT") return true;
    const profile = projectAssistant.value;
    const agent = projectAssistantAgent.value;
    return Boolean(
      projectRef.value &&
      profile?.state === "ACTIVE" &&
      profile.projectRef === projectRef.value &&
      agent?.ref === profile.agentRef &&
      agent.projectRef === projectRef.value &&
      agent.enabled &&
      !agent.system,
    );
  });

  function cancelRealtimeRead(): void {
    if (realtimeReadController) loading.value = false;
    realtimeReadController?.abort();
    realtimeReadController = undefined;
    realtimeReadRevision += 1;
    realtimeReadAgain = false;
    historyRefreshing.value = false;
    retainedSelectedDetail.value = undefined;
  }

  function cancelReads(): void {
    cancelRealtimeRead();
    clearTimeout(searchTimer);
    controller?.abort();
    profileController?.abort();
    profileReadGeneration += 1;
    generation += 1;
    loading.value = false;
    foregroundLoading.value = false;
    loadingMore.value = false;
  }

  function checkPage(
    page: ListAssistantConversationsResponse,
    scope?: string,
  ): void {
    if (scope && page.items.some((item) => item.projectRef !== scope))
      throw new Error("Assistant history project scope mismatch");
    if (page.items.some((item) => !matchesAssistantPin(item)))
      throw new Error("Assistant history assistant scope mismatch");
    if (page.items.some((item) => item.state !== historyState.value))
      throw new Error("Assistant history state mismatch");
    if (page.nextPageToken && historyCursors.has(page.nextPageToken))
      throw new Error("Assistant history cursor repeated");
  }

  function matchesAssistantPin(value: AssistantConversation): boolean {
    if (
      value.assistantScope !== assistantScope.value ||
      (activeAssistantRef.value &&
        value.assistantRef !== activeAssistantRef.value)
    )
      return false;
    return assistantScope.value === "PROJECT"
      ? Boolean(
          projectAssistant.value &&
          value.assistantProfileRef === projectAssistant.value.ref,
        )
      : value.assistantProfileRef === undefined;
  }

  const selectedConversation = computed(() => {
    const listed = conversations.value.find(
      (item) => item.ref === selectedRef.value,
    );
    if (
      listed &&
      matchesAssistantPin(listed) &&
      (!projectRef.value || listed.projectRef === projectRef.value)
    )
      return listed;
    const retained = retainedSelectedDetail.value;
    return loading.value &&
      retained &&
      retained.ref === selectedRef.value &&
      matchesAssistantPin(retained) &&
      (!projectRef.value || retained.projectRef === projectRef.value)
      ? retained
      : undefined;
  });
  const sessionStorageBlocker = computed(() => {
    const platform = usePlatformStore();
    return assistantConversationStorageBlocker(
      selectedConversation.value,
      platform.runs,
      platform.bootstrap?.organizationRef,
    );
  });
  const sortedConversations = computed(() =>
    [...conversations.value].sort((a, b) =>
      b.updatedAt.localeCompare(a.updatedAt),
    ),
  );
  const historyPageSize = ref<number>();

  function setHistoryPageSize(value: number): void {
    historyPageSize.value = Math.min(100, Math.max(1, Math.floor(value)));
  }

  function captureHistoryRequest(
    nextProjectRef = projectRef.value,
  ): HistoryRequest {
    return {
      projectRef: nextProjectRef,
      filter: {
        query: historyQuery.value,
        state: historyState.value,
        assistantScope: assistantScope.value,
        ...(activeAssistantRef.value
          ? { assistantRef: activeAssistantRef.value }
          : {}),
      },
      pageSize: historyPageSize.value ?? 40,
      assistantPin: activeAssistantRef.value,
      profileRef: projectAssistant.value?.ref,
    };
  }

  function readHistory(
    request: HistoryRequest,
    pageToken: string | undefined,
    signal: AbortSignal,
  ) {
    return readConversations(
      request.projectRef,
      pageToken,
      signal,
      request.filter,
      request.pageSize,
    );
  }

  function selectMatchingConversation(): void {
    const currentContext = context.value;
    if (!currentContext) return;
    // Ручной выбор пользователя важнее контекста открытого экрана. Диалог
    // может относиться к другой сущности того же проекта: realtime-снимок и
    // навигация не должны внезапно возвращать пользователя к новому чату.
    const selected = conversations.value.find(
      (item) => item.ref === selectedRef.value,
    );
    if (selected) return;
    selectedRef.value =
      sortedConversations.value.find((item) =>
        conversationMatchesContext(item, currentContext),
      )?.ref ?? sortedConversations.value[0]?.ref;
  }

  async function load(
    nextContext: AssistantContextDescriptor,
    nextProjectRef?: string,
    select = true,
    nextAssistantScope: AssistantScope = nextProjectRef
      ? projectRef.value === nextProjectRef
        ? assistantScope.value
        : restoreAssistantScope(nextProjectRef)
      : "SYSTEM",
  ): Promise<void> {
    cancelReads();
    const current = ++generation;
    const retained = select
      ? (selectedConversation.value ? selectedRef.value : undefined) ||
        (typeof window !== "undefined" &&
        !historyQuery.value &&
        historyState.value === "ACTIVE"
          ? restoreAssistantConversationRef(
              nextProjectRef,
              undefined,
              nextAssistantScope,
            )
          : undefined)
      : undefined;
    if (
      projectRef.value !== nextProjectRef ||
      assistantScope.value !== nextAssistantScope
    ) {
      pendingCreatedConversationVersions.clear();
      conversations.value = [];
      selectedRef.value = undefined;
      projectAssistant.value = undefined;
      projectAssistantAgent.value = undefined;
      historyOwnerLoaded = false;
      historyReadDepth = 1;
    }
    controller = new AbortController();
    const signal = controller.signal;
    nextPageToken.value = undefined;
    historyRequest = undefined;
    historyProblem.value = undefined;
    historyCursors.clear();
    context.value = nextContext;
    projectRef.value = nextProjectRef;
    assistantScope.value = nextAssistantScope;
    foregroundLoading.value = true;
    loading.value = true;
    problem.value = undefined;
    try {
      if (nextAssistantScope === "PROJECT") {
        if (!nextProjectRef)
          throw new Error("Project assistant requires a project context");
        try {
          const profile = await readProjectAssistant(nextProjectRef, signal);
          const agent = await readProjectAssistantAgent(profile, signal);
          if (current !== generation) return;
          projectAssistant.value = profile;
          projectAssistantAgent.value = agent;
        } catch (error) {
          if (current !== generation) return;
          if (asProblem(error).status !== 404) throw error;
          projectAssistant.value = undefined;
          projectAssistantAgent.value = undefined;
          conversations.value = [];
          selectedRef.value = undefined;
          return;
        }
      }
      const request = captureHistoryRequest(nextProjectRef);
      const [assistantValue, firstPage] = await Promise.all([
        assistant.value
          ? Promise.resolve(assistant.value)
          : readAssistant(signal),
        readHistory(request, undefined, signal),
      ]);
      if (current !== generation) return;
      assistant.value = assistantValue;
      checkPage(firstPage, nextProjectRef);
      const conversationValues = [...firstPage.items];
      let page = firstPage;
      let count = 1;
      while (
        retained &&
        !conversationValues.some((item) => item.ref === retained) &&
        page.nextPageToken
      ) {
        if (count++ >= 30) break;
        historyCursors.add(page.nextPageToken);
        page = await readHistory(request, page.nextPageToken, signal);
        if (current !== generation) return;
        checkPage(page, nextProjectRef);
        conversationValues.push(...page.items);
      }
      nextPageToken.value = page.nextPageToken;
      historyRequest = { ...request, assistantPin: activeAssistantRef.value };
      historyReadDepth = count;
      historyOwnerLoaded = true;
      assistant.value = assistantValue;
      const previousByRef = new Map(
        conversations.value.map((conversation) => [
          conversation.ref,
          conversation,
        ]),
      );
      const unique = new Map<string, AssistantConversation>();
      for (const conversation of conversationValues)
        unique.set(
          conversation.ref,
          mergeConversation(unique.get(conversation.ref), conversation, true),
        );
      conversations.value = [...unique.values()].map((conversation) =>
        mergeConversation(
          previousByRef.get(conversation.ref),
          conversation,
          true,
        ),
      );
      if (select) {
        if (
          retained &&
          conversations.value.some((item) => item.ref === retained)
        )
          selectedRef.value = retained;
        selectMatchingConversation();
      }
    } catch (error) {
      if (current === generation) {
        problem.value = asProblem(error);
        conversations.value = [];
        selectedRef.value = undefined;
        nextPageToken.value = undefined;
        assistant.value = undefined;
        receipt.value = undefined;
      }
    } finally {
      if (current === generation) {
        foregroundLoading.value = false;
        loading.value = historyRefreshing.value;
      }
    }
  }

  async function loadMoreHistory(pageSize?: number): Promise<void> {
    const cursor = nextPageToken.value;
    if (!cursor || loading.value || loadingMore.value || busy.value) return;
    if (pageSize !== undefined) setHistoryPageSize(pageSize);
    const request = historyRequest;
    if (
      !request ||
      request.projectRef !== projectRef.value ||
      request.filter.assistantScope !== assistantScope.value ||
      request.assistantPin !== activeAssistantRef.value ||
      request.profileRef !== projectAssistant.value?.ref ||
      request.filter.query !== historyQuery.value ||
      request.filter.state !== historyState.value
    ) {
      // Общий realtime cursor не доказывает параметры owner-истории.
      // Один свежий read создаёт её цепочку; чужой token не отправляем.
      if (context.value) await load(context.value, projectRef.value);
      return;
    }
    const current = generation;
    controller ??= new AbortController();
    loadingMore.value = true;
    historyProblem.value = undefined;
    try {
      const page = await readHistory(
        { ...request, pageSize: historyPageSize.value ?? 40 },
        cursor,
        controller.signal,
      );
      if (current !== generation) return;
      historyCursors.add(cursor);
      checkPage(page, projectRef.value);
      for (const item of page.items) upsertConversation(item, false, true);
      historyReadDepth += 1;
      historyOwnerLoaded = true;
      nextPageToken.value = page.nextPageToken;
    } catch (error) {
      if (current === generation) historyProblem.value = asProblem(error);
    } finally {
      if (current === generation) loadingMore.value = false;
    }
  }

  function filterHistory(
    query: string,
    state: AssistantConversation["state"],
  ): void {
    if (busy.value) return;
    const stateChanged = historyState.value !== state;
    cancelReads();
    historyQuery.value = query;
    historyState.value = state;
    historyOwnerLoaded = false;
    historyReadDepth = 1;
    pendingCreatedConversationVersions.clear();
    conversations.value = [];
    selectedRef.value = undefined;
    nextPageToken.value = undefined;
    historyRequest = undefined;
    historyProblem.value = undefined;
    problem.value = undefined;
    loading.value = true;
    searchTimer = setTimeout(
      () => {
        if (context.value) void load(context.value, projectRef.value, false);
        else loading.value = false;
      },
      stateChanged ? 0 : 500,
    );
  }

  async function moveToTrash(
    conversation: AssistantConversation,
  ): Promise<void> {
    if (
      conversation.state === "ARCHIVED" ||
      busy.value ||
      loading.value ||
      problem.value
    )
      return;
    await runMutation(async () => {
      await archiveConversation(conversation);
      conversations.value = conversations.value.filter(
        (item) => item.ref !== conversation.ref,
      );
      selectedRef.value = undefined;
      receipt.value = undefined;
    });
  }

  async function archiveSelected(): Promise<void> {
    const conversation = selectedConversation.value;
    if (conversation) await moveToTrash(conversation);
  }

  async function restoreFromTrash(
    conversation: AssistantConversation,
  ): Promise<void> {
    if (conversation.state !== "ARCHIVED" || busy.value || loading.value)
      return;
    await runMutation(async () => {
      await restoreConversation(conversation);
      conversations.value = conversations.value.filter(
        (item) => item.ref !== conversation.ref,
      );
      if (selectedRef.value === conversation.ref) selectedRef.value = undefined;
    });
  }

  async function purgeFromTrash(
    conversation: AssistantConversation,
  ): Promise<void> {
    if (conversation.state !== "ARCHIVED" || busy.value || loading.value)
      return;
    await runMutation(async () => {
      await purgeConversation(conversation);
      conversations.value = conversations.value.filter(
        (item) => item.ref !== conversation.ref,
      );
      if (selectedRef.value === conversation.ref) selectedRef.value = undefined;
    });
  }

  async function emptyTrash(): Promise<void> {
    if (historyState.value !== "ARCHIVED" || busy.value || loading.value)
      return;
    await runMutation(async () => {
      const all = new Map<string, AssistantConversation>();
      let cursor: string | undefined;
      const seen = new Set<string>();
      const signal = new AbortController().signal;
      do {
        if (cursor && seen.has(cursor))
          throw new Error("Assistant trash cursor repeated");
        if (cursor) seen.add(cursor);
        const page = await readConversations(
          projectRef.value,
          cursor,
          signal,
          {
            state: "ARCHIVED",
            assistantScope: assistantScope.value,
            assistantRef: activeAssistantRef.value,
          },
          historyPageSize.value ?? 40,
        );
        for (const item of page.items) all.set(item.ref, item);
        cursor = page.nextPageToken;
      } while (cursor);
      for (const item of all.values()) await purgeConversation(item);
      conversations.value = [];
      selectedRef.value = undefined;
      nextPageToken.value = undefined;
      historyRequest = undefined;
    });
  }

  async function moveSelectedToProject(
    targetProjectRef: string,
  ): Promise<AssistantConversation> {
    const conversation = selectedConversation.value;
    if (
      !conversation ||
      conversation.projectRef ||
      projectRef.value ||
      busy.value ||
      loading.value
    )
      throw new Error("Assistant conversation cannot move from this context");
    return runMutation(async () => {
      const moved = await moveConversationToProject(
        conversation,
        targetProjectRef,
      );
      upsertConversation(moved);
      return moved;
    });
  }

  function setContext(
    nextContext: AssistantContextDescriptor,
    nextProjectRef?: string,
  ): void {
    const projectChanged = projectRef.value !== nextProjectRef;
    context.value = nextContext;
    projectRef.value = nextProjectRef;
    if (projectChanged)
      assistantScope.value = restoreAssistantScope(nextProjectRef);
    else if (!nextProjectRef) assistantScope.value = "SYSTEM";
    if (projectChanged) {
      cancelReads();
      pendingCreatedConversationVersions.clear();
      conversations.value = [];
      nextPageToken.value = undefined;
      historyRequest = undefined;
      historyCursors.clear();
      historyOwnerLoaded = false;
      historyReadDepth = 1;
      selectedRef.value = undefined;
      projectAssistant.value = undefined;
      projectAssistantAgent.value = undefined;
      return;
    }
    selectMatchingConversation();
  }

  async function runMutation<T>(operation: () => Promise<T>): Promise<T> {
    // Mutation авторитетнее чтения, которое началось до него.
    cancelReads();
    controller = undefined;
    const currentGeneration = generation;
    busy.value = true;
    problem.value = undefined;
    try {
      return await operation();
    } catch (error) {
      const normalized = asProblem(error);
      if (currentGeneration === generation) problem.value = normalized;
      throw normalized;
    } finally {
      busy.value = false;
    }
  }

  function upsertConversation(
    value: AssistantConversation,
    select = true,
    authoritativeTurns = false,
  ): AssistantConversation {
    if (!matchesAssistantPin(value))
      throw new Error("Assistant conversation scope mismatch");
    const index = conversations.value.findIndex(
      (item) => item.ref === value.ref,
    );
    const merged = mergeConversation(
      index >= 0 ? conversations.value[index] : undefined,
      value,
      authoritativeTurns,
    );
    if (index >= 0) conversations.value[index] = merged;
    else conversations.value.push(merged);
    if (select) selectedRef.value = merged.ref;
    return merged;
  }

  function applyRealtimeSnapshot(
    assistantValue: SystemAssistant | undefined,
    values: AssistantConversation[],
    sourceProjectRef?: string,
    sourceNextPageToken?: string,
  ): void {
    if (projectRef.value !== sourceProjectRef) return;
    if (assistantValue) assistant.value = assistantValue;
    const scoped = sourceProjectRef
      ? values.filter((value) => value.projectRef === sourceProjectRef)
      : values;
    const visible = scoped.filter(
      (value) =>
        matchesAssistantPin(value) && value.state === historyState.value,
    );
    const previousByRef = new Map(
      conversations.value.map((conversation) => [
        conversation.ref,
        conversation,
      ]),
    );
    const requiresOwnerRead =
      Boolean(historyQuery.value || historyState.value !== "ACTIVE") ||
      ((Boolean(sourceNextPageToken) ||
        (assistantScope.value === "PROJECT" && !visible.length)) &&
        [...previousByRef.values()].some(
          (previous) =>
            matchesAssistantPin(previous) &&
            (!sourceProjectRef || previous.projectRef === sourceProjectRef) &&
            !visible.some((incoming) => incoming.ref === previous.ref),
        ));
    if (requiresOwnerRead) {
      // Частичная страница не является tombstone. Старое тело остаётся
      // readonly до ограниченной owner-сверки, а не навсегда в merged cache.
      retainedSelectedDetail.value ??= selectedConversation.value;
      for (const incoming of visible) {
        const previous = previousByRef.get(incoming.ref);
        if (previous)
          Object.assign(previous, mergeConversation(previous, incoming, true));
        else conversations.value.push(incoming);
      }
      refreshPartialHistory();
      return;
    }
    cancelRealtimeRead();
    const reconciled = visible.map((incoming) => {
      const previous = previousByRef.get(incoming.ref);
      const merged = mergeConversation(previous, incoming, true);
      if (!previous) return merged;
      Object.assign(previous, merged);
      return previous;
    });
    // ACK создания опережает общий realtime-кэш. Отсутствие нового диалога
    // в этом кэше ещё не означает удаление и не отменяет выбор пользователя.
    const visibleByRef = new Map(visible.map((value) => [value.ref, value]));
    for (const [ref, version] of pendingCreatedConversationVersions) {
      const previous = previousByRef.get(ref);
      const incoming = visibleByRef.get(ref);
      if (
        !previous ||
        !matchesAssistantPin(previous) ||
        (incoming && incoming.version >= version)
      ) {
        pendingCreatedConversationVersions.delete(ref);
        continue;
      }
      if (!incoming) reconciled.push(previous);
    }
    conversations.value.splice(0, conversations.value.length, ...reconciled);
    // Курсор общего cache-снимка не относится к отдельно фильтрованной
    // истории помощника. PROJECT продолжает собственный авторитетный cursor.
    if (
      assistantScope.value === "SYSTEM" &&
      (!sourceNextPageToken || !historyOwnerLoaded)
    ) {
      nextPageToken.value = sourceNextPageToken;
      historyRequest = undefined;
      historyOwnerLoaded = false;
    }
    if (
      selectedRef.value === undefined &&
      typeof window !== "undefined" &&
      !historyQuery.value &&
      historyState.value === "ACTIVE"
    ) {
      const retained = restoreAssistantConversationRef(
        sourceProjectRef,
        undefined,
        assistantScope.value,
      );
      // Storage хранит только предпочтение: выбирать можно лишь диалог,
      // уже прошедший scope/pin-проверки авторитетного снимка.
      if (
        retained &&
        visible.some(
          (value) => value.ref === retained && value.state === "ACTIVE",
        )
      )
        selectedRef.value = retained;
    }
    selectMatchingConversation();
  }

  function clearRealtimeState(): void {
    cancelReads();
    pendingCreatedConversationVersions.clear();
    conversations.value = [];
    selectedRef.value = undefined;
    nextPageToken.value = undefined;
    historyRequest = undefined;
    assistant.value = undefined;
    projectAssistant.value = undefined;
    projectAssistantAgent.value = undefined;
    receipt.value = undefined;
    historyReadDepth = 1;
    historyOwnerLoaded = false;
    historyCursors.clear();
  }

  function refreshPartialHistory(): void {
    if (realtimeReadController) {
      realtimeReadAgain = true;
      return;
    }
    const currentContext = context.value;
    if (!currentContext) return;
    // Повторный partial wake требует следующей сверки, но не делает текущий
    // owner read устаревшим: иначе непрерывный поток не даёт принять результат.
    realtimeReadRevision += 1;
    const current = generation;
    const revision = realtimeReadRevision;
    const ownerSignal = ownerRequestSignal();
    const readController = new AbortController();
    realtimeReadController = readController;
    const signal = AbortSignal.any([readController.signal, ownerSignal]);
    const sourceProject = projectRef.value;
    const sourceScope = assistantScope.value;
    const sourceAssistant = activeAssistantRef.value;
    const sourceProfile = projectAssistant.value?.ref;
    const retainedRef = selectedRef.value;
    const filter = {
      query: historyQuery.value,
      state: historyState.value,
      assistantScope: sourceScope,
      assistantRef: sourceAssistant,
    };
    const depth = Math.min(
      realtimeHistoryPageLimit,
      Math.max(1, historyReadDepth),
    );
    const pageSize = Math.min(
      100,
      Math.max(historyPageSize.value ?? 40, conversations.value.length),
    );
    const isCurrent = () =>
      !signal.aborted &&
      current === generation &&
      revision === realtimeReadRevision &&
      sourceProject === projectRef.value &&
      sourceScope === assistantScope.value &&
      sourceAssistant === activeAssistantRef.value &&
      sourceProfile === projectAssistant.value?.ref;
    loading.value = true;
    historyRefreshing.value = true;
    if (problem.value?.status !== 401 && problem.value?.status !== 403)
      problem.value = undefined;
    void (async () => {
      try {
        const items = new Map<string, AssistantConversation>();
        const cursors = new Set<string>();
        let cursor: string | undefined;
        let count = 0;
        do {
          const page = await readConversations(
            sourceProject,
            cursor,
            signal,
            filter,
            pageSize,
          );
          if (!isCurrent()) return;
          if (
            page.items.some(
              (item) =>
                !matchesAssistantPin(item) ||
                item.state !== filter.state ||
                (sourceProject && item.projectRef !== sourceProject),
            )
          )
            throw new Error("Assistant history scope mismatch");
          for (const item of page.items)
            items.set(
              item.ref,
              mergeConversation(items.get(item.ref), item, true),
            );
          if (cursor) cursors.add(cursor);
          cursor = page.nextPageToken;
          if (cursor && cursors.has(cursor))
            throw new Error("Assistant history cursor repeated");
          count += 1;
        } while (
          cursor &&
          count < realtimeHistoryPageLimit &&
          (count < depth || (retainedRef && !items.has(retainedRef)))
        );
        if (!isCurrent()) return;
        problem.value = undefined;
        const previous = new Map(
          conversations.value.map((item) => [item.ref, item]),
        );
        conversations.value = [...items.values()].map((item) =>
          mergeConversation(previous.get(item.ref), item, true),
        );
        historyCursors.clear();
        for (const consumed of cursors) historyCursors.add(consumed);
        nextPageToken.value = cursor;
        historyRequest = {
          projectRef: sourceProject,
          filter,
          pageSize,
          assistantPin: sourceAssistant,
          profileRef: sourceProfile,
        };
        historyReadDepth = count;
        historyOwnerLoaded = true;
        retainedSelectedDetail.value = undefined;
        selectMatchingConversation();
      } catch (error) {
        if (!isCurrent()) return;
        problem.value = asProblem(error);
        conversations.value = [];
        retainedSelectedDetail.value = undefined;
        selectedRef.value = undefined;
        nextPageToken.value = undefined;
      } finally {
        if (
          realtimeReadController === readController &&
          current === generation
        ) {
          realtimeReadController = undefined;
          if (ownerSignal.aborted) clearRealtimeState();
          else if (
            sourceProject !== projectRef.value ||
            sourceScope !== assistantScope.value ||
            sourceAssistant !== activeAssistantRef.value ||
            sourceProfile !== projectAssistant.value?.ref
          ) {
            conversations.value = [];
            retainedSelectedDetail.value = undefined;
            selectedRef.value = undefined;
            nextPageToken.value = undefined;
            loading.value = false;
            historyRefreshing.value = false;
          } else if (realtimeReadAgain) {
            realtimeReadAgain = false;
            refreshPartialHistory();
          } else {
            historyRefreshing.value = false;
            loading.value = foregroundLoading.value;
          }
        }
      }
    })();
  }

  function replacePlan(value: AssistantPlan): void {
    conversations.value = conversations.value.map((conversation) => ({
      ...conversation,
      turns: conversation.turns.map((turn) =>
        turn.plan?.ref === value.ref ? { ...turn, plan: value } : turn,
      ),
    }));
  }

  async function startConversation(): Promise<AssistantConversation> {
    const currentContext = context.value;
    if (!currentContext) throw new Error("Assistant context is unavailable");
    return runMutation(async () => {
      const value = await createConversation(
        currentContext,
        projectRef.value,
        assistantScope.value,
      );
      if (
        value.assistantScope !== assistantScope.value ||
        (activeAssistantRef.value &&
          value.assistantRef !== activeAssistantRef.value)
      )
        throw new Error("Created assistant conversation scope mismatch");
      if (historyQuery.value || historyState.value !== "ACTIVE") {
        pendingCreatedConversationVersions.clear();
        conversations.value = [];
        nextPageToken.value = undefined;
      }
      historyQuery.value = "";
      historyState.value = "ACTIVE";
      upsertConversation(value);
      pendingCreatedConversationVersions.set(value.ref, value.version);
      return value;
    });
  }

  async function changeTitle(value: string): Promise<void> {
    const conversation = selectedConversation.value;
    if (
      !conversation ||
      conversation.state === "ARCHIVED" ||
      value.trim() === ""
    )
      return;
    await runMutation(async () => {
      upsertConversation(await renameConversation(conversation, value.trim()));
    });
  }

  async function send(
    content: string,
    attachmentSetRef?: string,
    deliveryMode: "QUEUE" | "INTERRUPT_ACTIVE" = "QUEUE",
  ): Promise<void> {
    const normalized = content.trim();
    if (!normalized) return;
    if (sessionStorageBlocker.value)
      throw new Error("Assistant session storage is unavailable");
    if (
      selectedConversation.value &&
      selectedConversation.value.state !== "ACTIVE"
    )
      throw new Error("Assistant conversation is read-only");
    await runMutation(async () => {
      let conversation = selectedConversation.value;
      if (!conversation) {
        if (!context.value) throw new Error("Assistant context is unavailable");
        conversation = await createConversation(
          context.value,
          projectRef.value,
          assistantScope.value,
        );
        upsertConversation(conversation);
        pendingCreatedConversationVersions.set(
          conversation.ref,
          conversation.version,
        );
      }
      if (!context.value) throw new Error("Assistant context is unavailable");
      if (sessionStorageBlocker.value)
        throw new Error("Assistant session storage is unavailable");
      const appended = attachmentSetRef
        ? await appendTurn(
            conversation,
            normalized,
            context.value,
            attachmentSetRef,
            deliveryMode,
          )
        : await appendTurn(
            conversation,
            normalized,
            context.value,
            undefined,
            deliveryMode,
          );
      upsertConversation(appended);
    });
  }

  async function stopActiveTurn(): Promise<void> {
    const conversation = selectedConversation.value;
    if (!conversation || conversation.state !== "ACTIVE") return;
    await runMutation(async () => {
      const runRef = await cancelAssistantTurn(conversation);
      upsertConversation({
        ...conversation,
        turns: conversation.turns.map((turn) =>
          turn.runRef === runRef &&
          (turn.state === "QUEUED" || turn.state === "RUNNING")
            ? { ...turn, state: "CANCELLED" }
            : turn,
        ),
      });
    });
  }

  async function saveDraft(
    plan: AssistantPlan,
    summary: string,
    operations: AssistantPlanOperationInput[],
  ): Promise<AssistantPlan> {
    return runMutation(async () => {
      const value = await savePlanDraft(plan, summary, operations);
      receipt.value = undefined;
      replacePlan(value);
      return value;
    });
  }

  async function validate(plan: AssistantPlan): Promise<AssistantPlan> {
    return runMutation(async () => {
      const value = await validatePlanDraft(plan);
      replacePlan(value);
      return value;
    });
  }

  async function apply(plan: AssistantPlan): Promise<AssistantPlanReceipt> {
    return runMutation(async () => {
      const value = await applyPlanDraft(plan);
      if (
        value.conversation.ref !== plan.conversationRef ||
        value.plan.ref !== plan.ref ||
        value.plan.conversationRef !== plan.conversationRef ||
        value.plan.revision !== plan.revision ||
        value.receipt.planRef !== value.plan.ref ||
        value.receipt.planRevision !== value.plan.revision ||
        !(
          (value.plan.state === "APPLIED" &&
            value.receipt.outcome === "APPLIED") ||
          (value.plan.state === "STALE" && value.receipt.outcome === "CONFLICT")
        )
      )
        throw new Error("Assistant plan application response mismatch");
      if (value.receipt.outcome === "APPLIED") {
        const appliedPlan = { ...value.plan, receipt: value.receipt };
        if (
          appliedPlan.operations.some(
            (operation) =>
              operation.selected &&
              operation.type === "CREATE_PROJECT_FILE_REVISION" &&
              !appliedProjectFileRevision(appliedPlan, operation.ref),
          )
        )
          throw new Error(
            "Assistant file revision application receipt mismatch",
          );
      }
      receipt.value = value.receipt;
      // Ответ применения содержит отдельную авторитетную квитанцию и только
      // ref диалога. Привязываем её к точной ревизии, не подменяя весь диалог.
      replacePlan({ ...value.plan, receipt: value.receipt });
      return value.receipt;
    });
  }

  async function reject(plan: AssistantPlan): Promise<AssistantPlanReceipt> {
    return runMutation(async () => {
      const value = await rejectPlanDraft(plan);
      receipt.value = value.receipt;
      replacePlan(value.plan);
      return value.receipt;
    });
  }

  function clearReceipt(): void {
    receipt.value = undefined;
  }

  async function selectAssistantScope(scope: AssistantScope): Promise<void> {
    if (
      busy.value ||
      !context.value ||
      (scope === "PROJECT" && !projectRef.value)
    )
      return;
    persistAssistantScope(projectRef.value, scope);
    await load(context.value, projectRef.value, true, scope);
  }

  async function invalidateProjectAssistantFromRealtime(
    sourceProjectRef?: string,
  ): Promise<void> {
    if (
      assistantScope.value !== "PROJECT" ||
      !sourceProjectRef ||
      projectRef.value !== sourceProjectRef
    )
      return;
    profileController?.abort();
    profileController = new AbortController();
    const signal = profileController.signal;
    const currentGeneration = generation;
    const currentProfileRead = ++profileReadGeneration;
    const isCurrent = () =>
      !signal.aborted &&
      currentGeneration === generation &&
      currentProfileRead === profileReadGeneration &&
      projectRef.value === sourceProjectRef &&
      assistantScope.value === "PROJECT";
    projectAssistantAgent.value = undefined;
    try {
      const profile = await readProjectAssistant(sourceProjectRef, signal);
      const agent = await readProjectAssistantAgent(profile, signal);
      if (!isCurrent()) return;
      projectAssistant.value = profile;
      projectAssistantAgent.value = agent;
    } catch (error) {
      if (!isCurrent()) return;
      projectAssistant.value = undefined;
      projectAssistantAgent.value = undefined;
      conversations.value = [];
      selectedRef.value = undefined;
      nextPageToken.value = undefined;
      if (asProblem(error).status !== 404) problem.value = asProblem(error);
    }
  }

  async function createProjectProfile(input: {
    name: string;
    purpose: string;
    instructions: string;
  }): Promise<void> {
    const targetProject = projectRef.value;
    const targetContext = context.value;
    if (
      !targetProject ||
      !targetContext ||
      assistantScope.value !== "PROJECT" ||
      busy.value
    )
      return;
    const isCurrentProfileScope = () =>
      projectRef.value === targetProject && assistantScope.value === "PROJECT";
    await runMutation(async () => {
      const profile = await createProjectAssistantProfile(targetProject, input);
      if (
        projectRef.value !== targetProject ||
        assistantScope.value !== "PROJECT"
      )
        return;
      projectAssistant.value = profile;
      const agent = await readProjectAssistantAgent(profile);
      if (isCurrentProfileScope()) projectAssistantAgent.value = agent;
    });
    if (isCurrentProfileScope()) await load(targetContext, targetProject);
  }

  return {
    assistant,
    assistantScope,
    projectAssistant,
    projectAssistantAgent,
    activeAssistantRef,
    selectAssistantScope,
    createProjectProfile,
    invalidateProjectAssistantFromRealtime,
    conversations,
    selectedRef,
    context,
    projectRef,
    loading,
    historyRefreshing,
    conversationCreationReady,
    busy,
    problem,
    receipt,
    selectedConversation,
    sessionStorageBlocker,
    sortedConversations,
    nextPageToken,
    loadingMore,
    historyProblem,
    historyQuery,
    historyState,
    filterHistory,
    archiveSelected,
    moveToTrash,
    restoreFromTrash,
    purgeFromTrash,
    emptyTrash,
    moveSelectedToProject,
    loadMoreHistory,
    setHistoryPageSize,
    cancelReads,
    load,
    setContext,
    applyRealtimeSnapshot,
    clearRealtimeState,
    startConversation,
    changeTitle,
    send,
    stopActiveTurn,
    saveDraft,
    validate,
    apply,
    reject,
    clearReceipt,
  };
});
