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
): AssistantConversation {
  if (!previous) return incoming;
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
      conversations.value = conversationValues;
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
    busy.value = true;
    problem.value = undefined;
    try {
      return await operation();
    } catch (error) {
      problem.value = asProblem(error);
      throw error;
    } finally {
      busy.value = false;
    }
  }

  function upsertConversation(value: AssistantConversation): void {
    const index = conversations.value.findIndex(
      (item) => item.ref === value.ref,
    );
    const merged = mergeConversation(
      index >= 0 ? conversations.value[index] : undefined,
      value,
    );
    if (index >= 0) conversations.value[index] = merged;
    else conversations.value.push(merged);
    selectedRef.value = merged.ref;
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

  async function send(content: string): Promise<void> {
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
      upsertConversation(await appendTurn(conversation.ref, normalized));
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
