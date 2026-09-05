import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { AssistantConversation } from "@/shared/api/generated/openapi/types.gen";

const mocks = vi.hoisted(() => ({
  addAssistantTurn: vi.fn(),
  getSystemAssistant: vi.fn(),
  listAssistantConversations: vi.fn(),
  signal: new AbortController().signal,
}));

vi.mock("@/shared/api/client", () => ({
  requestSignal: (signal?: AbortSignal) => signal ?? mocks.signal,
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
import { appendTurn, readConversations } from "@/features/assistant/api";

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
  beforeEach(() => {
    vi.clearAllMocks();
    vi.stubGlobal("document", {
      cookie: `__Host-kodex-csrf=${"a".repeat(43)}`,
    });
    vi.stubGlobal("crypto", {
      randomUUID: () => "stable-assistant-idempotency-key",
    });
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  it("передаёт project/cursor/pageSize и возвращает nextPageToken истории", async () => {
    const data = { items: [conversation([])], nextPageToken: "after" };
    mocks.listAssistantConversations.mockResolvedValue({ data });
    const signal = new AbortController().signal;
    expect(await readConversations("prj_sales", "before", signal)).toEqual(
      data,
    );
    expect(mocks.listAssistantConversations).toHaveBeenCalledWith({
      query: { projectRef: "prj_sales", pageToken: "before", pageSize: 40 },
      signal,
    });
  });

  it("не вызывает SDK для отменённого чтения истории", async () => {
    const controller = new AbortController();
    controller.abort();
    await expect(
      readConversations(undefined, undefined, controller.signal),
    ).rejects.toThrow();
    expect(mocks.listAssistantConversations).not.toHaveBeenCalled();
  });

  it("повторяет turn с теми же key и полным payload", async () => {
    vi.useFakeTimers();
    const initial = conversation([]);
    const ownResult = conversation([
      {
        ref: "trn_own",
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
    mocks.addAssistantTurn
      .mockRejectedValueOnce(uncertain)
      .mockResolvedValueOnce({ data: ownResult });

    const result = appendTurn(
      initial,
      "Измени назначение",
      "attachment-set-own",
    );
    await vi.runAllTimersAsync();
    await expect(result).resolves.toBe(ownResult);

    expect(mocks.addAssistantTurn).toHaveBeenCalledTimes(2);
    expect(mocks.addAssistantTurn.mock.calls[0]?.[0]).toEqual(
      mocks.addAssistantTurn.mock.calls[1]?.[0],
    );
    expect(mocks.addAssistantTurn.mock.calls[1]?.[0]).toEqual({
      path: { conversationRef: "cnv_sales" },
      body: {
        content: "Измени назначение",
        attachmentSetRef: "attachment-set-own",
      },
      headers: {
        "Idempotency-Key": "stable-assistant-idempotency-key",
        "X-CSRF-Token": "a".repeat(43),
      },
      signal: mocks.signal,
    });
    expect(mocks.listAssistantConversations).not.toHaveBeenCalled();
  });

  it("не принимает совпавший turn из параллельной вкладки", async () => {
    vi.useFakeTimers();
    const initial = conversation([]);
    const uncertain = Object.assign(new Error("response body was lost"), {
      retryable: true,
    });
    mocks.addAssistantTurn.mockRejectedValue(uncertain);
    mocks.listAssistantConversations.mockResolvedValue({
      data: {
        items: [
          conversation([
            {
              ref: "trn_parallel_tab",
              sequence: 1,
              role: "USER",
              content: "Измени назначение",
              state: "QUEUED",
              createdAt: "2026-09-02T00:00:01Z",
            },
          ]),
        ],
      },
    });

    const result = appendTurn(initial, "Измени назначение", "attachment-own");
    const rejected = expect(result).rejects.toBe(uncertain);
    await vi.runAllTimersAsync();
    await rejected;

    expect(mocks.addAssistantTurn).toHaveBeenCalledTimes(3);
    expect(mocks.listAssistantConversations).not.toHaveBeenCalled();
  });
});
