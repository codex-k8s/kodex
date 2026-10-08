import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  createRenderer,
  defineComponent,
  nextTick,
  reactive,
  ssrContextKey,
  type Ref,
  type SetupContext,
} from "vue";
import { createI18n } from "vue-i18n";
import { AppProblem } from "@/shared/api/problem";
import type { ProviderAccount } from "./model";
import type { ProviderLifecycleResult } from "./lifecycle";
import {
  readProviderLifecycleAttempt,
  rememberProviderLifecycleAttempt,
  type ProviderLifecycleAttempt,
} from "./lifecycle-attempt";

const api = vi.hoisted(() => ({ retryProviderLifecycle: vi.fn() }));
vi.mock("./lifecycle", () => api);
import ProviderLifecycleRecovery from "./ProviderLifecycleRecovery.vue";

function account(): ProviderAccount {
  return {
    ref: "pacc_synthetic",
    version: 9,
    definitionKey: "openai-codex",
    name: "Проверка восстановления",
    externalAccountMasked: "",
    state: "DELETING",
    enabled: false,
    ready: false,
    maximumConcurrentExecutions: 10,
    nextActions: [],
    createdAt: "2026-09-01T00:00:00Z",
    updatedAt: "2026-09-01T00:00:00Z",
  };
}
const original: ProviderLifecycleAttempt = {
  accountRef: "pacc_synthetic",
  version: 3,
  key: "11111111-1111-4111-8111-111111111111",
  action: "DELETE",
};
const result: ProviderLifecycleResult = {
  account: { ...account(), state: "DELETED", version: 10 },
};
type State = {
  attempt: Ref<ProviderLifecycleAttempt | undefined>;
  recoveryProblem: Ref<AppProblem | undefined>;
  busy: Ref<boolean>;
  retry(): Promise<void>;
};
const source = ProviderLifecycleRecovery as unknown as {
  setup(props: object, context: SetupContext): State;
};
const disposers: (() => void)[] = [];
const settleRequests: (() => void)[] = [];
function deferred() {
  let resolve!: (value: ProviderLifecycleResult) => void;
  let reject!: (error: unknown) => void;
  const promise = new Promise<ProviderLifecycleResult>((yes, no) => {
    resolve = yes;
    reject = no;
  });
  settleRequests.push(() => resolve(result));
  return { promise, resolve, reject };
}
function storage(): Storage {
  const values = new Map<string, string>();
  return {
    get length() {
      return values.size;
    },
    key: (index) => [...values.keys()][index] ?? null,
    clear: () => values.clear(),
    getItem: (key) => values.get(key) ?? null,
    setItem: (key, value) => {
      values.set(key, value);
    },
    removeItem: (key) => {
      values.delete(key);
    },
  };
}
function mount(problem?: unknown) {
  const data = storage();
  rememberProviderLifecycleAttempt(original, data);
  vi.stubGlobal("window", { sessionStorage: data });
  const props = reactive({ account: account(), problem });
  const pending = vi.fn();
  const recovered = vi.fn();
  let state!: State;
  const renderer = createRenderer<object, object>({
    insert() {},
    remove() {},
    createElement: () => ({}),
    createText: () => ({}),
    createComment: () => ({}),
    setText() {},
    setElementText() {},
    parentNode: () => null,
    nextSibling: () => null,
    patchProp() {},
  });
  const app = renderer.createApp(
    defineComponent({
      emits: ["pending", "recovered"],
      setup(_props, context) {
        state = source.setup(props, context);
        return () => null;
      },
    }),
    { onPending: pending, onRecovered: recovered },
  );
  app.use(createI18n({ legacy: false, locale: "ru", messages: { ru: {} } }));
  app.provide(ssrContextKey, {});
  app.mount({});
  let mounted = true;
  const unmount = () => {
    if (!mounted) return;
    mounted = false;
    app.unmount();
  };
  disposers.push(unmount);
  function signal(): AbortSignal {
    const value: unknown = api.retryProviderLifecycle.mock.calls.at(-1)?.[2];
    if (!(value instanceof AbortSignal))
      throw new Error("Recovery signal missing");
    return value;
  }
  return { props, state, data, pending, recovered, signal, unmount };
}

beforeEach(() => vi.resetAllMocks());
afterEach(async () => {
  for (const dispose of disposers.splice(0)) dispose();
  for (const settle of settleRequests.splice(0)) settle();
  await nextTick();
  vi.unstubAllGlobals();
});

