import { renderToString } from "@vue/server-renderer";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  createRenderer,
  createSSRApp,
  defineComponent,
  nextTick,
  reactive,
  ssrContextKey,
  type Ref,
  type SetupContext,
} from "vue";
import type {
  PromptTemplatePreview,
  Run,
} from "@/shared/api/generated/openapi/types.gen";
import { AppProblem } from "@/shared/api/problem";
import { createI18n } from "vue-i18n";

const api = vi.hoisted(() => ({ loadRunPromptPreview: vi.fn() }));
vi.mock("./run-prompt-preview", () => api);
import RunPromptPreview from "./RunPromptPreview.vue";

const preview: PromptTemplatePreview = {
  safePreview: "[protected input]",
  complete: true,
  diagnostics: [],
  templateRef: "preview_fixture",
  templateDigest: "a".repeat(64),
  materializationDigest: "b".repeat(64),
  effectiveCapabilities: [],
  serviceTemplateRevision: "prompt.v1",
  serviceTemplateDigest: "c".repeat(64),
  variableSnapshotDigest: "d".repeat(64),
  locale: "ru",
  slots: [{ source: "PLATFORM", slot: "INPUT", position: 1 }],
  sections: [
    { source: "PLATFORM", slot: "INPUT", content: "[protected input]" },
  ],
};
type RunSnapshot = Pick<
  Run,
  "ref" | "version" | "attempt" | "graphRevision" | "lastEventSequence"
>;
function run(): RunSnapshot {
  return {
    ref: "run_fixture",
    version: 2,
    attempt: 1,
    graphRevision: 10,
    lastEventSequence: 10,
  };
}
type State = {
  preview: Ref<PromptTemplatePreview | undefined>;
  problem: Ref<AppProblem | undefined>;
  loading: Ref<boolean>;
  opener: Ref<HTMLButtonElement | undefined>;
  refresh(): Promise<void>;
  invalidate(): void;
  closePreview(): Promise<void>;
};
const source = RunPromptPreview as unknown as {
  setup(props: object, context: SetupContext): State;
};
const disposers: (() => void)[] = [];
const settleRequests: (() => void)[] = [];
function deferred() {
  let resolve!: (value: PromptTemplatePreview) => void;
  let reject!: (error: unknown) => void;
  const promise = new Promise<PromptTemplatePreview>((yes, no) => {
    resolve = yes;
    reject = no;
  });
  settleRequests.push(() => resolve(preview));
  return { promise, resolve, reject };
}
function mount() {
  const props = reactive({ run: run() });
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
      setup(_props, context) {
        state = source.setup(props, context);
        return () => null;
      },
    }),
  );
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
    const value: unknown = api.loadRunPromptPreview.mock.calls.at(-1)?.[1];
    if (!(value instanceof AbortSignal))
      throw new Error("Preview signal missing");
    return value;
  }
  return { props, state, signal, unmount };
}

beforeEach(() => vi.resetAllMocks());
afterEach(async () => {
  for (const dispose of disposers.splice(0)) dispose();
  for (const settle of settleRequests.splice(0)) settle();
  await nextTick();
});

