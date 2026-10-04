import { requestSignal } from "@/shared/api/client";
import {
  addAssistantTurn,
  archiveAssistantConversation,
  applyAssistantPlan,
  cancelAssistantTurn as cancelAssistantTurnRequest,
  createAssistantConversation,
  createProjectAssistant,
  getProjectAssistant,
  getAgent,
  getSystemAssistant,
  listAssistantConversations,
  moveAssistantConversationToProject,
  purgeAssistantConversation,
  rejectAssistantPlan,
  restoreAssistantConversation,
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
  ProjectAssistantProfile,
  Agent,
  AssistantScope,
  ListAssistantConversationsResponse,
} from "@/shared/api/generated/openapi/types.gen";
import { mutate, mutateWithRetry } from "@/shared/api/mutation";
import { unwrap } from "@/shared/api/problem";
import { readWithRetry } from "@/shared/api/read-retry";

export async function readAssistant(
  signal?: AbortSignal,
): Promise<SystemAssistant> {
  return readWithRetry(
    async () =>
      (await unwrap(getSystemAssistant({ signal: requestSignal(signal) })))
        .data,
    undefined,
    signal,
  );
}

export async function readProjectAssistant(
  projectRef: string,
  signal?: AbortSignal,
): Promise<ProjectAssistantProfile> {
  const profile = await readWithRetry(
    async () =>
      (
        await unwrap(
          getProjectAssistant({
            path: { projectRef },
            signal: requestSignal(signal),
          }),
        )
      ).data,
    undefined,
    signal,
  );
  if (profile.projectRef !== projectRef)
    throw new Error("Project assistant profile scope mismatch");
  return profile;
}

export async function readProjectAssistantAgent(
  profile: ProjectAssistantProfile,
  signal?: AbortSignal,
): Promise<Agent> {
  const agent = await readWithRetry(
    async () =>
      (
        await unwrap(
          getAgent({
            path: { agentRef: profile.agentRef },
            signal: requestSignal(signal),
          }),
        )
      ).data,
    undefined,
    signal,
  );
  if (agent.ref !== profile.agentRef || agent.projectRef !== profile.projectRef)
    throw new Error("Project assistant agent scope mismatch");
  return agent;
}

export async function createProjectAssistantProfile(
  projectRef: string,
  input: { name: string; purpose: string; instructions: string },
): Promise<ProjectAssistantProfile> {
  const profile = (
    await mutate((headers) =>
      createProjectAssistant({
        path: { projectRef },
        body: input,
        headers: {
          "Idempotency-Key": headers["Idempotency-Key"],
          "X-CSRF-Token": headers["X-CSRF-Token"],
        },
        signal: requestSignal(),
      }),
    )
  ).data;
  if (profile.projectRef !== projectRef)
    throw new Error("Created project assistant scope mismatch");
  return profile;
}

export async function readConversations(
  projectRef?: string,
  pageToken?: string,
  signal?: AbortSignal,
  filter: {
    query?: string;
    state?: AssistantConversation["state"];
    assistantScope?: AssistantScope;
    assistantRef?: string;
  } = {},
  pageSize = 40,
): Promise<ListAssistantConversationsResponse> {
  return readWithRetry(
    async () =>
      (
        await unwrap(
          listAssistantConversations({
            query: {
              pageSize,
              assistantScope: filter.assistantScope ?? "SYSTEM",
              ...(filter.assistantRef
                ? { assistantRef: filter.assistantRef }
                : {}),
              ...(projectRef ? { projectRef } : {}),
              ...(pageToken ? { pageToken } : {}),
              ...(filter.query?.trim() ? { query: filter.query.trim() } : {}),
              ...(filter.state ? { state: filter.state } : {}),
            },
            signal: requestSignal(signal),
          }),
        )
      ).data,
    undefined,
    signal,
  );
}

