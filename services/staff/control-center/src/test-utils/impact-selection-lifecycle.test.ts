import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  defineComponent,
  h,
  nextTick,
  reactive,
  ssrContextKey,
  type Component,
  type Ref,
  type SetupContext,
} from "vue";
import { createI18n } from "vue-i18n";
import { createPinia, setActivePinia } from "pinia";
import { createNoopRenderer } from "./create-noop-renderer";
import { createDeferred } from "./create-deferred";
import { usePlatformStore } from "@/features/platform/store";
import type { AppProblem } from "@/shared/api/problem";
import type {
  BootstrapState,
  RevisionImpactPage,
  RevisionImpactPlan,
  RoleImageImpactPage,
  RoleImageImpactPlan,
} from "@/shared/api/generated/openapi/types.gen";

const requests = vi.hoisted(() => {
  const read = () =>
    vi.fn<
      (
        plan: unknown,
        signal: AbortSignal,
        query: string,
        token: string | undefined,
        pageSize: number,
        organization: string | undefined,
      ) => Promise<unknown>
    >();
  return { role: read(), publication: read() };
});
vi.mock(
  "@/features/managed-configurations/role-image-impact",
  async (original) => ({
    ...(await original<
      typeof import("@/features/managed-configurations/role-image-impact")
    >()),
    readImageImpact: requests.role,
  }),
);
vi.mock("@/features/runtime/publication-impact", async (original) => ({
  ...(await original<typeof import("@/features/runtime/publication-impact")>()),
  readPublicationImpact: requests.publication,
}));
import RoleSelection from "@/features/managed-configurations/RoleImageImpactSelection.vue";
import PublicationSelection from "@/features/runtime/PublicationImpactSelection.vue";

const base = {
  ref: "plan_synthetic",
  version: 1,
  digest: "digest",
  total: 3,
  state: "PREPARED" as const,
  createdAt: "2026-09-17T00:00:00Z",
  expiresAt: "2099-09-17T00:00:00Z",
};
const publicationPlan: RevisionImpactPlan = {
  ...base,
  kind: "RUNTIME_ENVIRONMENT",
  sourceRef: "environment",
  sourceVersion: 3,
  sourceRevisionRef: "revision",
  draftRef: "draft",
  draftVersion: 5,
  targetDigest: "target",
};
const rolePlan: RoleImageImpactPlan = {
  ...base,
  configurationRef: "configuration",
  configurationVersion: 2,
  revisionRef: "revision",
  revisionDigest: "revision-digest",
  recipeRef: "recipe",
  recipeGeneration: 3,
  buildRef: "build",
  artifactRef: "artifact",
  artifactDigest: "artifact-digest",
  admissionPolicyDigest: "admission-digest",
};
interface SelectionState {
  page: Ref<RoleImageImpactPage | RevisionImpactPage | undefined>;
  query: Ref<string>;
  loading: Ref<boolean>;
  problem: Ref<AppProblem | undefined>;
  selected: Ref<Set<string>>;
  load(more?: boolean): Promise<void>;
  toggle(ref: string): void;
  publish(): void;
}
const mounted: Array<() => void> = [];
const variants = [
  {
    name: "образ роли",
    component: RoleSelection,
    plan: rolePlan,
    read: requests.role,
    defaults: false,
  },
  {
    name: "публикация",
    component: PublicationSelection,
    plan: publicationPlan,
    read: requests.publication,
    defaults: true,
  },
];

beforeEach(() => {
  vi.useFakeTimers();
  setActivePinia(createPinia());
  vi.resetAllMocks();
  usePlatformStore().bootstrap = {
    organizationRef: "org_synthetic",
  } as BootstrapState;
});
afterEach(() => {
  for (const dispose of mounted.splice(0)) dispose();
  vi.useRealTimers();
});