describe("RunPromptPreview", () => {
  it("сохраняет запрос при realtime-замене запуска с прежними pins", async () => {
    const pending = deferred();
    api.loadRunPromptPreview.mockReturnValueOnce(pending.promise);
    const { props, state, signal } = mount();
    const loading = state.refresh();
    const activeSignal = signal();
    props.run = {
      ...props.run,
      graphRevision: 11,
      lastEventSequence: 11,
    };
    await nextTick();
    expect(activeSignal.aborted).toBe(false);
    expect(state.loading.value).toBe(true);
    await state.refresh();
    expect(api.loadRunPromptPreview).toHaveBeenCalledExactlyOnceWith(
      "run_fixture",
      activeSignal,
    );
    pending.resolve(preview);
    await loading;
    expect(state.preview.value).toEqual(preview);
    expect(state.loading.value).toBe(false);
  });

  it("сохраняет полученный preview при realtime-замене с прежними pins", async () => {
    api.loadRunPromptPreview.mockResolvedValueOnce(preview);
    const { props, state } = mount();
    await state.refresh();
    props.run = { ...props.run, graphRevision: 12 };
    await nextTick();
    expect(state.preview.value).toEqual(preview);
    expect(api.loadRunPromptPreview).toHaveBeenCalledOnce();
  });

  it("показывает полученный контекст в штатной широкой модалке", async () => {
    api.loadRunPromptPreview.mockResolvedValueOnce(preview);
    const component = {
      ...RunPromptPreview,
      async setup(props: object, context: SetupContext) {
        const state = source.setup(props, context);
        await state.refresh();
        return state;
      },
    };
    const app = createSSRApp(component, { run: run() });
    app.use(
      createI18n({
        legacy: false,
        locale: "ru",
        messages: { ru: { promptContext: { preview: "Контекст исполнения" } } },
        missingWarn: false,
        fallbackWarn: false,
      }),
    );
    const html = await renderToString(app);
    expect(html).toMatch(/class="[^"]*\bmodal--xl\b[^"]*"/u);
    expect(html).toContain('role="dialog"');
    expect(html).toContain("Контекст исполнения");
    expect(html).toContain('class="prompt-context-details"');
    expect(html).toContain("preview_fixture");
  });

  it("закрывает полученный preview и открывает новый только по явному запросу", async () => {
    api.loadRunPromptPreview.mockResolvedValueOnce(preview);
    const { props, state } = mount();
    await state.refresh();
    await state.closePreview();
    expect(state.preview.value).toBeUndefined();
    props.run = { ...props.run, graphRevision: 12 };
    await nextTick();
    expect(state.preview.value).toBeUndefined();
    expect(api.loadRunPromptPreview).toHaveBeenCalledOnce();
    api.loadRunPromptPreview.mockResolvedValueOnce(preview);
    await state.refresh();
    expect(state.preview.value).toEqual(preview);
    expect(api.loadRunPromptPreview).toHaveBeenCalledTimes(2);
  });

  it.each(["success", "error"] as const)(
    "закрытие отменяет запрос и поздний %s не открывает preview повторно",
    async (outcome) => {
      const pending = deferred();
      api.loadRunPromptPreview.mockReturnValueOnce(pending.promise);
      const { props, state, signal } = mount();
      const loading = state.refresh();
      const activeSignal = signal();
      const closing = state.closePreview();
      expect(activeSignal.aborted).toBe(true);
      expect(state.loading.value).toBe(false);
      props.run = { ...props.run, graphRevision: 12 };
      if (outcome === "success") pending.resolve(preview);
      else pending.reject(new Error("Closed preview failure"));
      await loading;
      await closing;
      expect(state.preview.value).toBeUndefined();
      expect(state.problem.value).toBeUndefined();
      expect(api.loadRunPromptPreview).toHaveBeenCalledOnce();
    },
  );

  it("после закрытия возвращает фокус на подключённую кнопку при прежних pins", async () => {
    api.loadRunPromptPreview.mockResolvedValueOnce(preview);
    const { props, state } = mount();
    const focus = vi.fn();
    state.opener.value = {
      isConnected: true,
      focus,
    } as unknown as HTMLButtonElement;
    await state.refresh();
    const closing = state.closePreview();
    expect(state.preview.value).toBeUndefined();
    expect(focus).not.toHaveBeenCalled();
    props.run = { ...props.run, graphRevision: 12, lastEventSequence: 12 };
    await closing;
    expect(focus).toHaveBeenCalledOnce();
  });

  it.each(["ref", "version", "attempt"] as const)(
    "не возвращает фокус в прежний scope при смене %s перед nextTick",
    async (pin) => {
      const { props, state } = mount();
      const focus = vi.fn();
      state.opener.value = {
        isConnected: true,
        focus,
      } as unknown as HTMLButtonElement;
      const closing = state.closePreview();
      props.run = {
        ...props.run,
        [pin]: pin === "ref" ? "run_other" : props.run[pin] + 1,
      };
      await closing;
      expect(focus).not.toHaveBeenCalled();
    },
  );

  it.each(["unmount", "detached", "replaced"] as const)(
    "не фокусирует недоступную или заменённую кнопку: %s",
    async (boundary) => {
      const { state, unmount } = mount();
      const focus = vi.fn();
      state.opener.value = {
        isConnected: true,
        focus,
      } as unknown as HTMLButtonElement;
      const closing = state.closePreview();
      if (boundary === "unmount") unmount();
      else if (boundary === "detached")
        Object.defineProperty(state.opener.value, "isConnected", {
          value: false,
        });
      else state.opener.value = undefined;
      await closing;
      expect(focus).not.toHaveBeenCalled();
    },
  );

  it.each(["ref", "version", "attempt"] as const)(
    "очищает готовый preview при изменении %s",
    async (pin) => {
      api.loadRunPromptPreview.mockResolvedValueOnce(preview);
      const { props, state } = mount();
      await state.refresh();
      props.run = {
        ...props.run,
        [pin]: pin === "ref" ? "run_other" : props.run[pin] + 1,
      };
      expect(state.preview.value).toBeUndefined();
      expect(state.problem.value).toBeUndefined();
      expect(state.loading.value).toBe(false);
    },
  );

  it.each(["ref", "version", "attempt"] as const)(
    "при изменении %s отменяет запрос и игнорирует поздний успех и ошибку",
    async (pin) => {
      for (const outcome of ["success", "error"] as const) {
        const old = deferred();
        const fresh = deferred();
        api.loadRunPromptPreview
          .mockReturnValueOnce(old.promise)
          .mockReturnValueOnce(fresh.promise);
        const { props, state, signal, unmount } = mount();
        const oldLoading = state.refresh();
        const oldSignal = signal();
        props.run = {
          ...props.run,
          [pin]: pin === "ref" ? "run_other" : props.run[pin] + 1,
        };
        expect(oldSignal.aborted).toBe(true);
        expect(state.loading.value).toBe(false);
        const freshLoading = state.refresh();
        const freshSignal = signal();
        if (outcome === "success") old.resolve(preview);
        else old.reject(new Error("Stale preview failure"));
        await oldLoading;
        expect(state.preview.value).toBeUndefined();
        expect(state.problem.value).toBeUndefined();
        expect(state.loading.value).toBe(true);
        expect(freshSignal.aborted).toBe(false);
        fresh.resolve({ ...preview, templateRef: "preview_fresh" });
        await freshLoading;
        expect(state.preview.value?.templateRef).toBe("preview_fresh");
        unmount();
      }
    },
  );

  it("сохраняет текущую ошибку при прежних pins и очищает её при смене версии", async () => {
    const problem = new AppProblem({
      status: 403,
      code: "FRESH_AUTHENTICATION_REQUIRED",
      kind: "forbidden",
      retryable: false,
    });
    api.loadRunPromptPreview.mockRejectedValueOnce(problem);
    const { props, state } = mount();
    await state.refresh();
    props.run = { ...props.run, lastEventSequence: 12 };
    await nextTick();
    expect(state.problem.value).toEqual(problem);
    props.run = { ...props.run, version: 3 };
    expect(state.problem.value).toBeUndefined();
  });

  it.each(["success", "error"] as const)(
    "при unmount отменяет запрос и игнорирует поздний %s",
    async (outcome) => {
      const pending = deferred();
      api.loadRunPromptPreview.mockReturnValueOnce(pending.promise);
      const { state, signal, unmount } = mount();
      const loading = state.refresh();
      const activeSignal = signal();
      unmount();
      expect(activeSignal.aborted).toBe(true);
      if (outcome === "success") pending.resolve(preview);
      else pending.reject(new Error("Unmounted preview failure"));
      await loading;
      expect(state.preview.value).toBeUndefined();
      expect(state.problem.value).toBeUndefined();
      expect(state.loading.value).toBe(false);
      expect(api.loadRunPromptPreview).toHaveBeenCalledOnce();
    },
  );
});
