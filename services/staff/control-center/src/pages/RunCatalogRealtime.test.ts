import { createPinia } from "pinia";
import {
  createRenderer,
  defineComponent,
  nextTick,
  reactive,
  ssrContextKey,
  type SetupContext,
} from "vue";
import { afterEach, describe, expect, it, vi } from "vitest";
import { i18n } from "@/app/i18n";
import { usePlatformStore } from "@/features/platform/store";
import { useRunBoardStore } from "@/features/workboard/run-board";
import { selectProjectRef } from "@/shared/project-context";
import type { Run } from "@/shared/api/generated/openapi/types.gen";
import RunsPage from "./RunsPage.vue";
import HomeResultCatalog from "@/features/home/components/HomeResultCatalog.vue";

vi.mock("vue-router", () => ({ useRoute: () => ({ params: {} }) }));
vi.mock("@/features/realtime/store", () => ({
  useRealtimeStore: () => ({ refreshSession: vi.fn() }),
}));
vi.mock("@/shared/locale", () => ({ currentLocale: () => "ru" }));

const disposers: Array<() => void> = [];
afterEach(() => {
  for (const dispose of disposers.splice(0)) dispose();
  selectProjectRef(undefined);
});

function mountSetup(
  component: unknown,
  pinia: ReturnType<typeof createPinia>,
  props: Record<string, unknown> = {},
) {
  const emit = vi.fn();
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
  const runtimeComponent = component as {
    setup(props: Record<string, unknown>, context: SetupContext): unknown;
  };
  const app = renderer.createApp(
    defineComponent({
      setup(_props, context) {
        runtimeComponent.setup(reactive(props), { ...context, emit });
        return () => null;
      },
    }),
  );
  app.use(pinia);
  app.use(i18n);
  app.provide(ssrContextKey, {});
  app.mount({});
  disposers.push(() => app.unmount());
  return emit;
}

function fixture(ref: string): Run {
  return {
    ref,
    rootRunRef: ref,
    projectRef: "project_fixture",
    sessionRef: `session_${ref}`,
    version: 1,
    target: {
      type: "AGENT",
      ref: "agent_fixture",
      displayName: "Разработчик",
      version: 1,
    },
    title: ref,
    titleSource: "USER_EDITED",
    activitySummary: "Готово",
    source: "CONTROL_CENTER",
    initiator: { ref: "owner_fixture", displayName: "Владелец" },
    state: "SUCCEEDED",
    attempt: 1,
    graphRevision: 1,
    lastEventSequence: 1,
    artifactRefs: [],
    gateRefs: [],
    nextActions: ["ADD_TURN"],
    createdAt: "2026-10-24T00:00:00Z",
    usage: {
      totalTokens: 0,
      inputTokens: 0,
      outputTokens: 0,
      cachedInputTokens: 0,
      cacheWriteInputTokens: 0,
      reasoningOutputTokens: 0,
      modelContextWindow: 0,
    },
  };
}

function applyPage(
  store: ReturnType<typeof usePlatformStore>,
  runs: Run[],
  nextPageToken?: string,
) {
  store.applyPlatformSnapshot("RUN", undefined, {
    catalog: {
      runs,
      gates: [],
      gatesPage: {},
      total: nextPageToken ? runs.length + 1 : runs.length,
      page: { nextPageToken },
    },
  });
}

describe("Открытые каталоги после realtime замены страницы", () => {
  it("обновляет доску при удалении записи и изменении только cursor", async () => {
    selectProjectRef(undefined);
    const pinia = createPinia(),
      platform = usePlatformStore(pinia),
      board = useRunBoardStore(pinia);
    const first = fixture("run_first01"),
      removed = fixture("run_removed01");
    applyPage(platform, [first, removed]);
    mountSetup(RunsPage, pinia);
    await nextTick();
    expect(board.items.map((x) => x.ref)).toEqual([first.ref, removed.ref]);
    applyPage(platform, [first]);
    await nextTick();
    expect(board.items.map((x) => x.ref)).toEqual([first.ref]);
    expect(platform.runs[removed.ref]).toEqual(removed);
    applyPage(platform, [first], "next_page");
    await nextTick();
    expect(board.pageToken).toBe("more");
    applyPage(platform, [first]);
    await nextTick();
    expect(board.pageToken).toBeUndefined();
    applyPage(platform, []);
    await nextTick();
    expect(board.items).toEqual([]);
  });

  it("сообщает родителю нулевой и восстановленный счётчик сессий без смены detail cache", async () => {
    selectProjectRef(undefined);
    const pinia = createPinia(),
      platform = usePlatformStore(pinia),
      item = fixture("run_recent01");
    applyPage(platform, [item]);
    const emit = mountSetup(HomeResultCatalog, pinia, {
      kind: "SESSION",
      dashboard: true,
      ready: true,
    });
    await nextTick();
    expect(emit).toHaveBeenCalledWith("total", 1);
    emit.mockClear();
    applyPage(platform, []);
    await nextTick();
    expect(emit).toHaveBeenCalledWith("total", 0);
    expect(platform.runs[item.ref]).toEqual(item);
    emit.mockClear();
    applyPage(platform, [item]);
    await nextTick();
    expect(emit).toHaveBeenCalledWith("total", 1);
  });
});
