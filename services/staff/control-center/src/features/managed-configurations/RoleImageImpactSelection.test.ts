import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { createI18n } from "vue-i18n";
import { createPinia, setActivePinia } from "pinia";
import type { ComputedRef, Ref } from "vue";
import { captureSetupState } from "@/test-utils/setup-harness";
import { usePlatformStore } from "@/features/platform/store";
import { useRuntimeStore } from "@/features/runtime/store";
import type {
  Agent,
  BootstrapState,
  RoleImageImpactItem,
  RoleImageImpactPage,
  RoleImageImpactPlan,
  RuntimeEnvironmentSet,
} from "@/shared/api/generated/openapi/types.gen";

const impact = vi.hoisted(() => ({ read: vi.fn() }));
const cleanup = vi.hoisted(() => [] as Array<() => void>);
vi.mock("vue", async (original) => ({
  ...(await original<typeof import("vue")>()),
  onBeforeUnmount: (callback: () => void) => cleanup.push(callback),
}));
vi.mock("./role-image-impact", async (original) => ({
  ...(await original<typeof import("./role-image-impact")>()),
  readImageImpact: impact.read,
}));
import RoleImageImpactSelection from "./RoleImageImpactSelection.vue";

const plan: RoleImageImpactPlan = {
  ref: "plan_synthetic",
  version: 1,
  configurationRef: "configuration_synthetic",
  configurationVersion: 2,
  revisionRef: "revision_synthetic",
  revisionDigest: "revision-digest",
  recipeRef: "recipe_synthetic",
  recipeGeneration: 3,
  buildRef: "build_synthetic",
  artifactRef: "artifact_synthetic",
  artifactDigest: "artifact-digest",
  admissionPolicyDigest: "admission-digest",
  digest: "plan-digest",
  total: 2,
  state: "PREPARED",
  createdAt: "2026-09-06T00:00:00Z",
  expiresAt: "2099-09-06T00:00:00Z",
};
const environmentItem: RoleImageImpactItem = {
  ref: "impact_environment",
  environmentRef: "environment_synthetic",
  environmentVersion: 4,
  sourceVersionRef: "source_synthetic",
  sourceVersionDigest: "source-digest",
  scopeKind: "PROJECT",
  projectRef: "project_synthetic",
  organizationRef: "org_synthetic",
  outcome: "PENDING",
};
const agentItem: RoleImageImpactItem = {
  ...environmentItem,
  ref: "impact_agent",
  consumer: {
    agentRef: "agent_synthetic",
    agentVersion: 2,
    bindingRef: "binding_synthetic",
    bindingVersion: 3,
    versionRef: environmentItem.sourceVersionRef,
    scopeKind: "PROJECT",
    projectRef: environmentItem.projectRef,
    organizationRef: environmentItem.organizationRef,
  },
};

function cachedEnvironment(
  overrides: Partial<RuntimeEnvironmentSet> = {},
): RuntimeEnvironmentSet {
  return {
    ref: environmentItem.environmentRef,
    scopeKind: environmentItem.scopeKind,
    projectRef: environmentItem.projectRef,
    organizationRef: environmentItem.organizationRef,
    name: "Рабочее окружение команды",
    version: 4,
    description: "",
    state: "ACTIVE",
    currentVersion: {} as RuntimeEnvironmentSet["currentVersion"],
    updatedAt: plan.createdAt,
    ready: true,
    readinessBlockers: [],
    nextActions: [],
    ...overrides,
  };
}
function cachedAgent(overrides: Partial<Agent> = {}): Agent {
  return {
    ref: "agent_synthetic",
    projectRef: environmentItem.projectRef,
    name: "Помощник команды",
    ...overrides,
  } as Agent;
}
interface DisplayItem {
  item: RoleImageImpactItem;
  title: string;
  environmentName: string;
  reference: string;
}
async function selection(items = [environmentItem, agentItem]) {
  impact.read.mockResolvedValue({ plan, items, total: items.length });
  const state = (await captureSetupState(
    RoleImageImpactSelection,
    (app) =>
      app.use(
        createI18n({
          legacy: false,
          locale: "ru",
          messages: {
            ru: {
              nav: { agent: "ИИ-сотрудник", environment: "Окружение" },
            },
          },
        }),
      ),
    { plan },
  )) as unknown as {
    page: Ref<RoleImageImpactPage | undefined>;
    displayItems: ComputedRef<DisplayItem[]>;
    selected: Ref<Set<string>>;
    toggle(ref: string): void;
  };
  await vi.waitFor(() => expect(state.page.value).toBeDefined());
  return state;
}

