import { reactive, type Ref } from "vue";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { captureSetupState } from "@/test-utils/setup-harness";
import { providerUsageFixture } from "@/test-utils/provider-usage-fixture";
import type {
  ProviderAccount,
  ProviderAccountPage,
  ProviderAccountUsageContext,
} from "@/shared/api/generated/openapi/types.gen";
import type { AsyncEntityLoadRequest } from "@/shared/ui/async-entity-picker";
const api = vi.hoisted(() => ({
  loadProviderAccount: vi.fn(),
  loadProviderAccounts: vi.fn(),
}));
vi.mock("./api", () => api);
vi.mock("vue-i18n", () => ({ useI18n: () => ({ t: (key: string) => key }) }));
vi.mock("vue-router", () => ({ useRoute: () => ({ fullPath: "/agent-one" }) }));
import ProviderAccountSelector from "./ProviderAccountSelector.vue";

const context: ProviderAccountUsageContext = {
  purpose: "CONFIGURE",
  agentRef: "agent-one",
  runtimeProfileRef: "profile-one",
  providerDefinitionKey: "openai-codex",
  model: "model-one",
};
function account(selectedContext = context): ProviderAccount {
  return {
    ref: "account-one",
    version: 1,
    name: "Account",
    definitionKey: "openai-codex",
    enabled: true,
    ready: true,
    state: "AUTHORIZED",
    externalAccountMasked: "",
    nextActions: [],
    createdAt: "2026-09-05T00:00:00Z",
    updatedAt: "2026-09-05T00:00:00Z",
    usage: providerUsageFixture(selectedContext),
  };
}
async function setup() {
  const props = reactive({
    modelValue: [] as { accountRef: string; weight: number }[],
    definitionKey: "openai-codex",
    policyMode: "FIXED",
    usageContext: { ...context },
    disabled: false,
  });
  const state = (await captureSetupState(
    ProviderAccountSelector,
    undefined,
    props,
  )) as unknown as {
    resolved: Ref<Record<string, ProviderAccount>>;
    selectionEligible: Ref<boolean>;
    submissionAllowed: Ref<boolean>;
    freshness: Ref<number>;
    loadAccounts(
      request: AsyncEntityLoadRequest,
    ): Promise<{ items: unknown[] }>;
  };
  return { props, state };
}
beforeEach(() => vi.clearAllMocks());
describe("контекстный account selector", () => {
  it("не публикует позднюю страницу после переключения профиля того же провайдера", async () => {
    const { props, state } = await setup();
    let finish!: (page: ProviderAccountPage) => void;
    api.loadProviderAccounts.mockImplementation(
      () =>
        new Promise<ProviderAccountPage>((resolve) => {
          finish = resolve;
        }),
    );
    const pending = state.loadAccounts({
      query: "",
      cursor: undefined,
      signal: new AbortController().signal,
    });
    props.usageContext = { ...context, runtimeProfileRef: "profile-two" };
    finish({ items: [account()], nextPageToken: "", nextActions: [] });
    expect((await pending).items).toEqual([]);
    expect(state.resolved.value).toEqual({});
    expect(state.submissionAllowed.value).toBe(false);
  });
  it("отдельно разрешает выбор до модели и submit при занятой capacity", async () => {
    const { props, state } = await setup();
    props.modelValue = [{ accountRef: "account-one", weight: 1 }];
    props.usageContext = { ...context, model: undefined };
    state.resolved.value = { "account-one": account(props.usageContext) };
    expect(state.selectionEligible.value).toBe(true);
    expect(state.submissionAllowed.value).toBe(false);
    props.usageContext = { ...context };
    state.resolved.value = { "account-one": account() };
    expect(state.submissionAllowed.value).toBe(true);
    state.freshness.value = Date.parse("2100-01-01T00:00:00Z");
    expect(state.submissionAllowed.value).toBe(false);
  });
});
