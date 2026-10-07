import {
  createRenderer,
  defineComponent,
  nextTick,
  reactive,
  ssrContextKey,
  type App,
  type Ref,
  type SetupContext,
} from "vue";
import { readFileSync } from "node:fs";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { captureSetupState } from "@/test-utils/setup-harness";
import { providerUsageFixture } from "@/test-utils/provider-usage-fixture";
import type { ProviderAccount } from "./model";

const api = vi.hoisted(() => ({ setProviderAccountConcurrency: vi.fn() }));
vi.mock("./api", () => api);
vi.mock("@/shared/api/client", () => ({
  requestSignal: () => new AbortController().signal,
}));
vi.mock("vue-i18n", () => ({ useI18n: () => ({ t: (key: string) => key }) }));
import Settings from "./ProviderAccountConcurrencySettings.vue";

function account(): ProviderAccount {
  return {
    ref: "pacc_settings01",
    version: 4,
    name: "Account",
    definitionKey: "openai-codex",
    externalAccountMasked: "",
    state: "AUTHORIZED",
    enabled: true,
    ready: true,
    maximumConcurrentExecutions: 10,
    nextActions: ["OPEN", "EDIT"],
    createdAt: "2026-10-03T00:00:00Z",
    updatedAt: "2026-10-03T00:00:00Z",
  };
}
type State = {
  limit: Ref<number>;
  busy: Ref<boolean>;
  valid: Ref<boolean>;
  save(): Promise<void>;
};
async function setup() {
  const props = reactive({ account: account() });
  const state = (await captureSetupState(
    Settings,
    undefined,
    props,
  )) as unknown as State;
  return { props, state };
}
beforeEach(() => vi.clearAllMocks());
const apps: App[] = [];
afterEach(() => {
  for (const app of apps.splice(0)) app.unmount();
});
function mountedSettings() {
  const props = reactive({ account: account() });
  let state: State | undefined;
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
  const component = Settings as unknown as {
    setup(props: object, context: SetupContext): State;
  };
  const app = renderer.createApp(
    defineComponent({
      setup(_props, context) {
        state = component.setup(props, context);
        return () => null;
      },
    }),
  );
  app.provide(ssrContextKey, {});
  app.mount({});
  apps.push(app);
  if (!state) throw new Error("Settings setup state is missing");
  return { props, state };
}

describe("параллельность аккаунта", () => {
  it("сохраняет ввод и активный запрос при замене usage того же аккаунта и версии", async () => {
    const { props, state } = mountedSettings();
    state.limit.value = 20;
    props.account = {
      ...props.account,
      usage: providerUsageFixture(undefined, { activeExecutions: 2 }),
    };
    await nextTick();
    expect(state.limit.value).toBe(20);
    let finish: (value: ProviderAccount) => void = () => {};
    api.setProviderAccountConcurrency.mockImplementation(
      () => new Promise((resolve) => (finish = resolve)),
    );
    const pending = state.save();
    const signal: unknown =
      api.setProviderAccountConcurrency.mock.calls[0]?.[2];
    if (!(signal instanceof AbortSignal)) throw new Error("Signal is missing");
    props.account = {
      ...props.account,
      usage: providerUsageFixture(undefined, { activeExecutions: 3 }),
    };
    await nextTick();
    expect(state.limit.value).toBe(20);
    expect(state.busy.value).toBe(true);
    expect(signal.aborted).toBe(false);
    await state.save();
    expect(api.setProviderAccountConcurrency).toHaveBeenCalledTimes(1);
    finish({ ...props.account, version: 5, maximumConcurrentExecutions: 20 });
    await pending;
    expect(state.busy.value).toBe(false);
  });
  it.each(["ref", "version", "authority"] as const)(
    "сбрасывает ввод и отменяет запрос при смене %s",
    async (changed) => {
      const { props, state } = mountedSettings();
      let finish: (value: ProviderAccount) => void = () => {};
      api.setProviderAccountConcurrency.mockImplementation(
        () => new Promise((resolve) => (finish = resolve)),
      );
      state.limit.value = 20;
      const pending = state.save();
      const signal: unknown =
        api.setProviderAccountConcurrency.mock.calls[0]?.[2];
      if (!(signal instanceof AbortSignal))
        throw new Error("Signal is missing");
      props.account = {
        ...props.account,
        ...(changed === "ref" ? { ref: "pacc_other01" } : {}),
        ...(changed === "version" ? { version: 5 } : {}),
        ...(changed === "authority"
          ? { nextActions: ["OPEN"] as ProviderAccount["nextActions"] }
          : {}),
      };
      await nextTick();
      expect(signal.aborted).toBe(true);
      expect(state.limit.value).toBe(10);
      expect(state.busy.value).toBe(false);
      finish({ ...props.account, maximumConcurrentExecutions: 20 });
      await pending;
      expect(state.limit.value).toBe(10);
    },
  );
  it("показывает текущий лимит и не принимает дробные и выходящие за границы значения", async () => {
    const { state } = await setup();
    expect(state.limit.value).toBe(10);
    for (const limit of [0, -1, 257, 1.5, Number.NaN]) {
      state.limit.value = limit;
      expect(state.valid.value).toBe(false);
      await state.save();
    }
    expect(api.setProviderAccountConcurrency).not.toHaveBeenCalled();
  });
  it("не отправляет команду без server-derived EDIT", async () => {
    const { props, state } = await setup();
    props.account.nextActions = ["OPEN"];
    state.limit.value = 20;
    await state.save();
    expect(api.setProviderAccountConcurrency).not.toHaveBeenCalled();
  });
  it("сохраняет точный аккаунт и исключает повторный submit до ответа", async () => {
    const { props, state } = await setup();
    let finish!: (value: ProviderAccount) => void;
    api.setProviderAccountConcurrency.mockImplementation(
      () =>
        new Promise((resolve) => {
          finish = resolve;
        }),
    );
    state.limit.value = 20;
    const pending = state.save();
    expect(state.busy.value).toBe(true);
    await state.save();
    expect(api.setProviderAccountConcurrency).toHaveBeenCalledTimes(1);
    expect(api.setProviderAccountConcurrency).toHaveBeenCalledWith(
      props.account,
      20,
      expect.any(AbortSignal),
    );
    finish({ ...props.account, version: 5, maximumConcurrentExecutions: 20 });
    await pending;
    expect(state.busy.value).toBe(false);
  });
  it("оформляет ограниченную форму с общими кнопками, локализацией и отменой устаревшего запроса", () => {
    const source = readFileSync(
      new URL("./ProviderAccountConcurrencySettings.vue", import.meta.url),
      "utf8",
    );
    expect(source).toContain('type="range"');
    expect(source).toContain('<output :for="inputId">{{ limit }}</output>');
    expect(source).toContain('max="256"');
    expect(source).toContain('class="button button--primary"');
    expect(source).toContain('t("providers.concurrencyActiveHint")');
    expect(source).toContain("controller.abort()");
    expect(source).toContain("props.account.version === account.version");
  });
});
