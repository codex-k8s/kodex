import { createPinia } from "pinia";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  createRenderer,
  defineComponent,
  nextTick,
  ssrContextKey,
  type ComputedRef,
  type SetupContext,
} from "vue";
import { createI18n } from "vue-i18n";
import { createMemoryHistory, createRouter } from "vue-router";
import type {
  BootstrapState,
  Run,
  RunGraph,
  RunWorkspace,
} from "@/shared/api/generated/openapi/types.gen";
import { selectProjectRef } from "@/shared/project-context";

const api = vi.hoisted(() => ({
  getRunGraph: vi.fn(),
  listRunEvents: vi.fn(),
  listOwnerGates: vi.fn(),
}));
const streams = vi.hoisted(() => ({
  state: {},
  openRun: vi.fn(),
  closeRun: vi.fn(),
}));
vi.mock("@/features/realtime/store", () => ({
  useRealtimeStore: () => streams,
}));
vi.mock("@/shared/api/generated/openapi/sdk.gen", async (original) => ({
  ...(await original<
    typeof import("@/shared/api/generated/openapi/sdk.gen")
  >()),
  ...api,
}));
vi.mock("@/shared/api/client", () => ({
  requestSignal: (parent?: AbortSignal) =>
    parent ?? new AbortController().signal,
}));
import { usePlatformStore } from "@/features/platform/store";
import RunPage from "./RunPage.vue";

const runRef = "run_route_fixture";
const projectRef = "prj_route_fixture";
const bootstrap = { organizationRef: "org_route_fixture" } as BootstrapState;
const disposers: (() => void)[] = [];

function workspace(version = 1): RunWorkspace {
  const run: Run = {
    ref: runRef,
    rootRunRef: runRef,
    version,
    projectRef,
    sessionRef: "ses_route_fixture",
    target: {
      type: "WORKFLOW",
      ref: "wfl_route_fixture",
      displayName: "Проверка процесса",
      version: 1,
    },
    title: "Живой родительский запуск",
    titleSource: "USER_EDITED",
    activitySummary: "Выполняется процесс",
    source: "CONTROL_CENTER",
    initiator: { ref: "usr_route_fixture", displayName: "Владелец" },
    state: "RUNNING",
    attempt: 1,
    graphRevision: version,
    lastEventSequence: 0,
    usage: {
      totalTokens: 0,
      inputTokens: 0,
      cachedInputTokens: 0,
      cacheWriteInputTokens: 0,
      outputTokens: 0,
      reasoningOutputTokens: 0,
      modelContextWindow: 0,
    },
    artifactRefs: [],
    gateRefs: [],
    incidents: [],
    createdAt: "2026-10-06T00:00:00Z",
    nextActions: [],
  };
  return {
    run,
    graph: { runRef, revision: version, sequence: 0, nodes: [], edges: [] },
  };
}

beforeEach(() => {
  vi.resetAllMocks();
  selectProjectRef(projectRef);
  vi.stubGlobal("window", {
    setTimeout,
    clearTimeout,
    location: { origin: "https://kodex.test" },
  });
  api.getRunGraph.mockResolvedValue({
    data: workspace(),
    response: new Response(null, { status: 200 }),
  });
  api.listRunEvents.mockResolvedValue({
    data: { items: [], currentSequence: 0, complete: true },
    response: new Response(null, { status: 200 }),
  });
  api.listOwnerGates.mockResolvedValue({
    data: { items: [] },
    response: new Response(null, { status: 200 }),
  });
});
afterEach(() => {
  for (const dispose of disposers.splice(0)) dispose();
  selectProjectRef(undefined);
  vi.unstubAllGlobals();
});