export async function archiveConversation(
  conversation: AssistantConversation,
): Promise<AssistantConversation> {
  const result = (
    await mutate(
      (headers) =>
        archiveAssistantConversation({
          path: { conversationRef: conversation.ref },
          headers: {
            "If-Match": headers["If-Match"] ?? "",
            "Idempotency-Key": headers["Idempotency-Key"],
            "X-CSRF-Token": headers["X-CSRF-Token"],
          },
          signal: requestSignal(),
        }),
      conversation.version,
    )
  ).data;
  if (
    result.ref !== conversation.ref ||
    result.projectRef !== conversation.projectRef ||
    result.state !== "ARCHIVED" ||
    result.version <= conversation.version
  )
    throw new Error("Assistant archive receipt mismatch");
  return result;
}

export async function restoreConversation(
  conversation: AssistantConversation,
): Promise<AssistantConversation> {
  const result = (
    await mutate(
      (headers) =>
        restoreAssistantConversation({
          path: { conversationRef: conversation.ref },
          headers: {
            "If-Match": headers["If-Match"] ?? "",
            "Idempotency-Key": headers["Idempotency-Key"],
            "X-CSRF-Token": headers["X-CSRF-Token"],
          },
          signal: requestSignal(),
        }),
      conversation.version,
    )
  ).data;
  if (
    result.ref !== conversation.ref ||
    result.projectRef !== conversation.projectRef ||
    result.state !== "ACTIVE" ||
    result.version <= conversation.version
  )
    throw new Error("Assistant restore receipt mismatch");
  return result;
}

export async function purgeConversation(
  conversation: AssistantConversation,
): Promise<void> {
  await mutate(
    (headers) =>
      purgeAssistantConversation({
        path: { conversationRef: conversation.ref },
        headers: {
          "If-Match": headers["If-Match"] ?? "",
          "Idempotency-Key": headers["Idempotency-Key"],
          "X-CSRF-Token": headers["X-CSRF-Token"],
        },
        signal: requestSignal(),
      }),
    conversation.version,
  );
}

export async function moveConversationToProject(
  conversation: AssistantConversation,
  projectRef: string,
): Promise<AssistantConversation> {
  const result = (
    await mutate(
      (headers) =>
        moveAssistantConversationToProject({
          path: { conversationRef: conversation.ref },
          body: { projectRef },
          headers: {
            "If-Match": headers["If-Match"] ?? "",
            "Idempotency-Key": headers["Idempotency-Key"],
            "X-CSRF-Token": headers["X-CSRF-Token"],
          },
          signal: requestSignal(),
        }),
      conversation.version,
    )
  ).data;
  if (
    result.ref !== conversation.ref ||
    result.projectRef !== projectRef ||
    result.version !== conversation.version + 1
  )
    throw new Error("Assistant project move receipt mismatch");
  return result;
}

export async function createConversation(
  context: AssistantContextDescriptor,
  projectRef?: string,
  assistantScope: AssistantScope = "SYSTEM",
): Promise<AssistantConversation> {
  return (
    await mutate((headers) =>
      createAssistantConversation({
        body: {
          context,
          assistantScope,
          ...(projectRef ? { projectRef } : {}),
        },
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
  context: AssistantContextDescriptor,
  attachmentSetRef?: string,
  deliveryMode: "QUEUE" | "INTERRUPT_ACTIVE" = "QUEUE",
): Promise<AssistantConversation> {
  return (
    await mutateWithRetry((headers) =>
      addAssistantTurn({
        path: { conversationRef: conversation.ref },
        body: {
          content,
          context,
          deliveryMode,
          ...(attachmentSetRef ? { attachmentSetRef } : {}),
        },
        headers: {
          "Idempotency-Key": headers["Idempotency-Key"],
          "X-CSRF-Token": headers["X-CSRF-Token"],
        },
        signal: requestSignal(),
      }),
    )
  ).data;
}

export async function cancelAssistantTurn(
  conversation: AssistantConversation,
): Promise<string> {
  const result = await mutate(
    (headers) =>
      cancelAssistantTurnRequest({
        path: { conversationRef: conversation.ref },
        headers: {
          "If-Match": headers["If-Match"] ?? "",
          "Idempotency-Key": headers["Idempotency-Key"],
          "X-CSRF-Token": headers["X-CSRF-Token"],
        },
        signal: requestSignal(),
      }),
    conversation.version,
  );
  if (result.data.conversationRef !== conversation.ref)
    throw new Error("Assistant cancellation receipt mismatch");
  return result.data.runRef;
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
