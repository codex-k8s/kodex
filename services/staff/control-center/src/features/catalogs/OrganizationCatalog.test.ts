import { readFileSync } from "node:fs";
import { defineComponent, type Ref, type SetupContext } from "vue";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { captureSetupState } from "@/test-utils/setup-harness";
import type { Agent } from "@/shared/api/generated/openapi/types.gen";
import type { AppProblem } from "@/shared/api/problem";
import type { CatalogEntry, CatalogKind, CatalogPage } from "./api";

const dependencies = vi.hoisted(() => {
  const platform = {
    projects: {} as Record<string, { ref: string; name: string }>,
    agents: {} as Record<string, Agent>,
    workflows: {},
    schedules: {},
    memberships: {},
    snapshots: {} as Record<
      string,
      { scopeKey: string; nextPageToken?: string; total?: number }
    >,
    realtimeSnapshot: vi.fn<(kind: string, projectRef?: string) => unknown>(),
  };
  platform.realtimeSnapshot.mockImplementation(
    (kind, projectRef) => platform.snapshots[`${kind}:${projectRef ?? ""}`],
  );
  return {
    load: vi.fn<
      (
        kind: CatalogKind,
        query: string,
        signal: AbortSignal,
        cursor?: string,
        projectRef?: string,
      ) => Promise<CatalogPage>
    >(),
    platform,
    runtime: { environments: {} },
  };
});
vi.mock("@/features/platform/store", () => ({
  usePlatformStore: () => dependencies.platform,
}));
vi.mock("@/features/runtime/store", () => ({
  useRuntimeStore: () => dependencies.runtime,
}));
vi.mock("./api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("./api")>()),
  loadCatalog: dependencies.load,
}));
import OrganizationCatalog from "./OrganizationCatalog.vue";
import { catalogInvalidated } from "./api";

const catalogTemplate = readFileSync(
  new URL("./OrganizationCatalog.vue", import.meta.url),
  "utf8",
);
const agent = {
  ref: "agent_synthetic",
  projectRef: "project_synthetic",
  name: "Сотрудник",
  purpose: "Проверяет проект",
  state: "READY",
  version: 1,
  runtimeName: "Базовая среда",
  runtimeReady: true,
  nextActions: [],
} as unknown as Agent;
const entry: CatalogEntry = {
  ref: agent.ref,
  projectRef: agent.projectRef,
  title: agent.name,
  description: agent.purpose,
  state: agent.state,
  version: agent.version,
  path: "/projects/project_synthetic/agents/agent_synthetic",
  meta: [],
};
interface State {
  items: Ref<CatalogEntry[]>;
  query: Ref<string>;
  pageToken: Ref<string | undefined>;
  problem: Ref<AppProblem | undefined>;
  loading: Ref<boolean>;
  load(more?: boolean): Promise<void>;
  applyRealtime(): boolean;
}
async function catalog(
  projectRef?: string,
  kind: CatalogKind = "agents",
): Promise<State> {
  const source = OrganizationCatalog as unknown as {
    setup: (
      props: { kind: CatalogKind; projectRef?: string },
      context: SetupContext,
    ) => unknown;
  };
  return (await captureSetupState(
    defineComponent({
      setup(_props, context) {
        return source.setup({ kind, projectRef }, context) as Record<
          string,
          unknown
        >;
      },
    }),
  )) as unknown as State;
}

beforeEach(() => {
  vi.useFakeTimers();
  vi.clearAllMocks();
  Object.assign(dependencies.platform.projects, {
    [agent.projectRef]: { ref: agent.projectRef, name: "Проект" },
  });
  Object.assign(dependencies.platform.agents, { [agent.ref]: agent });
  Object.assign(dependencies.platform.snapshots, {
    "AGENT:": { scopeKey: "", nextPageToken: "cursor_old", total: 1 },
    [`AGENT:${agent.projectRef}`]: {
      scopeKey: agent.projectRef,
      nextPageToken: "cursor_old",
      total: 1,
    },
  });
  dependencies.load.mockResolvedValue({ items: [entry] });
});
afterEach(() => {
  for (const target of [
    dependencies.platform.projects,
    dependencies.platform.agents,
    dependencies.platform.snapshots,
  ])
    for (const key of Object.keys(target)) Reflect.deleteProperty(target, key);
  vi.useRealTimers();
});

