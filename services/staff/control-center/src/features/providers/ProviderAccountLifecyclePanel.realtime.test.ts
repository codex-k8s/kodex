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
import type {
  ProviderAccount,
  ProviderAccountBlocker,
  ProviderAccountBlockerPage,
} from "@/shared/api/generated/openapi/types.gen";
import type { AppProblem } from "@/shared/api/problem";

const api = vi.hoisted(() => ({ loadProviderBlockers: vi.fn() }));
vi.mock("./lifecycle", async (importOriginal) => ({
  ...(await importOriginal<typeof import("./lifecycle")>()),
  ...api,
}));
import ProviderAccountLifecyclePanel from "./ProviderAccountLifecyclePanel.vue";

function account(): ProviderAccount & {
  deletion: NonNullable<ProviderAccount["deletion"]>;
} {
  return {
    ref: "pacc_synthetic",
    version: 9,
    definitionKey: "openai-codex",
    name: "Проверка зависимостей",
    externalAccountMasked: "",
    state: "DELETING",
    enabled: false,
    ready: false,
    maximumConcurrentExecutions: 10,
    nextActions: [],
    createdAt: "2026-09-01T00:00:00Z",
    updatedAt: "2026-09-01T00:00:00Z",
    deletion: {
      ref: "pdel_synthetic",
      version: 4,
      state: "PENDING_BLOCKERS",
      pendingCleanup: 0,
      requestedAt: "2026-09-01T00:00:00Z",
      safeReason: "WAITING_FOR_DEPENDENCIES",
      blockers: [
        { kind: "AGENT", total: 0 },
        { kind: "PROVIDER_POOL", total: 0 },
        { kind: "AUTOMATION", total: 0 },
        { kind: "ACTIVE_TURN", total: 0 },
        { kind: "QUEUED_TURN", total: 1 },
        { kind: "WARM_RUNTIME", total: 0 },
      ],
    },
  };
}
function page(value: ProviderAccount = account()): ProviderAccountBlockerPage {
  return {
    items: [
      {
        kind: "QUEUED_TURN",
        ref: "run_synthetic",
        version: 1,
        name: "Ожидающее выполнение",
        canCancel: true,
      },
    ],
    total: 1,
    hiddenCount: 0,
    contextDigest: "a".repeat(64),
    accountVersion: value.version,
    deletionIntentVersion: value.deletion?.version ?? 0,
  };
}
type State = {
  currentAccount: Ref<ProviderAccount | undefined>;
  page: Ref<ProviderAccountBlockerPage | undefined>;
  items: Ref<ProviderAccountBlocker[]>;
  selected: Ref<string[]>;
  loading: Ref<boolean>;
  problem: Ref<AppProblem | undefined>;
  confirmation: Ref<"DELETE" | "CANCEL_QUEUED" | undefined>;
};
const source = ProviderAccountLifecyclePanel as unknown as {
  setup(props: object, context: SetupContext): State;
};
const disposers: (() => void)[] = [];
const settleRequests: (() => void)[] = [];
function deferred() {
  let resolve!: (value: ProviderAccountBlockerPage) => void;
  let reject!: (error: unknown) => void;
  const promise = new Promise<ProviderAccountBlockerPage>((yes, no) => {
    resolve = yes;
    reject = no;
  });
  settleRequests.push(() => resolve(page()));
  return { promise, resolve, reject };
}
function mount() {
  const props = reactive({ account: account() });
  const updated = vi.fn();
  const unavailable = vi.fn();
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
      emits: ["updated", "unavailable"],
      setup(_props, context) {
        state = source.setup(props, context);
        return () => null;
      },
    }),
    { onUpdated: updated, onUnavailable: unavailable },
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
    const value: unknown = api.loadProviderBlockers.mock.calls.at(-1)?.[2];
    if (!(value instanceof AbortSignal))
      throw new Error("Blocker signal missing");
    return value;
  }
  return { props, state, updated, unavailable, signal, unmount };
}

beforeEach(() => vi.resetAllMocks());
afterEach(async () => {
  for (const dispose of disposers.splice(0)) dispose();
  for (const settle of settleRequests.splice(0)) settle();
  await nextTick();
});

