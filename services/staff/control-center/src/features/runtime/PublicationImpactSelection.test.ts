import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { createI18n } from "vue-i18n";
import type { Ref } from "vue";
import { captureSetupState } from "@/test-utils/setup-harness";
import type {
  RevisionImpactPage,
  RevisionImpactPlan,
} from "@/shared/api/generated/openapi/types.gen";

const impact = vi.hoisted(() => ({ read: vi.fn() }));
const cleanup = vi.hoisted(() => [] as Array<() => void>);
vi.mock("vue", async (original) => ({
  ...(await original<typeof import("vue")>()),
  onBeforeUnmount: (callback: () => void) => cleanup.push(callback),
}));
vi.mock("./publication-impact", async (original) => ({
  ...(await original<typeof import("./publication-impact")>()),
  readPublicationImpact: impact.read,
}));
import PublicationImpactSelection from "./PublicationImpactSelection.vue";

const plan: RevisionImpactPlan = {
  ref: "plan",
  version: 1,
  kind: "RUNTIME_ENVIRONMENT",
  sourceRef: "environment",
  sourceVersion: 3,
  sourceRevisionRef: "revision",
  draftRef: "draft",
  draftVersion: 5,
  targetDigest: "target",
  digest: "digest",
  total: 3,
  state: "PREPARED",
  createdAt: "2026-09-17T00:00:00Z",
  expiresAt: "2099-09-17T00:00:00Z",
};

function item(ref: string, outcome: "PENDING" | "CONFLICT") {
  return {
    ref,
    projectRef: "project",
    consumerKind: "AGENT" as const,
    consumerRef: `agent_${ref}`,
    consumerVersion: 1,
    bindingRef: `binding_${ref}`,
    bindingVersion: 1,
    sourceRevisionRef: "revision",
    outcome,
  };
}

function page(
  items: RevisionImpactPage["items"],
  nextPageToken = "",
): RevisionImpactPage {
  return { plan, total: 3, items, nextPageToken };
}

async function selection() {
  const i18n = createI18n({
    legacy: false,
    locale: "ru",
    messages: { ru: {} },
  });
  const state = (await captureSetupState(
    PublicationImpactSelection,
    (app) => app.use(i18n),
    { plan },
  )) as unknown as {
    page: Ref<RevisionImpactPage | undefined>;
    selected: Ref<Set<string>>;
    query: Ref<string>;
    load(more?: boolean): Promise<void>;
    toggle(ref: string): void;
  };
  await vi.waitFor(() => expect(state.page.value).toBeDefined());
  return state;
}

beforeEach(() => {
  vi.clearAllMocks();
  cleanup.length = 0;
});
afterEach(() => {
  for (const callback of cleanup) callback();
  vi.useRealTimers();
});

it("выбирает допустимые строки каждой страницы и сохраняет явное снятие", async () => {
  impact.read
    .mockResolvedValueOnce(
      page([item("first", "PENDING"), item("conflict", "CONFLICT")], "cursor"),
    )
    .mockResolvedValueOnce(page([item("second", "PENDING")]));
  const state = await selection();
  expect([...state.selected.value]).toEqual(["first"]);
  state.toggle("first");
  await state.load(true);
  expect([...state.selected.value]).toEqual(["second"]);
  expect(state.page.value?.items).toHaveLength(3);
});

it("поиск создаёт новый bounded default selection без старых refs", async () => {
  impact.read
    .mockResolvedValueOnce(page([item("first", "PENDING")]))
    .mockResolvedValueOnce(page([item("filtered", "PENDING")]));
  const state = await selection();
  state.query.value = "filtered";
  await state.load();
  expect([...state.selected.value]).toEqual(["filtered"]);
});