describe.each(variants)(
  "жизненный цикл списка: $name",
  ({ component, plan, read, defaults }) => {
    function page(
      ref: string,
      token = "",
      outcome: "PENDING" | "CONFLICT" = "PENDING",
    ) {
      return {
        plan,
        total: 3,
        nextPageToken: token,
        items: [{ ref, outcome }],
      };
    }
    function mount() {
      const props = reactive({ plan: { ...plan }, busy: false });
      const emit = vi.fn();
      let state!: SelectionState;
      const setup = (
        component as Component & {
          setup: (props: object, context: SetupContext) => object;
        }
      ).setup;
      const wrapper = defineComponent({
        setup(_props, context) {
          state = setup(props, { ...context, emit }) as SelectionState;
          return () => h("impact-lifecycle-harness");
        },
      });
      const app = createNoopRenderer().createApp(wrapper);
      // Vite выдаёт SSR-вариант SFC; контекст нужен его учёту модулей.
      // Сам wrapper смонтирован renderer и использует настоящий client lifecycle.
      app.provide(ssrContextKey, {});
      app.use(
        createI18n({ legacy: false, locale: "ru", messages: { ru: {} } }),
      );
      app.mount({});
      const dispose = () => app.unmount();
      mounted.push(dispose);
      return { state, props, emit, dispose };
    }
    async function flush() {
      await Promise.resolve();
      await nextTick();
    }

    it("сразу читает после инициализации и сохраняет самостоятельный выбор", async () => {
      read.mockResolvedValue(page("first"));
      const { state } = mount();
      expect(read).toHaveBeenCalledOnce();
      expect(read.mock.calls[0]?.[0]).toEqual(plan);
      expect(read.mock.calls[0]?.[1]).toBeInstanceOf(AbortSignal);
      expect(read.mock.calls[0]?.slice(2)).toEqual([
        "",
        undefined,
        9,
        "org_synthetic",
      ]);
      await flush();
      expect(state.loading.value).toBe(false);
      expect([...state.selected.value]).toEqual(defaults ? ["first"] : []);
      state.toggle("first");
      expect(state.selected.value.has("first")).toBe(!defaults);
    });

    it.each(["success", "error"])(
      "не применяет поздний %s и не завершает новый запрос",
      async (result) => {
        const old = createDeferred<ReturnType<typeof page>>();
        const fresh = createDeferred<ReturnType<typeof page>>();
        read
          .mockReturnValueOnce(old.promise)
          .mockReturnValueOnce(fresh.promise);
        const { state } = mount();
        const request = state.load();
        expect(read.mock.calls[0]?.[1].aborted).toBe(true);
        if (result === "success") old.resolve(page("stale"));
        else old.reject(new Error("Synthetic stale failure"));
        await flush();
        expect(state.page.value).toBeUndefined();
        expect(state.problem.value).toBeUndefined();
        expect(state.loading.value).toBe(true);
        fresh.resolve(page("fresh"));
        await request;
        expect(state.page.value?.items[0]?.ref).toBe("fresh");
        expect(state.loading.value).toBe(false);
      },
    );

    it("объединяет поиск в один запрос через400ms и очищает прежний выбор", async () => {
      read.mockResolvedValue(page("first"));
      const { state } = mount();
      await flush();
      state.selected.value.add("first");
      state.query.value = "a";
      await nextTick();
      expect(state.selected.value.size).toBe(0);
      expect(state.page.value).toBeUndefined();
      expect(state.loading.value).toBe(true);
      expect(read.mock.calls[0]?.[1].aborted).toBe(true);
      await vi.advanceTimersByTimeAsync(200);
      state.query.value = "ab";
      await nextTick();
      await vi.advanceTimersByTimeAsync(399);
      expect(read).toHaveBeenCalledTimes(1);
      await vi.advanceTimersByTimeAsync(1);
      expect(read).toHaveBeenCalledTimes(2);
      expect(read.mock.calls[1]?.[2]).toBe("ab");
    });

    it("не читает при busy и перечитывает после его снятия", async () => {
      read.mockResolvedValue(page("first", "cursor"));
      const { state, props } = mount();
      await flush();
      props.busy = true;
      await nextTick();
      expect(read.mock.calls[0]?.[1].aborted).toBe(false);
      await state.load();
      await state.load(true);
      expect(read).toHaveBeenCalledTimes(1);
      props.busy = false;
      await flush();
      expect(read).toHaveBeenCalledTimes(2);
      expect(read.mock.calls[0]?.[1].aborted).toBe(true);
    });

    it("перечитывает при новой версии плана и отвергает прежний ACK", async () => {
      const old = createDeferred<ReturnType<typeof page>>();
      read
        .mockReturnValueOnce(old.promise)
        .mockResolvedValueOnce(page("fresh"));
      const { state, props } = mount();
      props.plan.version++;
      await flush();
      expect(read).toHaveBeenCalledTimes(2);
      expect(read.mock.calls[0]?.[1].aborted).toBe(true);
      old.resolve(page("stale"));
      await flush();
      expect(state.page.value?.items[0]?.ref).toBe("fresh");
    });

    it("не допускает параллельную догрузку и сохраняет страницу при повторном ref", async () => {
      const extra = createDeferred<ReturnType<typeof page>>();
      read
        .mockResolvedValueOnce(page("first", "cursor"))
        .mockReturnValueOnce(extra.promise);
      const { state } = mount();
      await flush();
      const request = state.load(true);
      await state.load(true);
      expect(read).toHaveBeenCalledTimes(2);
      expect(read.mock.calls[1]?.[3]).toBe("cursor");
      extra.resolve(page("first"));
      await request;
      expect(state.page.value?.items.map((item) => item.ref)).toEqual([
        "first",
      ]);
      expect(state.problem.value).toBeDefined();
      expect(state.loading.value).toBe(false);
    });

    it("размонтирование отменяет запрос и поздний результат", async () => {
      const old = createDeferred<ReturnType<typeof page>>();
      read.mockReturnValue(old.promise);
      const { state, dispose } = mount();
      dispose();
      mounted.pop();
      expect(read.mock.calls[0]?.[1].aborted).toBe(true);
      old.resolve(page("stale"));
      await flush();
      expect(state.page.value).toBeUndefined();
      expect(vi.getTimerCount()).toBe(0);
    });

    it("размонтирование снимает debounce и terminal запрещает публикацию", async () => {
      read.mockResolvedValue(page("first"));
      const { state, props, emit, dispose } = mount();
      await flush();
      read.mockResolvedValue({
        ...page("first"),
        plan: { ...plan, state: "EXPIRED" },
      });
      props.plan.state = "EXPIRED";
      await flush();
      state.publish();
      expect(emit).not.toHaveBeenCalled();
      state.query.value = "pending";
      await nextTick();
      const count = read.mock.calls.length;
      dispose();
      mounted.pop();
      await vi.advanceTimersByTimeAsync(1000);
      expect(read).toHaveBeenCalledTimes(count);
      expect(vi.getTimerCount()).toBe(0);
    });
  },
);
