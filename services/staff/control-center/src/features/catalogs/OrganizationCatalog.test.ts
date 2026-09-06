import { defineComponent, type Ref, type SetupContext } from "vue";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { captureSetupState } from "@/test-utils/setup-harness";
import type { AppProblem } from "@/shared/api/problem";
import type { CatalogEntry, CatalogKind, CatalogPage } from "./api";

interface Action {
  name: string;
  args: string[];
  after(callback: () => void): void;
  onError(callback: (error: unknown) => void): void;
}
const dependencies = vi.hoisted(() => ({
  subscribe: vi.fn<(callback: (action: Action) => void) => () => void>(),
  load: vi.fn<
    (
      kind: CatalogKind,
      query: string,
      signal: AbortSignal,
      cursor?: string,
      projectRef?: string,
    ) => Promise<CatalogPage>
  >(),
  project: vi.fn(),
}));
vi.mock("@/features/platform/store", () => ({
  usePlatformStore: () => ({ $onAction: dependencies.subscribe }),
}));
vi.mock("./api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("./api")>()),
  loadCatalog: dependencies.load,
  loadCatalogProject: dependencies.project,
}));
import OrganizationCatalog from "./OrganizationCatalog.vue";
import { catalogInvalidated } from "./api";

const entry: CatalogEntry = {
  ref: "agent_synthetic",
  projectRef: "project_synthetic",
  title: "Сотрудник",
  description: "",
  state: "READY",
  version: 1,
  path: "/projects/project_synthetic/agents/agent_synthetic",
  meta: [],
};
interface State {
  items: Ref<CatalogEntry[]>;
  pageToken: Ref<string | undefined>;
  problem: Ref<AppProblem | undefined>;
  loading: Ref<boolean>;
}
async function catalog(): Promise<State> {
  const source = OrganizationCatalog as unknown as {
    setup: (props: { kind: CatalogKind }, context: SetupContext) => unknown;
  };
  return (await captureSetupState(
    defineComponent({
      setup(_props, context) {
        return source.setup({ kind: "agents" }, context) as Record<
          string,
          unknown
        >;
      },
    }),
  )) as unknown as State;
}
function action(name: string, kind = "AGENT") {
  let after: (() => void) | undefined;
  let onError: ((error: unknown) => void) | undefined;
  const listener = dependencies.subscribe.mock.calls[0]?.[0];
  if (!listener) throw new Error("Catalog subscription is missing");
  listener({
    name,
    args: [kind],
    after: (callback) => {
      after = callback;
    },
    onError: (callback) => {
      onError = callback;
    },
  });
  return {
    complete: () => after?.(),
    fail: () => onError?.(new Error("Synthetic reload failure")),
  };
}
beforeEach(() => {
  vi.useFakeTimers();
  vi.resetAllMocks();
  dependencies.subscribe.mockReturnValue(() => undefined);
  dependencies.load.mockResolvedValue({
    items: [entry],
    nextPageToken: "cursor_old",
  });
  dependencies.project.mockResolvedValue({
    ref: entry.projectRef,
    name: "Проект",
  });
});
afterEach(() => vi.useRealTimers());

describe("OrganizationCatalog realtime", () => {
  it("отменяет in-flight страницу и не принимает её после membership invalidation", async () => {
    let resolve!: (page: CatalogPage) => void;
    dependencies.load.mockReturnValueOnce(
      new Promise<CatalogPage>((done) => {
        resolve = done;
      }),
    );
    const state = await catalog();
    await vi.advanceTimersByTimeAsync(500);
    const signal = dependencies.load.mock.calls[0]?.[2];
    expect(signal?.aborted).toBe(false);
    action("reloadPlatformKind", "MEMBERSHIP");
    expect(signal?.aborted).toBe(true);
    resolve({ items: [entry], nextPageToken: "stale" });
    await vi.advanceTimersByTimeAsync(0);
    expect(state.items.value).toEqual([]);
    expect(state.pageToken.value).toBeUndefined();
  });
  it("закрывает старые страницы до authoritative reload и начинает с первого cursor", async () => {
    const state = await catalog();
    await vi.advanceTimersByTimeAsync(500);
    expect(state.items.value).toEqual([entry]);
    const reload = action("reloadPlatformKind");
    expect(state.items.value).toEqual([]);
    expect(state.pageToken.value).toBeUndefined();
    expect(state.loading.value).toBe(true);
    expect(dependencies.load).toHaveBeenCalledOnce();
    dependencies.load.mockResolvedValue({ items: [{ ...entry, version: 2 }] });
    reload.complete();
    await vi.advanceTimersByTimeAsync(0);
    expect(state.items.value[0]?.version).toBe(2);
    expect(dependencies.load.mock.calls[1]?.[3]).toBeUndefined();
  });
  it("ошибка membership reload не возвращает старую выборку, logout не запускает чтение", async () => {
    const state = await catalog();
    await vi.advanceTimersByTimeAsync(500);
    action("reloadPlatformKind", "MEMBERSHIP").fail();
    expect(state.items.value).toEqual([]);
    expect(state.problem.value).toBeDefined();
    expect(state.loading.value).toBe(false);
    action("clearOwnerState").complete();
    await vi.advanceTimersByTimeAsync(500);
    expect(dependencies.load).toHaveBeenCalledOnce();
    expect(state.items.value).toEqual([]);
  });
  it("не запускает устаревший completion после второго invalidation", async () => {
    const state = await catalog();
    await vi.advanceTimersByTimeAsync(500);
    const previous = action("reloadPlatformKind");
    action("reloadPlatformState");
    previous.complete();
    await vi.advanceTimersByTimeAsync(0);
    expect(dependencies.load).toHaveBeenCalledOnce();
    expect(state.items.value).toEqual([]);
  });
  it("RUN сохраняет геометрию карточек до ответа и перечитывает с первого cursor", async () => {
    const state = await catalog();
    await vi.advanceTimersByTimeAsync(500);
    const reload = action("reloadPlatformKind", "RUN");
    expect(state.items.value).toEqual([entry]);
    expect(state.pageToken.value).toBeUndefined();
    dependencies.load.mockResolvedValueOnce({
      items: [{ ...entry, version: 2 }],
    });
    reload.complete();
    await vi.advanceTimersByTimeAsync(0);
    expect(state.items.value[0]?.version).toBe(2);
    expect(dependencies.load.mock.calls[1]?.[3]).toBeUndefined();
  });
  it("не выдумывает отсутствующие ENVIRONMENT/SECRET event kinds", () => {
    expect(catalogInvalidated("agents", "AGENT")).toBe(true);
    expect(catalogInvalidated("workflows", "WORKFLOW")).toBe(true);
    expect(catalogInvalidated("automations", "SCHEDULE")).toBe(true);
    expect(catalogInvalidated("workflows", "AGENT")).toBe(true);
    expect(catalogInvalidated("workflows", "RUN")).toBe(true);
    expect(catalogInvalidated("agents", "RUN")).toBe(true);
    expect(catalogInvalidated("projects", "INTEGRATION_GRANT")).toBe(true);
    expect(catalogInvalidated("environments", "MEMBERSHIP")).toBe(true);
    expect(catalogInvalidated("secrets", "PLATFORM_MEMBERSHIP")).toBe(true);
    expect(catalogInvalidated("environments", "ENVIRONMENT")).toBe(false);
    expect(catalogInvalidated("secrets", "SECRET")).toBe(false);
  });
});
