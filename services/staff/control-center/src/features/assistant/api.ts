import { requestSignal } from "@/shared/api/client";
import {
  addAssistantTurn,
  applyAssistantPlan,
  createAssistantConversation,
  getSystemAssistant,
  listAssistantConversations,
  rejectAssistantPlan,
  updateAssistantConversationTitle,
  updateAssistantPlanDraft,
  validateAssistantPlan,
} from "@/shared/api/generated/openapi/sdk.gen";
import type {
  AssistantContextDescriptor,
  AssistantConversation,
  AssistantPlan,
  AssistantPlanApplicationResponse,
  AssistantPlanDecisionResponse,
  AssistantPlanOperationInput,
  SystemAssistant,
} from "@/shared/api/generated/openapi/types.gen";
import { mutate } from "@/shared/api/mutation";
import { asProblem, unwrap } from "@/shared/api/problem";

const readRetryDelaysMs = [0, 200, 600] as const;

export async function readAssistant(): Promise<SystemAssistant> {
  return readWithRetry(
    async () =>
      (await unwrap(getSystemAssistant({ signal: requestSignal() }))).data,
  );
}

export async function readConversations(
  projectRef?: string,
): Promise<AssistantConversation[]> {
  return readWithRetry(
    async () =>
      (
        await unwrap(
          listAssistantConversations({
            ...(projectRef ? { query: { projectRef } } : {}),
            signal: requestSignal(),
          }),
        )
      ).data.items,
  );
}

export async function createConversation(
  context: AssistantContextDescriptor,
  projectRef?: string,
): Promise<AssistantConversation> {
  return (
    await mutate((headers) =>
      createAssistantConversation({
        body: { context, ...(projectRef ? { projectRef } : {}) },
        headers: {
          "Idempotency-Key": headers["Idempotency-Key"],
          "X-CSRF-Token": headers["X-CSRF-Token"],
        },
        signal: requestSignal(),
      }),
    )
  ).data;
}

export async function renameConversation(
  conversation: AssistantConversation,
  title: string,
): Promise<AssistantConversation> {
  return (
    await mutate(
      (headers) =>
        updateAssistantConversationTitle({
          path: { conversationRef: conversation.ref },
          body: { title },
          headers: {
            "Idempotency-Key": headers["Idempotency-Key"],
            "X-CSRF-Token": headers["X-CSRF-Token"],
            "If-Match": headers["If-Match"] ?? "",
          },
          signal: requestSignal(),
        }),
      conversation.version,
    )
  ).data;
}

export async function appendTurn(
  conversation: AssistantConversation,
  content: string,
  attachmentSetRef?: string,
): Promise<AssistantConversation> {
  const knownTurnRefs = new Set(conversation.turns.map((turn) => turn.ref));
  try {
    return (
      await mutate((headers) =>
        addAssistantTurn({
          path: { conversationRef: conversation.ref },
          body: { content, ...(attachmentSetRef ? { attachmentSetRef } : {}) },
          headers: {
            "Idempotency-Key": headers["Idempotency-Key"],
            "X-CSRF-Token": headers["X-CSRF-Token"],
          },
          signal: requestSignal(),
        }),
      )
    ).data;
  } catch (error) {
    const mutationProblem = asProblem(error);
    if (!mutationProblem.retryable) throw mutationProblem;
    try {
      const authoritative = (
        await readConversations(conversation.projectRef)
      ).find((item) => item.ref === conversation.ref);
      const matchingTurns = authoritative?.turns.filter(
        (turn) =>
          turn.role === "USER" &&
          turn.content === content &&
          !knownTurnRefs.has(turn.ref),
      );
      if (authoritative && matchingTurns?.length === 1) return authoritative;
    } catch {
      // Сохраняем исходную ошибку, если авторитетная сверка недоступна.
    }
    throw mutationProblem;
  }
}

async function readWithRetry<T>(request: () => Promise<T>): Promise<T> {
  let lastProblem = asProblem(new Error("Assistant read did not start"));
  for (const delayMs of readRetryDelaysMs) {
    if (delayMs > 0) {
      await new Promise<void>((resolve) =>
        globalThis.setTimeout(resolve, delayMs),
      );
    }
    try {
      return await request();
    } catch (error) {
      lastProblem = asProblem(error);
      if (!lastProblem.retryable || delayMs === readRetryDelaysMs.at(-1)) {
        throw lastProblem;
      }
    }
  }
  throw lastProblem;
}

export async function savePlanDraft(
  plan: AssistantPlan,
  summary: string,
  operations: AssistantPlanOperationInput[],
): Promise<AssistantPlan> {
  return (
    await mutate(
      (headers) =>
        updateAssistantPlanDraft({
          path: { planRef: plan.ref },
          body: { summary, operations },
          headers: {
            "Idempotency-Key": headers["Idempotency-Key"],
            "X-CSRF-Token": headers["X-CSRF-Token"],
            "If-Match": headers["If-Match"] ?? "",
          },
          signal: requestSignal(),
        }),
      plan.version,
    )
  ).data;
}

export async function validatePlanDraft(
  plan: AssistantPlan,
): Promise<AssistantPlan> {
  return (
    await mutate(
      (headers) =>
        validateAssistantPlan({
          path: { planRef: plan.ref },
          body: { revision: plan.revision },
          headers: {
            "Idempotency-Key": headers["Idempotency-Key"],
            "X-CSRF-Token": headers["X-CSRF-Token"],
            "If-Match": headers["If-Match"] ?? "",
          },
          signal: requestSignal(),
        }),
      plan.version,
    )
  ).data;
}

export async function applyPlanDraft(
  plan: AssistantPlan,
): Promise<AssistantPlanApplicationResponse> {
  return (
    await mutate(
      (headers) =>
        applyAssistantPlan({
          path: { planRef: plan.ref },
          body: { revision: plan.revision },
          headers: {
            "Idempotency-Key": headers["Idempotency-Key"],
            "X-CSRF-Token": headers["X-CSRF-Token"],
            "If-Match": headers["If-Match"] ?? "",
          },
          signal: requestSignal(),
        }),
      plan.version,
    )
  ).data;
}

export async function rejectPlanDraft(
  plan: AssistantPlan,
): Promise<AssistantPlanDecisionResponse> {
  return (
    await mutate(
      (headers) =>
        rejectAssistantPlan({
          path: { planRef: plan.ref },
          body: { revision: plan.revision },
          headers: {
            "Idempotency-Key": headers["Idempotency-Key"],
            "X-CSRF-Token": headers["X-CSRF-Token"],
            "If-Match": headers["If-Match"] ?? "",
          },
          signal: requestSignal(),
        }),
      plan.version,
    )
  ).data;
}
