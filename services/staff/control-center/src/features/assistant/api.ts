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
import { unwrap } from "@/shared/api/problem";

export async function readAssistant(): Promise<SystemAssistant> {
  return (await unwrap(getSystemAssistant({ signal: requestSignal() }))).data;
}

export async function readConversations(
  projectRef?: string,
): Promise<AssistantConversation[]> {
  return (
    await unwrap(
      listAssistantConversations({
        ...(projectRef ? { query: { projectRef } } : {}),
        signal: requestSignal(),
      }),
    )
  ).data.items;
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
  conversationRef: string,
  content: string,
  artifactRefs: string[] = [],
): Promise<AssistantConversation> {
  return (
    await mutate((headers) =>
      addAssistantTurn({
        path: { conversationRef },
        body: { content, ...(artifactRefs.length ? { artifactRefs } : {}) },
        headers: {
          "Idempotency-Key": headers["Idempotency-Key"],
          "X-CSRF-Token": headers["X-CSRF-Token"],
        },
        signal: requestSignal(),
      }),
    )
  ).data;
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