describe("ProviderLifecycleRecovery", () => {
  it("сохраняет ожидающий retry при замене account с прежними ref и problem", async () => {
    const request = deferred();
    api.retryProviderLifecycle.mockReturnValueOnce(request.promise);
    const { props, state, data, pending, recovered, signal } = mount();
    expect(pending).toHaveBeenCalledExactlyOnceWith(true);
    expect(state.attempt.value).toEqual(original);
    const retry = state.retry();
    const activeSignal = signal();
    props.account = { ...props.account, version: 10, ready: true };
    await nextTick();
    expect(activeSignal.aborted).toBe(false);
    expect(state.busy.value).toBe(true);
    expect(state.attempt.value).toEqual(original);
    expect(pending).toHaveBeenCalledOnce();
    await state.retry();
    expect(api.retryProviderLifecycle).toHaveBeenCalledExactlyOnceWith(
      original,
      data,
      activeSignal,
    );
    request.resolve(result);
    await retry;
    expect(recovered).toHaveBeenCalledExactlyOnceWith(result);
    expect(pending.mock.calls).toEqual([[true], [false]]);
    expect(state.attempt.value).toBeUndefined();
    expect(state.busy.value).toBe(false);
    await state.retry();
    expect(api.retryProviderLifecycle).toHaveBeenCalledOnce();
    expect(recovered).toHaveBeenCalledOnce();
  });

  it.each(["account", "problem"] as const)(
    "изменение %s отменяет запрос и принимает только новый явный retry",
    async (identity) => {
      const request = deferred();
      api.retryProviderLifecycle.mockReturnValueOnce(request.promise);
      const { props, state, data, pending, recovered, signal } = mount();
      const retry = state.retry();
      const activeSignal = signal();
      const nextAttempt = { ...original, accountRef: "pacc_next_fixture" };
      if (identity === "account") {
        rememberProviderLifecycleAttempt(nextAttempt, data);
        props.account = { ...props.account, ref: nextAttempt.accountRef };
      } else props.problem = new Error("Changed recovery context");
      await nextTick();
      expect(activeSignal.aborted).toBe(true);
      expect(state.busy.value).toBe(false);
      const currentAttempt = identity === "account" ? nextAttempt : original;
      expect(state.attempt.value).toEqual(currentAttempt);
      expect(pending.mock.calls).toEqual([[true], [true]]);
      expect(api.retryProviderLifecycle).toHaveBeenCalledOnce();
      const nextRequest = deferred();
      api.retryProviderLifecycle.mockReturnValueOnce(nextRequest.promise);
      const nextRetry = state.retry();
      const currentSignal = signal();
      expect(currentSignal).not.toBe(activeSignal);
      request.resolve(result);
      await retry;
      expect(recovered).not.toHaveBeenCalled();
      expect(state.busy.value).toBe(true);
      expect(state.attempt.value).toEqual(currentAttempt);
      const nextResult = {
        account: { ...result.account, ref: currentAttempt.accountRef },
      };
      nextRequest.resolve(nextResult);
      await nextRetry;
      expect(recovered).toHaveBeenCalledExactlyOnceWith(nextResult);
      expect(pending.mock.calls).toEqual([[true], [true], [false]]);
    },
  );

  it.each(["account", "problem"] as const)(
    "изменение %s игнорирует позднюю ошибку старого retry",
    async (identity) => {
      const request = deferred();
      api.retryProviderLifecycle.mockReturnValueOnce(request.promise);
      const { props, state, data, pending, recovered, signal } = mount();
      const retry = state.retry();
      const activeSignal = signal();
      if (identity === "account")
        props.account = { ...props.account, ref: "pacc_next_fixture" };
      else props.problem = new Error("Changed recovery context");
      await nextTick();
      expect(activeSignal.aborted).toBe(true);
      request.reject(new Error("Stale recovery failure"));
      await retry;
      expect(state.recoveryProblem.value).toBeUndefined();
      expect(state.busy.value).toBe(false);
      expect(recovered).not.toHaveBeenCalled();
      expect(pending.mock.calls).toEqual([[true], [identity === "problem"]]);
      expect(readProviderLifecycleAttempt(original.accountRef, data)).toEqual(
        original,
      );
    },
  );

  it("сохраняет UNKNOWN intent и ошибку при замене account и повторяет прежний key только явно", async () => {
    const request = deferred();
    api.retryProviderLifecycle.mockReturnValueOnce(request.promise);
    const { props, state, data, pending, recovered, signal } = mount();
    const retry = state.retry();
    const activeSignal = signal();
    request.reject(new Error("Lost recovery response"));
    await retry;
    const problem = state.recoveryProblem.value;
    expect(problem?.code).toBe("UNKNOWN");
    expect(state.attempt.value).toEqual(original);
    expect(readProviderLifecycleAttempt(original.accountRef, data)).toEqual(
      original,
    );
    props.account = { ...props.account, version: 11 };
    await nextTick();
    expect(activeSignal.aborted).toBe(false);
    expect(state.recoveryProblem.value).toBe(problem);
    expect(api.retryProviderLifecycle).toHaveBeenCalledOnce();
    expect(pending).toHaveBeenCalledExactlyOnceWith(true);
    expect(recovered).not.toHaveBeenCalled();
    api.retryProviderLifecycle.mockResolvedValueOnce(result);
    await state.retry();
    expect(api.retryProviderLifecycle.mock.calls[1]?.[0]).toEqual(original);
    expect(recovered).toHaveBeenCalledExactlyOnceWith(result);
    expect(pending.mock.calls).toEqual([[true], [false]]);
  });

  it.each(["success", "error"] as const)(
    "unmount отменяет retry и игнорирует поздний %s, сохраняя intent",
    async (outcome) => {
      const request = deferred();
      api.retryProviderLifecycle.mockReturnValueOnce(request.promise);
      const { state, data, pending, recovered, signal, unmount } = mount();
      const retry = state.retry();
      const activeSignal = signal();
      unmount();
      expect(activeSignal.aborted).toBe(true);
      if (outcome === "success") request.resolve(result);
      else request.reject(new Error("Unmounted recovery failure"));
      await retry;
      expect(recovered).not.toHaveBeenCalled();
      expect(pending).toHaveBeenCalledExactlyOnceWith(true);
      expect(state.recoveryProblem.value).toBeUndefined();
      expect(readProviderLifecycleAttempt(original.accountRef, data)).toEqual(
        original,
      );
      expect(api.retryProviderLifecycle).toHaveBeenCalledOnce();
    },
  );
});