async function mount() {
  const pinia = createPinia();
  const platform = usePlatformStore(pinia);
  platform.bootstrap = bootstrap;
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: "/projects/:projectRef/runs/:runRef", component: RunPage },
    ],
  });
  const path = `/projects/${projectRef}/runs/${runRef}`;
  await router.push(path);
  await router.isReady();
  let state!: {
    run: ComputedRef<Run | undefined>;
    graph: ComputedRef<RunGraph | undefined>;
    hasAuthoritativeSnapshot: ComputedRef<boolean>;
    fatalLoadProblem: ComputedRef<{ status: number } | undefined>;
  };
  const source = RunPage as unknown as {
    setup(props: object, context: SetupContext): typeof state;
  };
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
  const app = renderer.createApp(
    defineComponent({
      setup(props, context) {
        state = source.setup(props, context);
        return () => null;
      },
    }),
  );
  app
    .use(pinia)
    .use(router)
    .use(
      createI18n({
        legacy: false,
        locale: "ru",
        messages: { ru: {} },
        missingWarn: false,
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
  await vi.waitFor(() =>
    expect(state.hasAuthoritativeSnapshot.value).toBe(true),
  );
  await nextTick();
  return { platform, router, state, path, unmount };
}

describe("Восстановление точного Run route", () => {
  it("не возобновляет stream после ухода со страницы во время exact readback", async () => {
    const { platform, unmount } = await mount();
    let resolve!: (value: { data: RunWorkspace; response: Response }) => void;
    api.getRunGraph.mockReturnValueOnce(
      new Promise((ready) => {
        resolve = ready;
      }),
    );
    platform.applyRealtimeAvailability([], projectRef);
    await vi.waitFor(() => expect(api.getRunGraph).toHaveBeenCalledTimes(2));
    unmount();
    resolve({
      data: workspace(2),
      response: new Response(null, { status: 200 }),
    });
    await vi.waitFor(() => expect(platform.runs[runRef]?.version).toBe(2));
    await nextTick();
    expect(streams.openRun).toHaveBeenCalledTimes(1);
  });
  it("смена route запускает одно чтение нового ref, а не второй recovery запрос", async () => {
    const { router, state } = await mount();
    const nextRef = "run_next_route_fixture";
    const next = workspace(2);
    next.run.ref = nextRef;
    next.run.rootRunRef = nextRef;
    next.graph.runRef = nextRef;
    api.getRunGraph.mockResolvedValue({
      data: next,
      response: new Response(null, { status: 200 }),
    });
    await router.push(`/projects/${projectRef}/runs/${nextRef}`);
    await vi.waitFor(() => expect(state.run.value?.ref).toBe(nextRef));
    expect(api.getRunGraph).toHaveBeenCalledTimes(2);
    expect(api.getRunGraph).toHaveBeenLastCalledWith(
      expect.objectContaining({ path: { runRef: nextRef } }),
    );
  });
  it("повторно читает exact Run после availability snapshot, не используя старый граф как PASS", async () => {
    const { platform, router, state, path } = await mount();
    api.getRunGraph.mockResolvedValue({
      data: workspace(2),
      response: new Response(null, { status: 200 }),
    });
    platform.applyRealtimeAvailability([], projectRef);
    expect(state.run.value).toBeUndefined();
    expect(state.graph.value?.revision).toBe(1);
    await vi.waitFor(() => expect(api.getRunGraph).toHaveBeenCalledTimes(2));
    await vi.waitFor(() => expect(state.run.value?.version).toBe(2));
    expect(state.graph.value?.revision).toBe(2);
    expect(router.currentRoute.value.fullPath).toBe(path);
    expect(api.getRunGraph).toHaveBeenLastCalledWith(
      expect.objectContaining({ path: { runRef } }),
    );
  });

  it("возвращает прежний route после нового owner bootstrap и fresh readback", async () => {
    const { platform, router, state, path } = await mount();
    platform.clearOwnerState();
    await nextTick();
    expect(state.hasAuthoritativeSnapshot.value).toBe(false);
    expect(api.getRunGraph).toHaveBeenCalledTimes(1);
    api.getRunGraph.mockResolvedValue({
      data: workspace(3),
      response: new Response(null, { status: 200 }),
    });
    platform.bootstrap = { ...bootstrap };
    await vi.waitFor(() => expect(state.run.value?.version).toBe(3));
    expect(state.graph.value?.revision).toBe(3);
    expect(router.currentRoute.value.fullPath).toBe(path);
  });

  it.each([403, 404])(
    "после потери cache показывает authoritative %s и не повторяет чтение бесконечно",
    async (status) => {
      const { platform, state } = await mount();
      api.getRunGraph.mockResolvedValue({
        error: {
          status,
          code: status === 403 ? "FORBIDDEN" : "NOT_FOUND",
          retryable: false,
        },
        response: new Response(null, { status }),
      });
      platform.applyRealtimeAvailability([], projectRef);
      await vi.waitFor(() =>
        expect(state.fatalLoadProblem.value?.status).toBe(status),
      );
      await nextTick();
      expect(state.run.value).toBeUndefined();
      expect(state.hasAuthoritativeSnapshot.value).toBe(false);
      expect(api.getRunGraph).toHaveBeenCalledTimes(2);
      expect(streams.closeRun).toHaveBeenCalledWith(runRef);
      expect(streams.openRun).toHaveBeenCalledTimes(1);
    },
  );
});