describe("ProviderAccountLifecyclePanel realtime", () => {
  it("сохраняет запрос зависимостей при realtime-замене account с прежними pins", async () => {
    const request = deferred();
    api.loadProviderBlockers.mockReturnValueOnce(request.promise);
    const { props, state, signal, updated, unavailable } = mount();
    const activeSignal = signal();
    expect(state.loading.value).toBe(true);
    props.account = {
      ...props.account,
      deletion: { ...props.account.deletion },
    };
    await nextTick();
    expect(activeSignal.aborted).toBe(false);
    expect(api.loadProviderBlockers).toHaveBeenCalledOnce();
    expect(state.loading.value).toBe(true);
    request.resolve(page());
    await nextTick();
    expect(state.page.value).toEqual(page());
    expect(state.items.value).toEqual(page().items);
    expect(state.loading.value).toBe(false);
    expect(updated).toHaveBeenCalledExactlyOnceWith(props.account);
    expect(unavailable).not.toHaveBeenCalled();
  });

  it("сохраняет выбор и подтверждение после замены account с прежними pins", async () => {
    api.loadProviderBlockers.mockResolvedValueOnce(page());
    const { props, state, signal, updated } = mount();
    await nextTick();
    const activeSignal = signal();
    state.selected.value = ["run_synthetic"];
    state.confirmation.value = "CANCEL_QUEUED";
    props.account = {
      ...props.account,
      deletion: { ...props.account.deletion },
    };
    await nextTick();
    expect(activeSignal.aborted).toBe(false);
    expect(api.loadProviderBlockers).toHaveBeenCalledOnce();
    expect(state.selected.value).toEqual(["run_synthetic"]);
    expect(state.confirmation.value).toBe("CANCEL_QUEUED");
    expect(state.page.value).toEqual(page());
    expect(state.items.value).toEqual(page().items);
    expect(updated).toHaveBeenCalledOnce();
  });

  it.each(["ref", "version", "deletionVersion", "deletionState"] as const)(
    "изменение %s отменяет старый запрос и перечитывает зависимости",
    async (identity) => {
      const request = deferred();
      const nextRequest = deferred();
      api.loadProviderBlockers
        .mockReturnValueOnce(request.promise)
        .mockReturnValueOnce(nextRequest.promise);
      const { props, state, signal, updated, unavailable } = mount();
      const activeSignal = signal();
      state.selected.value = ["run_synthetic"];
      state.confirmation.value = "CANCEL_QUEUED";
      const next = {
        ...props.account,
        deletion: { ...props.account.deletion },
      };
      if (identity === "ref") next.ref = "pacc_next_fixture";
      else if (identity === "version") next.version++;
      else if (identity === "deletionVersion") next.deletion.version++;
      else next.deletion.state = "CLEANUP_QUEUED";
      props.account = next;
      await nextTick();
      const currentSignal = signal();
      expect(activeSignal.aborted).toBe(true);
      expect(currentSignal).not.toBe(activeSignal);
      expect(currentSignal.aborted).toBe(false);
      expect(api.loadProviderBlockers).toHaveBeenCalledTimes(2);
      expect(api.loadProviderBlockers.mock.calls[1]?.[0]).toBe(next.ref);
      expect(state.currentAccount.value).toEqual(next);
      expect(state.selected.value).toEqual([]);
      expect(state.confirmation.value).toBeUndefined();
      request.resolve(page());
      await nextTick();
      expect(state.page.value).toBeUndefined();
      expect(state.loading.value).toBe(true);
      expect(updated).not.toHaveBeenCalled();
      nextRequest.resolve(page(next));
      await nextTick();
      expect(state.page.value).toEqual(page(next));
      expect(state.loading.value).toBe(false);
      expect(updated).toHaveBeenCalledExactlyOnceWith(next);
      expect(unavailable).not.toHaveBeenCalled();
    },
  );

  it("смена версии игнорирует позднюю ошибку и отклоняет несовпадающую версию readback", async () => {
    const request = deferred();
    const nextRequest = deferred();
    api.loadProviderBlockers
      .mockReturnValueOnce(request.promise)
      .mockReturnValueOnce(nextRequest.promise);
    const { props, state, unavailable } = mount();
    props.account = { ...props.account, version: 10 };
    await nextTick();
    request.reject(new Error("Stale blocker failure"));
    await nextTick();
    expect(state.problem.value).toBeUndefined();
    expect(unavailable).not.toHaveBeenCalled();
    nextRequest.resolve(page());
    await nextTick();
    expect(state.page.value).toBeUndefined();
    expect(state.currentAccount.value).toBeUndefined();
    expect(state.problem.value).toBeDefined();
    expect(unavailable).toHaveBeenCalledExactlyOnceWith(props.account.ref);
  });

  it("unmount отменяет запрос и игнорирует поздний readback", async () => {
    const request = deferred();
    api.loadProviderBlockers.mockReturnValueOnce(request.promise);
    const { state, signal, unmount, updated, unavailable } = mount();
    const activeSignal = signal();
    unmount();
    expect(activeSignal.aborted).toBe(true);
    request.resolve(page());
    await nextTick();
    expect(state.page.value).toBeUndefined();
    expect(state.currentAccount.value).toBeUndefined();
    expect(updated).not.toHaveBeenCalled();
    expect(unavailable).not.toHaveBeenCalled();
  });
});
