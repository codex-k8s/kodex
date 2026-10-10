import { readFileSync } from "node:fs";
import {
  createRenderer,
  defineComponent,
  reactive,
  ssrContextKey,
  type Ref,
  type SetupContext,
} from "vue";
import { createI18n } from "vue-i18n";
import { afterEach, describe, expect, it, vi } from "vitest";
import FilesWorkspace from "./FilesWorkspace.vue";
import { AppProblem } from "@/shared/api/problem";
import type { Artifact } from "@/shared/api/generated/openapi/types.gen";

const api = vi.hoisted(() => ({ loadArtifactPage: vi.fn() }));
const platform = vi.hoisted(() => ({
  artifacts: {} as Record<string, Artifact>,
  projects: {},
  realtimeSnapshot: vi.fn(),
}));
vi.mock("./api", () => ({
  ...api,
  deleteArtifactItem: vi.fn(),
  loadArtifactImpact: vi.fn(),
  mutateArtifactsSequentially: vi.fn(),
  purgeArtifactItem: vi.fn(),
  restoreArtifactItem: vi.fn(),
  uploadArtifactItem: vi.fn(),
}));
vi.mock("@/features/platform/store", () => ({
  usePlatformStore: () => platform,
}));

const source = readFileSync(
  new URL("./FilesWorkspace.vue", import.meta.url),
  "utf8",
);
const artifact: Artifact = {
  ref: "art_fixture",
  currentRevisionRef: "arv_fixture",
  projectRef: "prj_fixture",
  version: 1,
  revision: 1,
  fileName: "document.md",
  mediaType: "text/markdown",
  sizeBytes: 3,
  digest: `sha256:${"a".repeat(64)}`,
  scanState: "CLEAN",
  source: "CONTROL_CENTER",
  previewAvailable: true,
  lifecycleState: "ACTIVE",
  agentBindings: [],
  nextActions: [],
  createdAt: "2026-10-10T00:00:00Z",
};
const unavailable = new AppProblem({
  status: 503,
  code: "UNAVAILABLE",
  kind: "unavailable",
  retryable: true,
});

type State = {
  items: Ref<{ artifact: Artifact }[]>;
  listProblem: Ref<AppProblem | undefined>;
  initialLoading: Ref<boolean>;
  refresh: () => void;
  query: Ref<string>;
};
const unmounts: (() => void)[] = [];
function mount() {
  let state!: State;
  const renderer = createRenderer<object, object>({
    patchProp() {},
    insert() {},
    remove() {},
    createElement: () => ({}),
    createText: () => ({}),
    createComment: () => ({}),
    setText() {},
    setElementText() {},
    parentNode: () => null,
    nextSibling: () => null,
  });
  const component = FilesWorkspace as unknown as {
    setup(props: unknown, context: SetupContext): State;
  };
  const app = renderer.createApp(
    defineComponent({
      setup(_props, context) {
        state = component.setup(
          reactive({ projectRef: "prj_fixture", mode: "ACTIVE" }),
          context,
        );
        return () => null;
      },
    }),
  );
  app.use(
    createI18n({
      legacy: false,
      locale: "ru",
      messages: { ru: {} },
      missingWarn: false,
      fallbackWarn: false,
    }),
  );
  app.provide(ssrContextKey, {});
  app.mount({});
  unmounts.push(() => app.unmount());
  return state;
}
afterEach(() => {
  unmounts.splice(0).forEach((unmount) => unmount());
  platform.artifacts = {};
  vi.resetAllMocks();
  vi.useRealTimers();
});

describe("FilesWorkspace initial loading and retry", () => {
  it("без realtime snapshot читает typed list, показывает503 и явно повторяет чтение", async () => {
    vi.useFakeTimers();
    api.loadArtifactPage
      .mockRejectedValueOnce(unavailable)
      .mockResolvedValueOnce({ items: [], total: 0 });
    const state = mount();
    expect(state.initialLoading.value).toBe(true);
    await vi.runAllTimersAsync();
    expect(api.loadArtifactPage).toHaveBeenCalledOnce();
    expect(api.loadArtifactPage).toHaveBeenCalledWith(
      "prj_fixture",
      expect.objectContaining({
        query: "",
        signal: expect.any(AbortSignal) as AbortSignal,
      }),
      { lifecycleState: "ACTIVE", allSources: true },
    );
    expect(state.listProblem.value).toBe(unavailable);
    expect(state.items.value).toEqual([]);
    state.refresh();
    await vi.runAllTimersAsync();
    expect(api.loadArtifactPage).toHaveBeenCalledTimes(2);
    expect(state.listProblem.value).toBeUndefined();
    expect(state.items.value).toEqual([]);
    await vi.advanceTimersByTimeAsync(60_000);
    expect(api.loadArtifactPage).toHaveBeenCalledTimes(2);
  });

  it("при существующем snapshot не запускает list, включая авторитетную пустую страницу", async () => {
    vi.useFakeTimers();
    platform.realtimeSnapshot.mockReturnValue({ scopeKey: "prj_fixture" });
    const state = mount();
    await vi.runAllTimersAsync();
    expect(state.items.value).toEqual([]);
    expect(state.listProblem.value).toBeUndefined();
    expect(state.initialLoading.value).toBe(false);
    expect(api.loadArtifactPage).not.toHaveBeenCalled();
  });

  it("сохраняет загруженный файл при503 refresh и заменяет его после успешного retry", async () => {
    vi.useFakeTimers();
    const item = { artifact, id: artifact.ref, label: artifact.fileName };
    api.loadArtifactPage
      .mockResolvedValueOnce({ items: [item] })
      .mockRejectedValueOnce(unavailable)
      .mockResolvedValueOnce({ items: [] });
    const state = mount();
    await vi.runAllTimersAsync();
    state.refresh();
    expect(state.items.value).toEqual([item]);
    await vi.runAllTimersAsync();
    expect(state.items.value).toEqual([item]);
    expect(state.listProblem.value).toBe(unavailable);
    state.refresh();
    await vi.runAllTimersAsync();
    expect(state.items.value).toEqual([]);
    expect(state.listProblem.value).toBeUndefined();
  });

  it("unmount отменяет initial typed read и отклоняет поздний ACK", async () => {
    vi.useFakeTimers();
    let resolve!: (page: {
      items: { artifact: Artifact; id: string; label: string }[];
    }) => void;
    api.loadArtifactPage.mockImplementationOnce(
      () =>
        new Promise((accept) => {
          resolve = accept;
        }),
    );
    const state = mount();
    await vi.advanceTimersByTimeAsync(0);
    const request = api.loadArtifactPage.mock.calls[0]?.[1] as {
      signal: AbortSignal;
    };
    unmounts.pop()?.();
    expect(request.signal.aborted).toBe(true);
    resolve({
      items: [{ artifact, id: artifact.ref, label: artifact.fileName }],
    });
    await vi.runAllTimersAsync();
    expect(state.items.value).toEqual([]);
  });

  it("отрисовывает error/retry вместо empty и сохраняет видимость загруженных файлов", () => {
    expect(source).toContain(':loading="initialLoading && items.length === 0"');
    expect(source).toContain(
      ':problem="items.length === 0 ? listProblem : undefined"',
    );
    expect(source).toContain('v-if="listProblem && items.length > 0"');
    expect(source).toContain('@retry="refresh"');
  });
});
