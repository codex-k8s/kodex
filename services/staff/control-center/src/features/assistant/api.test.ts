import { beforeEach, describe, expect, it, vi } from "vitest";

import type { AssistantConversation } from "@/shared/api/generated/openapi/types.gen";

const mocks = vi.hoisted(() => ({
  addAssistantTurn: vi.fn(),
  getSystemAssistant: vi.fn(),
  listAssistantConversations: vi.fn(),
  signal: new AbortController().signal,
}));

vi.mock("@/shared/api/client", () => ({
  requestSignal: () => mocks.signal,
}));
vi.mock("@/shared/api/generated/openapi/sdk.gen", () => ({
  addAssistantTurn: mocks.addAssistantTurn,
  applyAssistantPlan: vi.fn(),
  createAssistantConversation: vi.fn(),
  getSystemAssistant: mocks.getSystemAssistant,
  listAssistantConversations: mocks.listAssistantConversations,
  rejectAssistantPlan: vi.fn(),
  updateAssistantConversationTitle: vi.fn(),
  updateAssistantPlanDraft: vi.fn(),
  validateAssistantPlan: vi.fn(),
}));
vi.mock("@/shared/api/problem", () => ({
  asProblem: (error: unknown) => error,
  unwrap: (value: unknown) => Promise.resolve(value),
}));
vi.mock("@/shared/api/mutation", () => ({
  mutate: (request: (headers: Record<string, string>) => Promise<unknown>) =>
    request({
      "Idempotency-Key": "test-idempotency",
      "X-CSRF-Token": "test-csrf",
    }),
}));

import { appendTurn } from "@/features/assistant/api";

function conversation(
  turns: AssistantConversation["turns"],
): AssistantConversation {
  return {
    ref: "cnv_sales",
    version: 2,
    title: "Продажи",
    titleSource: "USER_EDITED",
    titleRevision: 1,
    context: {
      route: "/projects/prj_sales",
      entityKind: "PROJECT",
      entityRef: "prj_sales",
      entityName: "Продажи",
      entityVersion: 1,
      allowedOperations: ["CHANGE_CAPABILITY"],
    },
    projectRef: "prj_sales",
    turns,
    updatedAt: "2026-09-02T00:00:00Z",
  };
}

describe("assistant api mutation reconciliation", () => {
  beforeEach(() => vi.clearAllMocks());

  it("принимает только один новый авторитетный USER-turn", async () => {
    const initial = conversation([]);
    const authoritative = conversation([
      {
        ref: "trn_user",
        sequence: 1,
        role: "USER",
        content: "Измени назначение",
        state: "QUEUED",
        createdAt: "2026-09-02T00:00:01Z",
      },
    ]);
    const uncertain = Object.assign(new Error("response body was lost"), {
      retryable: true,
    });
    mocks.addAssistantTurn.mockRejectedValueOnce(uncertain);
    mocks.listAssistantConversations.mockResolvedValueOnce({
      data: { items: [authoritative] },
    });

    await expect(appendTurn(initial, "Измени назначение")).resolves.toBe(
      authoritative,
    );
    expect(mocks.addAssistantTurn).toHaveBeenCalledTimes(1);
    expect(mocks.listAssistantConversations).toHaveBeenCalledTimes(1);
  });

  it("не скрывает неопределённый ответ без точного нового turn", async () => {
    const initial = conversation([]);
    const uncertain = Object.assign(new Error("response body was lost"), {
      retryable: true,
    });
    mocks.addAssistantTurn.mockRejectedValueOnce(uncertain);
    mocks.listAssistantConversations.mockResolvedValueOnce({
      data: { items: [initial] },
    });

    await expect(appendTurn(initial, "Измени назначение")).rejects.toBe(
      uncertain,
    );
    expect(mocks.addAssistantTurn).toHaveBeenCalledTimes(1);
  });
});
