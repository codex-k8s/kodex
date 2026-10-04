import { reactive, type Ref } from "vue";
import { readFileSync } from "node:fs";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { captureSetupState } from "@/test-utils/setup-harness";
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

describe("параллельность аккаунта", () => {
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