describe("OrganizationCatalog realtime", () => {
  it("в общем пустом каталоге ведёт к выбору Проекта и не показывает пустой поиск", () => {
    expect(catalogTemplate).toContain('v-if="items.length || query.trim()"');
    expect(catalogTemplate).toContain('to="/projects"');
    expect(catalogTemplate).toContain('"emptyGlobalTitle"');
    expect(catalogTemplate).toContain('"catalog.emptyGlobalHelp"');
  });

  it("объясняет поля серверного поиска участников", () => {
    expect(catalogTemplate).toContain("catalog.memberSearchPlaceholder");
    expect(catalogTemplate).toContain("catalog.searchPlaceholder");
    expect(catalogTemplate).toContain(":aria-label=");
    expect(catalogTemplate).toContain(":placeholder=");
  });

  it("показывает один общий табличный реестр с точными переходами к Проекту", () => {
    expect(catalogTemplate).toContain('v-if="items.length"');
    expect(catalogTemplate).toContain("<table");
    expect(catalogTemplate).toContain(
      ':to="`/projects/${encodeURIComponent(entry.projectRef)}/${kind}`"',
    );
    expect(catalogTemplate).toContain("organization-catalog__project-link");
    expect(catalogTemplate).not.toContain('<EntityIcon kind="PROJECT" />');
    expect(catalogTemplate).toContain("AgentAvatar");
    expect(catalogTemplate).toContain("workflowLaunch(entry.workflow)");
    expect(catalogTemplate).not.toContain("<AgentCard");
    expect(catalogTemplate).not.toContain("<WorkflowCard");
    expect(catalogTemplate).not.toContain("expandedProject");
    expect(catalogTemplate).not.toContain("<ModalDialog");
  });

  it("ставит название сущности первым, а Проект показывает отдельной ячейкой без иконки", () => {
    const header = catalogTemplate.slice(
      catalogTemplate.indexOf("<thead>"),
      catalogTemplate.indexOf("</thead>"),
    );
    expect(header.indexOf("catalog.table.name")).toBeLessThan(
      header.indexOf("catalog.table.project"),
    );
    const row = catalogTemplate.slice(
      catalogTemplate.indexOf('<tr\n            v-for="entry in items"'),
      catalogTemplate.indexOf('class="organization-catalog__description"'),
    );
    expect(row.indexOf('class="organization-catalog__identity"')).toBeLessThan(
      row.indexOf('class="organization-catalog__project-link"'),
    );
    expect(row.slice(row.indexOf('<td v-if="!projectRef">'))).not.toContain(
      "<EntityIcon",
    );
  });

  it("первую страницу берёт из realtime snapshot без HTTP", async () => {
    const state = await catalog();
    expect(state.items.value[0]).toMatchObject({
      ref: entry.ref,
      projectRef: entry.projectRef,
      title: entry.title,
      version: entry.version,
    });
    expect(state.pageToken.value).toBe("cursor_old");
    expect(state.loading.value).toBe(false);
    expect(dependencies.load).not.toHaveBeenCalled();
  });

  it("внутри Проекта оставляет только его snapshot", async () => {
    dependencies.platform.agents.agent_foreign = {
      ...agent,
      ref: "agent_foreign",
      projectRef: "project_foreign",
    };
    const state = await catalog(agent.projectRef);
    expect(state.items.value.map((item) => item.ref)).toEqual([agent.ref]);
    expect(dependencies.load).not.toHaveBeenCalled();
  });

  it("cursor-догрузку выполняет через сервер и сохраняет первый snapshot", async () => {
    const state = await catalog();
    dependencies.load.mockResolvedValueOnce({
      items: [{ ...entry, ref: "agent_later" }],
    });
    await state.load(true);
    expect(dependencies.load.mock.calls[0]?.[3]).toBe("cursor_old");
    expect(state.items.value.map((item) => item.ref)).toEqual([
      agent.ref,
      "agent_later",
    ]);
  });

  it("непустой поиск выполняет на сервере", async () => {
    const state = await catalog();
    state.query.value = "аналитик";
    await state.load();
    expect(dependencies.load).toHaveBeenCalledOnce();
    expect(dependencies.load.mock.calls[0]?.slice(0, 2)).toEqual([
      "agents",
      "аналитик",
    ]);
  });

  it("возврат к пустому запросу отменяет HTTP и сразу восстанавливает snapshot", async () => {
    let resolve!: (page: CatalogPage) => void;
    dependencies.load.mockReturnValueOnce(
      new Promise<CatalogPage>((done) => {
        resolve = done;
      }),
    );
    const state = await catalog();
    state.query.value = "поиск";
    const pending = state.load();
    const signal = dependencies.load.mock.calls[0]?.[2];
    state.query.value = "";
    state.applyRealtime();
    expect(signal?.aborted).toBe(true);
    expect(state.items.value.map((item) => item.ref)).toEqual([agent.ref]);
    resolve({ items: [{ ...entry, ref: "stale" }] });
    await pending;
    expect(state.items.value.map((item) => item.ref)).toEqual([agent.ref]);
  });

  it("применяет новую версию snapshot без HTTP readback", async () => {
    const state = await catalog();
    dependencies.platform.agents[agent.ref] = { ...agent, version: 2 };
    state.applyRealtime();
    expect(state.items.value[0]?.version).toBe(2);
    expect(dependencies.load).not.toHaveBeenCalled();
  });

  it("не выдумывает отсутствующие ENVIRONMENT/SECRET event kinds", () => {
    expect(catalogInvalidated("agents", "AGENT")).toBe(true);
    expect(catalogInvalidated("workflows", "WORKFLOW")).toBe(true);
    expect(catalogInvalidated("automations", "SCHEDULE")).toBe(true);
    expect(catalogInvalidated("environments", "ENVIRONMENT")).toBe(false);
    expect(catalogInvalidated("secrets", "SECRET")).toBe(false);
  });
});
