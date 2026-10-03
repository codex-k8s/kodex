import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { AssistantConversation } from "@/shared/api/generated/openapi/types.gen";

const mocks = vi.hoisted(() => ({
  addAssistantTurn: vi.fn(),
  cancelAssistantTurn: vi.fn(),
  archiveAssistantConversation: vi.fn(),
  getSystemAssistant: vi.fn(),
  listAssistantConversations: vi.fn(),
  signal: new AbortController().signal,
}));

vi.mock("@/shared/api/client", () => ({
  requestSignal: (signal?: AbortSignal) => signal ?? mocks.signal,
}));
vi.mock("@/shared/api/generated/openapi/sdk.gen", () => ({
  addAssistantTurn: mocks.addAssistantTurn,
  cancelAssistantTurn: mocks.cancelAssistantTurn,
  archiveAssistantConversation: mocks.archiveAssistantConversation,
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
import {
  appendTurn,
  archiveConversation,
  cancelAssistantTurn,
  readConversations,
} from "@/features/assistant/api";

function conversation(
  turns: AssistantConversation["turns"],
): AssistantConversation {
  return {
    ref: "cnv_sales",
    assistantScope: "SYSTEM",
    assistantRef: "ast_system_assistant",
    state: "ACTIVE",
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
  it("передаёт серверный query/state вместе с cursor", async () => {
    mocks.listAssistantConversations.mockResolvedValue({ data: { items: [] } });
    await readConversations("prj_sales", "cursor", mocks.signal, {
      query: "  архив  ",
      state: "ARCHIVED",
    });
    expect(mocks.listAssistantConversations).toHaveBeenCalledWith({
      query: {
        assistantScope: "SYSTEM",
        projectRef: "prj_sales",
        pageToken: "cursor",
        pageSize: 40,
        query: "архив",
        state: "ARCHIVED",
      },
      signal: mocks.signal,
    });
  });
  it("архивирует с OCC и проверяет exact terminal receipt без retry", async () => {
    const source = conversation([]);
    const archived = { ...source, state: "ARCHIVED", version: 3 };
    mocks.archiveAssistantConversation.mockResolvedValue({ data: archived });
    expect(await archiveConversation(source)).toEqual(archived);
    expect(mocks.archiveAssistantConversation).toHaveBeenCalledWith(
      expect.objectContaining({
        path: { conversationRef: source.ref },
        headers: {
          "If-Match": '"2"',
          "Idempotency-Key": "stable-assistant-idempotency-key",
          "X-CSRF-Token": "a".repeat(43),
        },
      }),
    );
    mocks.archiveAssistantConversation.mockRejectedValue(new Error("Timeout"));
    await expect(archiveConversation(source)).rejects.toThrow("Timeout");
    expect(mocks.archiveAssistantConversation).toHaveBeenCalledTimes(2);
    mocks.archiveAssistantConversation.mockResolvedValue({ data: source });
    await expect(archiveConversation(source)).rejects.toThrow(
      "receipt mismatch",
    );
  });
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
      query: {
        assistantScope: "SYSTEM",
        projectRef: "prj_sales",
        pageToken: "before",
        pageSize: 40,
      },
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
      initial.context,
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
        context: initial.context,
        attachmentSetRef: "attachment-set-own",
        deliveryMode: "QUEUE",
      },
      headers: {
        "Idempotency-Key": "stable-assistant-idempotency-key",
        "X-CSRF-Token": "a".repeat(43),
      },
      signal: mocks.signal,
    });
    expect(mocks.listAssistantConversations).not.toHaveBeenCalled();
  });

  it("передаёт немедленный режим без расширения payload", async () => {
    const initial = conversation([]);
    const accepted = conversation([
      {
        ref: "trn_immediate",
        sequence: 1,
        role: "USER",
        content: "Сначала выполни это",
        state: "QUEUED",
        runRef: "run_immediate",
        createdAt: "2026-09-02T00:00:01Z",
      },
    ]);
    mocks.addAssistantTurn.mockResolvedValue({ data: accepted });

    await appendTurn(
      initial,
      "Сначала выполни это",
      initial.context,
      undefined,
      "INTERRUPT_ACTIVE",
    );

    expect(mocks.addAssistantTurn).toHaveBeenCalledWith(
      expect.objectContaining({
        path: { conversationRef: initial.ref },
        body: {
          content: "Сначала выполни это",
          context: initial.context,
          deliveryMode: "INTERRUPT_ACTIVE",
        },
      }),
    );
  });

  it("останавливает ход с OCC и проверяет точную квитанцию", async () => {
    const initial = conversation([]);
    mocks.cancelAssistantTurn.mockResolvedValue({
      data: {
        conversationRef: initial.ref,
        runRef: "run_active",
        cancelled: true,
      },
    });

    await expect(cancelAssistantTurn(initial)).resolves.toBe("run_active");
    expect(mocks.cancelAssistantTurn).toHaveBeenCalledWith({
      path: { conversationRef: initial.ref },
      headers: {
        "If-Match": '"2"',
        "Idempotency-Key": "stable-assistant-idempotency-key",
        "X-CSRF-Token": "a".repeat(43),
      },
      signal: mocks.signal,
    });

    mocks.cancelAssistantTurn.mockResolvedValue({
      data: {
        conversationRef: "cnv_other",
        runRef: "run_active",
        cancelled: true,
      },
    });
    await expect(cancelAssistantTurn(initial)).rejects.toThrow(
      "receipt mismatch",
    );
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

    const result = appendTurn(
      initial,
      "Измени назначение",
      initial.context,
      "attachment-own",
    );
    const rejected = expect(result).rejects.toBe(uncertain);
    await vi.runAllTimersAsync();
    await rejected;

    expect(mocks.addAssistantTurn).toHaveBeenCalledTimes(3);
    expect(mocks.listAssistantConversations).not.toHaveBeenCalled();
  });
});
