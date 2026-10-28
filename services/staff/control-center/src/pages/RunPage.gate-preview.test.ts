import { createPinia } from "pinia";
import { renderToString } from "@vue/server-renderer";
import { createSSRApp, type Component, type Ref, type SetupContext } from "vue";
import { createMemoryHistory, createRouter } from "vue-router";
import { createI18n } from "vue-i18n";
import { afterEach, expect, it, vi } from "vitest";
import { i18n as applicationI18n } from "@/app/i18n";
import { usePlatformStore } from "@/features/platform/store";
import type { OwnerGate, Run } from "@/shared/api/generated/openapi/types.gen";
import RunPage from "./RunPage.vue";

vi.mock("@/shared/locale", () => ({ currentLocale: () => "ru" }));

afterEach(() => vi.unstubAllGlobals());

// Открываем только локальное состояние модалки, оставляя настоящий setup и
// template страницы: проверка не заменяет production-разметку тестовым HTML.
async function renderGate(
  intent?: OwnerGate["integrationIntent"],
): Promise<string> {
  vi.stubGlobal("window", {
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    setTimeout,
    clearTimeout,
  });
  const pinia = createPinia();
  const platform = usePlatformStore(pinia);
  platform.bootstrap = { organizationRef: "org_synthetic" } as NonNullable<
    typeof platform.bootstrap
  >;
  const run: Run = {
    ref: "run_preview",
    rootRunRef: "run_preview",
    sessionRef: "ses_preview",
    projectRef: "prj_preview",
    version: 1,
    title: "Техническая проверка",
    titleSource: "USER_EDITED",
    source: "CONTROL_CENTER",
    target: {
      type: "AGENT",
      ref: "agt_preview",
      displayName: "QA",
      version: 1,
    },
    state: "WAITING_HUMAN",
    activitySummary: "Ожидает решения",
    initiator: { ref: "usr_owner", displayName: "Владелец" },
    attempt: 1,
    graphRevision: 1,
    lastEventSequence: 0,
    artifactRefs: [],
    gateRefs: ["gat_preview"],
    nextActions: [],
    createdAt: "2026-10-25T00:00:00Z",
    usage: {
      totalTokens: 0,
      inputTokens: 0,
      outputTokens: 0,
      cachedInputTokens: 0,
      cacheWriteInputTokens: 0,
      reasoningOutputTokens: 0,
      modelContextWindow: 0,
    },
  };
  platform.runs[run.ref] = run;
  platform.graphs[run.ref] = {
    runRef: run.ref,
    revision: 1,
    sequence: 1,
    edges: [],
    nodes: [
      {
        ref: "nod_preview",
        runRef: run.ref,
        type: "HUMAN_GATE",
        state: "WAITING",
        displayName: "Решение",
        role: "Решение владельца",
        attempt: 1,
        artifactRefs: [],
        childRunRefs: [],
        nextActions: [],
        createdAt: run.createdAt,
      },
    ],
  };
  platform.gates.gat_preview = {
    ref: "gat_preview",
    scopeKind: "PROJECT",
    organizationRef: "org_synthetic",
    projectRef: run.projectRef,
    version: 1,
    runRef: run.ref,
    nodeRef: "nod_preview",
    title: "Проверить действие",
    contextSummary: "Обычный вопрос",
    consequencesSummary: "Будет выполнено согласованное действие",
    requestedBy: { ref: "agt_preview", displayName: "QA" },
    state: "OPEN",
    allowedDecisions: ["APPROVE", "REJECT"],
    decisionConsequences: [],
    openedAt: "2026-10-25T00:00:00Z",
    nextActions: ["RESOLVE_GATE"],
    integrationIntent: intent,
  };
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: "/runs/:runRef", component: RunPage }],
  });
  await router.push(`/runs/${run.ref}`);
  type Page = Component & {
    setup(props: unknown, context: SetupContext): Record<string, unknown>;
  };
  const original = RunPage as Page;
  const opened = {
    ...RunPage,
    setup(props: unknown, context: SetupContext) {
      const state = original.setup(props, context);
      (state.gateDialogOpen as Ref<boolean>).value = true;
      return state;
    },
  };
  const app = createSSRApp(opened)
    .use(pinia)
    .use(router)
    .use(
      createI18n({
        legacy: false,
        locale: "ru",
        messages: { ru: applicationI18n.global.getLocaleMessage("ru") },
      }),
    );
  return renderToString(app);
}

it("до согласования показывает точные поля и journal в настоящей модалке запуска", async () => {
  const html = await renderGate({
    connectionRef: "int_preview",
    connectionName: "Техническая QA оснастка",
    definitionKey: "synthetic",
    capabilityKey: "synthetic.journal.write",
    operation: "synthetic.journal.write",
    effectKey: "effect_preview",
    resourceScope: {
      kind: "SYNTHETIC_JOURNAL",
      values: { journal: "qa1830-exact-journal" },
      digest: "a".repeat(64),
    },
    effectPreview: {
      risk: "WRITE",
      approvalPolicy: "HUMAN_EACH_EFFECT",
      contentComplete: true,
      fields: [
        {
          key: "action",
          type: "STRING",
          value: "CREATE",
          opaque: false,
          truncated: false,
        },
        {
          key: "value",
          type: "STRING",
          value: "Точное содержимое QA",
          opaque: false,
          truncated: false,
        },
        {
          key: "fault",
          type: "STRING",
          value: "RETRYABLE_ONCE",
          opaque: false,
          truncated: false,
        },
      ],
    },
  });
  expect(html).toContain('role="dialog"');
  for (const value of [
    "CREATE",
    "Точное содержимое QA",
    "RETRYABLE_ONCE",
    "qa1830-exact-journal",
  ])
    expect(html).toContain(value);
  const primary = html.replace(/<details\b[^]*?<\/details>/g, "");
  expect(primary).toContain("qa1830-exact-journal");
  expect(primary.indexOf("Точное содержимое QA")).toBeLessThan(
    primary.indexOf('class="gate-actions"'),
  );
});

it("обычный Gate сохраняет исходный вопрос без integration preview", async () => {
  const html = await renderGate();
  expect(html).toContain("Обычный вопрос");
  expect(html).not.toContain("decision-integration-intent");
});
