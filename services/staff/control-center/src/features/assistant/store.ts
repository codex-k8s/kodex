import { defineStore } from "pinia";
import { computed, ref } from "vue";

import {
  appendTurn,
  applyPlanDraft,
  createConversation,
  readAssistant,
  readConversations,
  rejectPlanDraft,
  renameConversation,
  savePlanDraft,
  validatePlanDraft,
} from "@/features/assistant/api";
import { conversationMatchesContext } from "@/features/assistant/context";
import type {
  AssistantContextDescriptor,
  AssistantConversation,
  AssistantPlan,
  AssistantPlanOperationInput,
  AssistantPlanReceipt,
  SystemAssistant,
} from "@/shared/api/generated/openapi/types.gen";
import { asProblem, type AppProblem } from "@/shared/api/problem";

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
  const conversations = ref<AssistantConversation[]>([]);
  const selectedRef = ref<string>();
  const context = ref<AssistantContextDescriptor>();
  const projectRef = ref<string>();
  const loading = ref(false);
  const busy = ref(false);
  const problem = ref<AppProblem>();
  const receipt = ref<AssistantPlanReceipt>();
  let generation = 0;

  const selectedConversation = computed(() =>
    conversations.value.find((item) => item.ref === selectedRef.value),
  );
  const sortedConversations = computed(() =>
    [...conversations.value].sort((a, b) =>
      b.updatedAt.localeCompare(a.updatedAt),
    ),
  );

  function selectMatchingConversation(): void {
    const currentContext = context.value;
    if (!currentContext) return;
    const selected = conversations.value.find(
      (item) =>
        item.ref === selectedRef.value &&
        conversationMatchesContext(item, currentContext),
    );
    if (selected) return;
    selectedRef.value = sortedConversations.value.find((item) =>
      conversationMatchesContext(item, currentContext),
    )?.ref;
  }

  async function load(
    nextContext: AssistantContextDescriptor,
    nextProjectRef?: string,
  ): Promise<void> {
    const current = ++generation;
    context.value = nextContext;
    projectRef.value = nextProjectRef;
    loading.value = true;
    problem.value = undefined;
    try {
      const [assistantValue, conversationValues] = await Promise.all([
        readAssistant(),
        readConversations(nextProjectRef),
      ]);
      if (current !== generation) return;
      assistant.value = assistantValue;
      const previousByRef = new Map(
        conversations.value.map((conversation) => [
          conversation.ref,
          conversation,
        ]),
      );
      conversations.value = conversationValues.map((conversation) =>
        mergeConversation(
          previousByRef.get(conversation.ref),
          conversation,
          true,
        ),
      );
      selectMatchingConversation();
    } catch (error) {
      if (current === generation) problem.value = asProblem(error);
    } finally {
      if (current === generation) loading.value = false;
    }
  }

  function setContext(
    nextContext: AssistantContextDescriptor,
    nextProjectRef?: string,
  ): void {
    const projectChanged = projectRef.value !== nextProjectRef;
    context.value = nextContext;
    projectRef.value = nextProjectRef;
    if (projectChanged) {
      selectedRef.value = undefined;
      return;
    }
    selectMatchingConversation();
  }

  async function runMutation<T>(operation: () => Promise<T>): Promise<T> {
    // Mutation авторитетнее чтения, которое началось до него.
    generation += 1;
    loading.value = false;
    busy.value = true;
    problem.value = undefined;
    try {
      return await operation();
    } catch (error) {
      const normalized = asProblem(error);
      problem.value = normalized;
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
  ): void {
    if (projectRef.value !== sourceProjectRef) return;
    if (assistantValue) assistant.value = assistantValue;
    const visible = sourceProjectRef
      ? values.filter((value) => value.projectRef === sourceProjectRef)
      : values;
    const previousByRef = new Map(
      conversations.value.map((conversation) => [
        conversation.ref,
        conversation,
      ]),
    );
    const reconciled = visible.map((incoming) => {
      const previous = previousByRef.get(incoming.ref);
      const merged = mergeConversation(previous, incoming, true);
      if (!previous) return merged;
      Object.assign(previous, merged);
      return previous;
    });
    conversations.value.splice(0, conversations.value.length, ...reconciled);
    selectMatchingConversation();
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
      const value = await createConversation(currentContext, projectRef.value);
      upsertConversation(value);
      return value;
    });
  }

  async function changeTitle(value: string): Promise<void> {
    const conversation = selectedConversation.value;
    if (!conversation || value.trim() === "") return;
    await runMutation(async () => {
      upsertConversation(await renameConversation(conversation, value.trim()));
    });
  }

  async function send(
    content: string,
    attachmentSetRef?: string,
  ): Promise<void> {
    const normalized = content.trim();
    if (!normalized) return;
    await runMutation(async () => {
      let conversation = selectedConversation.value;
      if (!conversation) {
        if (!context.value) throw new Error("Assistant context is unavailable");
        conversation = await createConversation(
          context.value,
          projectRef.value,
        );
        upsertConversation(conversation);
      }
      const appended = attachmentSetRef
        ? await appendTurn(conversation.ref, normalized, attachmentSetRef)
        : await appendTurn(conversation.ref, normalized);
      conversations.value = conversations.value.map((item) =>
        item.ref === conversation.ref
          ? { ...item, turns: item.turns.filter((turn) => !turn.plan) }
          : item,
      );
      upsertConversation(appended);
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
      receipt.value = value.receipt;
      upsertConversation(value.conversation);
      replacePlan(value.plan);
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

  return {
    assistant,
    conversations,
    selectedRef,
    context,
    projectRef,
    loading,
    busy,
    problem,
    receipt,
    selectedConversation,
    sortedConversations,
    load,
    setContext,
    applyRealtimeSnapshot,
    startConversation,
    changeTitle,
    send,
    saveDraft,
    validate,
    apply,
    reject,
    clearReceipt,
  };
});
