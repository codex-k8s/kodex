import { effectScope, nextTick, ref } from "vue";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import {
  toExecutionTargetOption,
  type ExecutionTargetPickerOption,
  type ExecutionTargetType,
} from "@/shared/api/execution-target-picker";
import type { Agent, Workflow } from "@/shared/api/generated/openapi/types.gen";
import { watchAssistantExecutionTarget } from "./execution-target-readback";

type TargetReadRequest = {
  path: { agentRef?: string; workflowRef?: string };
  signal: AbortSignal;
};
type TargetReadResponse = { data: Agent | Workflow; response: Response };
const api = vi.hoisted(() => ({
  getAgent: vi.fn<(input: TargetReadRequest) => Promise<TargetReadResponse>>(),
  getWorkflow:
    vi.fn<(input: TargetReadRequest) => Promise<TargetReadResponse>>(),
}));
vi.mock("@/shared/api/generated/openapi/sdk.gen", () => api);
vi.mock("@/shared/api/client", () => ({
  requestSignal: (signal: AbortSignal) => signal,
}));

const scopes: ReturnType<typeof effectScope>[] = [];
beforeEach(() => vi.resetAllMocks());
afterEach(() => scopes.splice(0).forEach((scope) => scope.stop()));

function candidate(type: ExecutionTargetType, overrides: object = {}) {
  return {
    ref: "target_one",
    projectRef: "project_one",
    name: "Анализ обращения",
    purpose: "Проверяет исходные данные",
    version: 2,
    enabled: true,
    system: false,
    state: type === "AGENT" ? "READY" : "PUBLISHED",
    nextActions: ["LAUNCH"],
    ...overrides,
  } as Agent | Workflow;
}

function response(data: Agent | Workflow) {
  return { data, response: new Response(null, { status: 200 }) };
}

function pendingRead() {
  let resolve!: (value: ReturnType<typeof response>) => void;
  let reject!: (reason: Error) => void;
  const promise = new Promise<ReturnType<typeof response>>((accept, fail) => {
    resolve = accept;
    reject = fail;
  });
  return { promise, resolve, reject };
}

function fixture(
  type: ExecutionTargetType,
  initial?: ExecutionTargetPickerOption,
) {
  const scope = effectScope();
  scopes.push(scope);
  const project = ref<string | undefined>("project_one");
  const kind = ref<ExecutionTargetType | undefined>(type);
  const targetRef = ref("target_one");
  const selected = ref<ExecutionTargetPickerOption | undefined>(initial);
  const problem = ref(false);
  scope.run(() =>
    watchAssistantExecutionTarget(
      [project, kind, targetRef],
      selected,
      problem,
      "Target readback mismatch",
    ),
  );
  return { scope, project, kind, targetRef, selected, problem };
}

describe("чтение цели в формах запуска и расписания помощника", () => {
  it.each(["AGENT", "WORKFLOW"] as const)(
    "читает точную разрешённую цель %s своим endpoint",
    async (kind) => {
      const target = candidate(kind);
      const reader = kind === "AGENT" ? api.getAgent : api.getWorkflow;
      reader.mockResolvedValue(response(target));
      const state = fixture(kind);
      await vi.waitFor(() =>
        expect(state.selected.value?.ref).toBe(target.ref),
      );
      expect(state.selected.value?.targetType).toBe(kind);
      expect(state.problem.value).toBe(false);
      const request = reader.mock.calls[0]?.[0];
      expect(request?.path).toEqual(
        kind === "AGENT"
          ? { agentRef: target.ref }
          : { workflowRef: target.ref },
      );
      expect(request?.signal).toBeInstanceOf(AbortSignal);
      expect(
        kind === "AGENT" ? api.getWorkflow : api.getAgent,
      ).not.toHaveBeenCalled();
    },
  );

  it.each([
    ["AGENT", { ref: "other" }],
    ["AGENT", { projectRef: "other" }],
    ["AGENT", { enabled: false }],
    ["AGENT", { system: true }],
    ["AGENT", { nextActions: [] }],
    ["WORKFLOW", { state: "DRAFT" }],
    ["WORKFLOW", { nextActions: [] }],
  ] as const)(
    "отклоняет %s с несовпадением или отсутствием LAUNCH: %j",
    async (kind, changes) => {
      (kind === "AGENT" ? api.getAgent : api.getWorkflow).mockResolvedValue(
        response(candidate(kind, changes)),
      );
      const state = fixture(kind);
      await vi.waitFor(() => expect(state.problem.value).toBe(true));
      expect(state.selected.value).toBeUndefined();
    },
  );

  it("сохраняет уже выбранную точную цель без повторного чтения", () => {
    const option = toExecutionTargetOption("AGENT", candidate("AGENT"));
    const state = fixture("AGENT", option);
    expect(state.selected.value?.ref).toBe(option.ref);
    expect(api.getAgent).not.toHaveBeenCalled();
  });

  it.each(["resolve", "reject"] as const)(
    "при смене ref отменяет прежний запрос и игнорирует поздний %s",
    async (outcome) => {
      const old = pendingRead();
      const current = pendingRead();
      api.getAgent
        .mockReturnValueOnce(old.promise)
        .mockReturnValueOnce(current.promise);
      const state = fixture("AGENT");
      const previousSignal = api.getAgent.mock.calls[0]?.[0].signal;
      state.targetRef.value = "target_two";
      await nextTick();
      expect(previousSignal?.aborted).toBe(true);
      current.resolve(response(candidate("AGENT", { ref: "target_two" })));
      await vi.waitFor(() =>
        expect(state.selected.value?.ref).toBe("target_two"),
      );
      if (outcome === "resolve") old.resolve(response(candidate("AGENT")));
      else old.reject(new Error("Old read failed"));
      await nextTick();
      await nextTick();
      expect(state.selected.value?.ref).toBe("target_two");
      expect(state.problem.value).toBe(false);
    },
  );

  it("при удалении проекта очищает цель и ошибку, не начиная новый запрос", async () => {
    api.getAgent.mockRejectedValue(new Error("Read failed"));
    const state = fixture("AGENT");
    await vi.waitFor(() => expect(state.problem.value).toBe(true));
    state.project.value = undefined;
    await nextTick();
    expect(state.selected.value).toBeUndefined();
    expect(state.problem.value).toBe(false);
    expect(api.getAgent).toHaveBeenCalledTimes(1);
  });

  it("закрытие формы отменяет запрос и не принимает поздний ответ", async () => {
    const pending = pendingRead();
    api.getAgent.mockReturnValue(pending.promise);
    const state = fixture("AGENT");
    const signal = api.getAgent.mock.calls[0]?.[0].signal;
    state.scope.stop();
    expect(signal?.aborted).toBe(true);
    pending.resolve(response(candidate("AGENT")));
    await nextTick();
    await nextTick();
    expect(state.selected.value).toBeUndefined();
    expect(state.problem.value).toBe(false);
  });
});