beforeEach(() => {
  setActivePinia(createPinia());
  vi.clearAllMocks();
  cleanup.length = 0;
  usePlatformStore().bootstrap = {
    organizationRef: "org_synthetic",
  } as BootstrapState;
});
afterEach(() => {
  for (const callback of cleanup) callback();
});

it("показывает имена точных scoped сотрудников и окружений, сохраняя технические refs", async () => {
  useRuntimeStore().environments[environmentItem.environmentRef] =
    cachedEnvironment();
  usePlatformStore().agents.agent_synthetic = cachedAgent();
  const state = await selection();
  expect(state.displayItems.value.map((row) => row.title)).toEqual([
    "Рабочее окружение команды",
    "Помощник команды",
  ]);
  expect(state.displayItems.value[1]?.environmentName).toBe(
    "Рабочее окружение команды",
  );
  expect(state.displayItems.value.map((row) => row.reference)).toEqual([
    "environment_synthetic",
    "agent_synthetic",
  ]);
  expect(state.selected.value.size).toBe(0);
  state.toggle("agent_synthetic");
  expect(state.selected.value.size).toBe(0);
  state.toggle(agentItem.ref);
  expect([...state.selected.value]).toEqual([agentItem.ref]);
});

it.each([
  { organizationRef: "org_foreign" },
  { projectRef: "project_foreign" },
  { scopeKind: "ORGANIZATION" as const, projectRef: "" },
  { ref: "environment_foreign" },
  { state: "DELETED" as const },
  { name: "   " },
])(
  "не берёт имя окружения из несовпадающего или недоступного cache: %j",
  async (override) => {
    useRuntimeStore().environments[environmentItem.environmentRef] =
      cachedEnvironment(override);
    const state = await selection([environmentItem]);
    expect(state.displayItems.value[0]?.title).toBe("Окружение");
  },
);

it.each([
  { projectRef: "project_foreign" },
  { ref: "agent_foreign" },
  { name: "   " },
])(
  "не берёт имя сотрудника из несовпадающего или пустого cache: %j",
  async (override) => {
    usePlatformStore().agents.agent_synthetic = cachedAgent(override);
    const state = await selection([agentItem]);
    expect(state.displayItems.value[0]?.title).toBe("ИИ-сотрудник");
  },
);

it("не раскрывает cache имена при смене organization или consumer scope", async () => {
  const consumer = agentItem.consumer;
  if (!consumer) throw new Error("Synthetic consumer is missing");
  useRuntimeStore().environments[environmentItem.environmentRef] =
    cachedEnvironment();
  usePlatformStore().agents.agent_synthetic = cachedAgent();
  const state = await selection();
  usePlatformStore().bootstrap = {
    organizationRef: "org_foreign",
  } as BootstrapState;
  expect(state.displayItems.value.map((row) => row.title)).toEqual([
    "Окружение",
    "ИИ-сотрудник",
  ]);
  expect(state.displayItems.value[1]?.environmentName).toBe("");
  usePlatformStore().bootstrap = {
    organizationRef: "org_synthetic",
  } as BootstrapState;
  state.page.value = {
    plan,
    total: 1,
    items: [
      {
        ...agentItem,
        consumer: {
          ...consumer,
          organizationRef: "org_foreign",
        },
      },
    ],
  };
  expect(state.displayItems.value[0]?.title).toBe("ИИ-сотрудник");
});

it("реактивно заменяет fallback при появлении точного имени в scoped cache", async () => {
  const state = await selection();
  expect(state.displayItems.value[0]?.title).toContain("Окружение");
  expect(state.displayItems.value[1]?.title).toContain("ИИ-сотрудник");
  useRuntimeStore().environments[environmentItem.environmentRef] =
    cachedEnvironment();
  usePlatformStore().agents.agent_synthetic = cachedAgent();
  expect(state.displayItems.value.map((row) => row.title)).toEqual([
    "Рабочее окружение команды",
    "Помощник команды",
  ]);
});

it("разрешает имя ORGANIZATION окружения только с точным owner scope", async () => {
  const item = {
    ...environmentItem,
    scopeKind: "ORGANIZATION" as const,
    projectRef: "",
  };
  useRuntimeStore().environments[environmentItem.environmentRef] =
    cachedEnvironment({ scopeKind: "ORGANIZATION", projectRef: "" });
  const state = await selection([item]);
  expect(state.displayItems.value[0]?.title).toBe("Рабочее окружение команды");
});
