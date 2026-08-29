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
  AssistantTurn,
  SystemAssistant,
} from "@/shared/api/generated/openapi/types.gen";
import { asProblem, type AppProblem } from "@/shared/api/problem";

const activeTurnStates = new Set<AssistantTurn["state"]>(["QUEUED", "RUNNING"]);
const conversationPollIntervalMs = 1_000;
const conversationPollAttempts = 15 * 60;

function delay(durationMs: number): Promise<void> {
  return new Promise((resolve) => globalThis.setTimeout(resolve, durationMs));
}

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
  const conversationPollGenerations = new Map<string, number>();

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

  async function pollConversationUntilSettled(
    conversationRef: string,
    submittedTurn: AssistantTurn,
    sourceProjectRef?: string,
  ): Promise<void> {
    const pollGeneration =
      (conversationPollGenerations.get(conversationRef) ?? 0) + 1;
    conversationPollGenerations.set(conversationRef, pollGeneration);

    for (let attempt = 0; attempt < conversationPollAttempts; attempt += 1) {
      await delay(conversationPollIntervalMs);
      if (
        conversationPollGenerations.get(conversationRef) !== pollGeneration ||
        projectRef.value !== sourceProjectRef
      )
        return;

      let incoming: AssistantConversation | undefined;
      try {
        incoming = (await readConversations(sourceProjectRef)).find(
          (conversation) => conversation.ref === conversationRef,
        );
      } catch {
        continue;
      }
      if (
        !incoming ||
        conversationPollGenerations.get(conversationRef) !== pollGeneration ||
        projectRef.value !== sourceProjectRef
      )
        continue;

      const merged = upsertConversation(incoming, false, true);
      const submitted = merged.turns.find(
        (turn) => turn.ref === submittedTurn.ref,
      );
      if (submitted?.state === "FAILED") break;
      if (
        submitted?.state === "COMPLETED" &&
        merged.turns.some(
          (turn) =>
            turn.sequence > submittedTurn.sequence &&
            !activeTurnStates.has(turn.state),
        )
      )
        break;
    }

    if (conversationPollGenerations.get(conversationRef) === pollGeneration)
      conversationPollGenerations.delete(conversationRef);
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
    artifactRefs: string[] = [],
  ): Promise<void> {
    const normalized = content.trim();
    if (!normalized) return;
    let submitted:
      | {
          conversationRef: string;
          projectRef?: string;
          turn: AssistantTurn;
        }
      | undefined;
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
      const previousSequence = Math.max(
        0,
        ...conversation.turns.map((turn) => turn.sequence),
      );
      const appended = artifactRefs.length
        ? await appendTurn(conversation.ref, normalized, artifactRefs)
        : await appendTurn(conversation.ref, normalized);
      conversations.value = conversations.value.map((item) =>
        item.ref === conversation.ref
          ? { ...item, turns: item.turns.filter((turn) => !turn.plan) }
          : item,
      );
      upsertConversation(appended);
      const turn = [...appended.turns]
        .filter(
          (candidate) =>
            candidate.role === "USER" && candidate.sequence > previousSequence,
        )
        .sort((left, right) => right.sequence - left.sequence)[0];
      if (turn)
        submitted = {
          conversationRef: appended.ref,
          projectRef: projectRef.value,
          turn,
        };
    });
    if (submitted)
      void pollConversationUntilSettled(
        submitted.conversationRef,
        submitted.turn,
        submitted.projectRef,
      );
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
