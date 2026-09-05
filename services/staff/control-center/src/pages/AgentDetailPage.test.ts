import { ref, type Ref } from "vue";
import { expect, it, vi } from "vitest";
import { captureSetupState } from "@/test-utils/setup-harness";
const navigation = vi.hoisted(() => ({ replace: vi.fn() }));
vi.mock("vue-router", () => ({
  useRoute: () => ({ query: { tab: "runtime" }, params: {} }),
  useRouter: () => navigation,
}));
vi.mock("vue-i18n", () => ({
  useI18n: () => ({ locale: ref("ru"), t: (key: string) => key }),
}));
vi.mock("@/features/platform/store", () => ({
  usePlatformStore: () => ({ agents: {} }),
}));
import AgentDetailPage from "./AgentDetailPage.vue";

it("сохраняет runtime-вкладку, пока route guard не разрешил переход", async () => {
  navigation.replace.mockResolvedValue({ type: 4 });
  const state = (await captureSetupState(AgentDetailPage)) as unknown as {
    selectTab(tab: "profile"): void;
    activeTab: Ref<string>;
  };
  state.selectTab("profile");
  await Promise.resolve();
  expect(navigation.replace).toHaveBeenCalledWith({
    query: { tab: "profile" },
  });
  expect(state.activeTab.value).toBe("runtime");
});
