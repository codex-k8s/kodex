import { beforeEach, expect, it, vi } from "vitest";
import type { Ref } from "vue";
import { createI18n } from "vue-i18n";
import { captureSetupState } from "@/test-utils/setup-harness";
import type {
  RuntimeSecretDraft,
  RuntimeSecretDraftImpactPlan,
  RuntimeSecretDraftImpactPage,
} from "@/shared/api/generated/openapi/types.gen";
const api = vi.hoisted(() => ({
  readSecretDraft: vi.fn(),
  readRuntimeSecret: vi.fn(),
  safeDraftProblem: () => ({ status: 0, code: "UNKNOWN" }),
}));
const impact = vi.hoisted(() => ({
  prepareDraftImpact: vi.fn(),
  readDraftImpact: vi.fn(),
  restoreDraftImpact: vi.fn(),
  publishSecretDraft: vi.fn(),
  checkedPlan: vi.fn(),
}));
vi.mock("./draft-api", () => api);
vi.mock("./api", () => ({ readRuntimeSecret: api.readRuntimeSecret }));
vi.mock("./draft-impact", () => impact);
vi.mock("@/shared/api/mutation", () => ({
  idempotencyKey: () => "original-key",
}));
import RuntimeSecretDraftImpact from "./RuntimeSecretDraftImpact.vue";
const draft = {
  ref: "draft",
  projectRef: "project",
  secretRef: "secret",
  version: 2,
  secretVersion: 1,
  state: "VALID",
} as RuntimeSecretDraft;
const plan = {
  ref: "plan",
  draftRef: "draft",
  draftVersion: 2,
  secretVersion: 1,
  state: "PREPARED",
} as RuntimeSecretDraftImpactPlan;
async function panel() {
  const state = (await captureSetupState(
    RuntimeSecretDraftImpact,
    (app) => app.use(createI18n({ legacy: false, locale: "ru" })),
    { draft },
  )) as unknown as {
    plan: Ref<RuntimeSecretDraftImpactPlan | undefined>;
    page: Ref<RuntimeSecretDraftImpactPage | undefined>;
    selected: Ref<string[]>;
    selectionReady: Ref<boolean>;
    pending: Ref<boolean>;
    publish(): Promise<void>;
    refresh(more?: boolean): Promise<void>;
    selectAvailable(): Promise<void>;
    toggle(ref: string): void;
  };
  state.plan.value = plan;
  state.selectionReady.value = true;
  state.page.value = { plan, items: [], total: 0, nextPageToken: "old_cursor" };
  return state;
}
beforeEach(() => vi.clearAllMocks());
it("выбирает все доступные строки плана, включая последующие серверные страницы", async () => {
  const state = await panel();
  state.selectionReady.value = false;
  state.page.value = {
    plan,
    total: 2,
    nextPageToken: "next",
    items: [{ ref: "first" }],
  } as RuntimeSecretDraftImpactPage;
  impact.readDraftImpact.mockResolvedValue({
    plan,
    total: 2,
    nextPageToken: "",
    items: [{ ref: "second" }],
  });
  await state.selectAvailable();
  expect(state.selected.value).toEqual(["first", "second"]);
  expect(state.selectionReady.value).toBe(true);
  expect(impact.readDraftImpact.mock.calls[0]?.[3]).toBe("next");
});
it("сохраняет выбор и version pins после lost ACK и читает APPLIED с первой страницы", async () => {
  const state = await panel();
  state.selected.value = ["item"];
  api.readSecretDraft.mockResolvedValue(draft);
  impact.publishSecretDraft
    .mockRejectedValueOnce(new Error("lost ACK"))
    .mockResolvedValueOnce({
      draft: { ...draft, state: "PUBLISHED" },
      secret: {},
    });
  impact.readDraftImpact.mockResolvedValue({
    plan: { ...plan, state: "APPLIED" },
    items: [],
    total: 0,
    nextPageToken: "",
  });
  await state.publish();
  expect(state.pending.value).toBe(true);
  state.toggle("other");
  expect(state.selected.value).toEqual(["item"]);
  await state.publish();
  expect(api.readSecretDraft).toHaveBeenCalledOnce();
  expect(impact.publishSecretDraft.mock.calls[0]).toEqual(
    impact.publishSecretDraft.mock.calls[1],
  );
  expect(impact.readDraftImpact.mock.calls[0]?.[3]).toBeUndefined();
  expect(state.pending.value).toBe(false);
  expect(state.plan.value?.state).toBe("APPLIED");
});
it("снимает устаревший план до публикации после изменения owner version", async () => {
  const state = await panel();
  api.readSecretDraft.mockResolvedValue({ ...draft, secretVersion: 2 });
  await state.publish();
  expect(impact.publishSecretDraft).not.toHaveBeenCalled();
  expect(state.plan.value).toBeUndefined();
  expect(state.pending.value).toBe(false);
});
it("подтверждает lost ACK через authoritative PUBLISHED/APPLIED без повторной mutation", async () => {
  const state = await panel();
  api.readSecretDraft
    .mockResolvedValueOnce(draft)
    .mockResolvedValueOnce({ ...draft, state: "PUBLISHED" });
  api.readRuntimeSecret.mockResolvedValue({ ref: "secret" });
  impact.publishSecretDraft.mockRejectedValueOnce(new Error("lost ACK"));
  impact.readDraftImpact.mockResolvedValue({
    plan: { ...plan, state: "APPLIED" },
    items: [],
    total: 0,
    nextPageToken: "",
  });
  await state.publish();
  await state.refresh();
  expect(impact.publishSecretDraft).toHaveBeenCalledOnce();
  expect(api.readRuntimeSecret).toHaveBeenCalledOnce();
  expect(state.pending.value).toBe(false);
});
