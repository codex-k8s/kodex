import { createPinia } from "pinia";
import { createSSRApp, defineComponent, h, ref } from "vue";
import { renderToString } from "@vue/server-renderer";
import { createI18n } from "vue-i18n";
import { createMemoryHistory, createRouter } from "vue-router";
import { describe, expect, it, vi } from "vitest";
const gateProjectReadback = vi.hoisted(() => ({ available: true }));
vi.mock("@/shared/locale", () => ({ currentLocale: () => "ru" }));
vi.mock("@/features/workboard/gate-catalog", () => ({
  useGateCatalog: () => ({
    items: ref([gate]),
    total: ref(91),
    pageToken: ref(),
    loading: ref(false),
    problem: ref(),
    load: vi.fn(),
    invalidate: vi.fn(),
    reset: vi.fn(),
    applySnapshot: vi.fn(),
  }),
}));
vi.mock("@/features/workboard/gate-projects", () => ({
  useGateProjects: () => ({
    projects: ref(
      gateProjectReadback.available ? { [project.ref]: project } : {},
    ),
    ensure: vi.fn(),
    dispose: vi.fn(),
  }),
}));
import { i18n as applicationI18n } from "@/app/i18n";

import { usePlatformStore } from "@/features/platform/store";
import DecisionsPage from "@/pages/DecisionsPage.vue";
import type {
  AuditEvent,
  OwnerGate,
  Project,
  Run,
} from "@/shared/api/generated/openapi/types.gen";

const project: Project = {
  ref: "prj_sales",
  version: 1,
  name: "Продажи",
  purpose: "Работа с клиентами",
  language: "ru",
  lifecycle: "ACTIVE",
  agentCount: 1,
  integrationState: "NONE",
  workflowCount: 1,
  activeRunCount: 1,
  pendingGateCount: 1,
  updatedAt: "2026-08-29T10:00:00Z",
  nextActions: [],
};

const run: Run = {
  ref: "run_offer",
  version: 1,
  projectRef: project.ref,
  sessionRef: "ses_offer",
  rootRunRef: "run_offer",
  target: {
    type: "AGENT",
    ref: "agt_sales",
    displayName: "Менеджер продаж",
    version: 1,
  },
  title: "Согласование коммерческого предложения",
  titleSource: "USER_EDITED",
  activitySummary: "Ожидает решения владельца",
  state: "WAITING_HUMAN",
  source: "CONTROL_CENTER",
  initiator: { ref: "usr_owner", displayName: "Владелец" },
  attempt: 1,
  graphRevision: 2,
  lastEventSequence: 3,
  usage: {
    totalTokens: 0,
    inputTokens: 0,
    cachedInputTokens: 0,
    cacheWriteInputTokens: 0,
    outputTokens: 0,
    reasoningOutputTokens: 0,
    modelContextWindow: 0,
  },
  artifactRefs: [],
  gateRefs: ["gat_offer"],
  createdAt: "2026-08-29T10:00:00Z",
  nextActions: [],
};

const gate: OwnerGate = {
  ref: "gat_offer",
  scopeKind: "PROJECT",
  organizationRef: "org_synthetic",
  version: 1,
  projectRef: project.ref,
  runRef: run.ref,
  nodeRef: "nod_offer_gate",
  title: "Утвердить отправку предложения",
  contextSummary: "Проверены цена, срок и состав работ.",
  consequencesSummary: "i18n:INTEGRATION_EFFECT_GATE_PROMPT",
  requestedBy: { ref: "agt_sales", displayName: "Менеджер продаж" },
  state: "OPEN",
  allowedDecisions: ["APPROVE", "REQUEST_CHANGES", "REJECT"],
  decisionConsequences: [
    {
      decision: "APPROVE",
      safeSummary: "Агент отправит предложение клиенту",
      executesExternalEffect: true,
      terminalForRun: false,
    },
    {
      decision: "REQUEST_CHANGES",
      safeSummary: "Агент скорректирует предложение",
      executesExternalEffect: false,
      terminalForRun: false,
    },
    {
      decision: "REJECT",
      safeSummary: "Запуск завершится без отправки",
      executesExternalEffect: false,
      terminalForRun: true,
    },
  ],
  openedAt: "2026-08-29T10:05:00Z",
  nextActions: ["RESOLVE_GATE"],
};

const auditEvent: AuditEvent = {
  ref: "aud_gate_opened",
  projectRef: project.ref,
  initiator: { ref: "usr_owner", displayName: "Владелец" },
  executor: "control-plane",
  source: "CONTROL_CENTER",
  action: "OWNER_GATE_OPENED",
  resourceType: "OWNER_GATE",
  resourceRef: gate.ref,
  resourceName: gate.title,
  outcome: "SUCCEEDED",
  safeSummary: "i18n:OWNER_GATE_REVIEW_PROMPT",
  occurredAt: "2026-08-29T10:05:00Z",
};

describe("DecisionsPage", () => {
  async function renderIntegration(
    intent: NonNullable<OwnerGate["integrationIntent"]>,
    locale: "ru" | "en" = "ru",
  ): Promise<string> {
    const original = gate.integrationIntent;
    gate.integrationIntent = intent;
    try {
      const pinia = createPinia();
      const router = createRouter({
        history: createMemoryHistory(),
        routes: [{ path: "/decisions", component: DecisionsPage }],
      });
      await router.push("/decisions");
      await router.isReady();
      const platform = usePlatformStore(pinia);
      platform.projects[project.ref] = project;
      platform.runs[run.ref] = run;
      platform.gates[gate.ref] = gate;
      const i18n = createI18n({
        legacy: false,
        locale,
        messages: { [locale]: applicationI18n.global.getLocaleMessage(locale) },
      });
      const app = createSSRApp(DecisionsPage);
      app.use(pinia);
      app.use(router);
      app.use(i18n);
      return await renderToString(app);
    } finally {
      gate.integrationIntent = original;
    }
  }
  const commentIntent = (
    fields: unknown[],
    contentComplete = true,
  ): NonNullable<OwnerGate["integrationIntent"]> => ({
    connectionRef: "intconn_fixture",
    connectionName: "Fixture GitHub",
    definitionKey: "github",
    capabilityKey: "github.issue.comment.create",
    operation: "github.issue.comment.create",
    resourceScope: {
      kind: "GITHUB_REPOSITORY",
      values: { owner: "fixture-owner", repository: "fixture-repo" },
      digest: "a".repeat(64),
    },
    effectPreview: {
      risk: "WRITE",
      contentComplete,
      fields,
      inputDigest: "b".repeat(64),
      inputBytes: 42,
    },
    effectKey: "effect_fixture",
  });

  it.each(["ru", "en"] as const)(
    "показывает repo/Issue/comment из actual typed key fields ДО решения (%s)",
    async (locale) => {
      const html = await renderIntegration(
        commentIntent([
          { key: "issue_number", type: "INTEGER", value: 17, opaque: false },
          {
            key: "body",
            type: "STRING",
            value: "Точный внешний комментарий\nВторая строка",
            opaque: false,
            truncated: false,
          },
        ]),
        locale,
      );
      const details =
        /<details\b[^>]*class="decision-technical-details"[^>]*>[^]*?<\/details>/.exec(
          html,
        )?.[0] ?? "";
      expect(details).not.toBe("");
      const primary = html.replace(details, "");
      expect(primary).toContain("fixture-owner/fixture-repo");
      expect(primary).toContain(
        locale === "ru"
          ? "Добавить комментарий к Issue #17"
          : "Add a comment to Issue #17",
      );
      expect(primary).toContain(
        locale === "ru" ? "Текст комментария" : "Comment text",
      );
      expect(primary).toContain("Точный внешний комментарий\nВторая строка");
      expect(primary).not.toContain("Безопасное описание действия неполное");
      expect(primary).not.toMatch(
        /effect_fixture|github.issue.comment.create|inputDigest/,
      );
      expect(primary.indexOf("Точный внешний комментарий")).toBeLessThan(
        primary.indexOf('class="decision-actions"'),
      );
      expect(details).toContain("effect_fixture");
      expect(details).not.toContain("Точный внешний комментарий");
      expect(details).not.toMatch(/<details[^>]*\bopen\b/);
      expect(html.match(/type="radio"/g)).toHaveLength(3);
    },
  );

  it("opaque/header/secret value никогда не появляются даже в technical details", async () => {
    const intent = commentIntent([
      {
        key: "body",
        type: "STRING",
        value: "OPAQUE_VALUE_SENTINEL",
        opaque: true,
        truncated: false,
      },
      {
        key: "authorization",
        type: "STRING",
        value: "HEADER_VALUE_SENTINEL",
        opaque: false,
        truncated: false,
      },
      {
        key: "secret",
        type: "STRING",
        value: "SECRET_VALUE_SENTINEL",
        opaque: false,
        truncated: false,
      },
    ]);
    intent.effectPreview.headers = { authorization: "EXTRA_HEADER_SENTINEL" };
    const html = await renderIntegration(intent);
    expect(html).not.toMatch(
      /OPAQUE_VALUE_SENTINEL|HEADER_VALUE_SENTINEL|SECRET_VALUE_SENTINEL|EXTRA_HEADER_SENTINEL/,
    );
    expect(html).toContain("Значение скрыто в безопасном описании");
    expect(html).toContain("Безопасное описание действия неполное");
    expect(html).toContain("Риск внешнего действия");
  });

  it("показывает безопасный truncated prefix с явным предупреждением, не как полный body", async () => {
    const html = await renderIntegration(
      commentIntent(
        [
          {
            key: "body",
            type: "STRING",
            value: "Доступный префикс",
            opaque: false,
            truncated: true,
          },
        ],
        false,
      ),
    );
    expect(html).toContain("Доступный префикс");
    expect(html).toContain("Показана только часть значения");
    expect(html).toContain("Безопасное описание действия неполное");
    expect(html.indexOf("Безопасное описание действия неполное")).toBeLessThan(
      html.indexOf('class="decision-actions"'),
    );
  });

  it("показывает неполноту при отсутствующем safe preview и сохраняет generic action/risk", async () => {
    const intent = commentIntent([]);
    intent.effectPreview = { risk: "DESTRUCTIVE" };
    const html = await renderIntegration(intent);
    expect(html).toContain("Безопасное описание действия неполное");
    expect(html).toContain("необратимое изменение");
    expect(html).not.toContain("Добавить комментарий к Issue #");
  });

  it("body остаётся буквальным escaped text без исполняемого HTML/Markdown", async () => {
    const html = await renderIntegration(
      commentIntent([
        {
          key: "body",
          type: "STRING",
          value:
            '<img src=x onerror="unexpected()"> [link](https://fixture.invalid)',
          opaque: false,
          truncated: false,
        },
      ]),
    );
    expect(html).toContain("&lt;img");
    expect(html).not.toContain("<img src=x");
    expect(html).not.toContain('href="https://fixture.invalid"');
  });

  it("показывает организационный SYSTEM gate без фиктивного проекта", async () => {
    const originalProject = gate.projectRef;
    gate.scopeKind = "ORGANIZATION";
    Reflect.deleteProperty(gate, "projectRef");
    try {
      const pinia = createPinia();
      const router = createRouter({
        history: createMemoryHistory(),
        routes: [
          { path: "/decisions", component: DecisionsPage },
          {
            path: "/:pathMatch(.*)*",
            component: defineComponent({ render: () => h("div") }),
          },
        ],
      });
      await router.push("/decisions");
      await router.isReady();
      const platform = usePlatformStore(pinia);
      platform.gates[gate.ref] = gate;
      const app = createSSRApp(DecisionsPage);
      app.use(pinia);
      app.use(router);
      app.use(applicationI18n);
      const html = await renderToString(app);
      expect(html).toContain("Организация · общесистемный помощник");
      expect(html).not.toContain("Название Проекта недоступно");
      expect(html).not.toContain("/projects/undefined");
      expect(html).not.toContain("/projects/prj_sales");
    } finally {
      gate.scopeKind = "PROJECT";
      gate.projectRef = originalProject;
    }
  });
  it("не раскрывает имя из старого общего кэша после отказа адресного чтения", async () => {
    gateProjectReadback.available = false;
    try {
      const pinia = createPinia();
      const router = createRouter({
        history: createMemoryHistory(),
        routes: [{ path: "/decisions", component: DecisionsPage }],
      });
      await router.push("/decisions");
      await router.isReady();
      const platform = usePlatformStore(pinia);
      platform.projects[project.ref] = project;
      const app = createSSRApp(DecisionsPage);
      app.use(pinia);
      app.use(router);
      app.use(applicationI18n);
      const html = await renderToString(app);
      expect(html).toContain("Название Проекта недоступно");
      expect(html).not.toContain(project.name);
    } finally {
      gateProjectReadback.available = true;
    }
  });

  it("показывает сгруппированный контекст и ведёт на точный узел запуска", async () => {
    const pinia = createPinia();
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: "/decisions", component: DecisionsPage },
        {
          path: "/:pathMatch(.*)*",
          component: defineComponent({ render: () => h("div") }),
        },
      ],
    });
    await router.push("/decisions");
    await router.isReady();

    const platform = usePlatformStore(pinia);
    platform.projects[project.ref] = project;
    platform.runs[run.ref] = run;
    platform.gates[gate.ref] = gate;
    platform.auditEvents = [auditEvent];
    const i18n = createI18n({
      legacy: false,
      locale: "ru",
      messages: {
        ru: {
          app: { project: "Проект" },
          common: {
            approve: "Одобрить",
            reject: "Отклонить",
            requestChanges: "Запросить изменения",
            cancel: "Отменить",
            unknownStatus: "Неизвестно",
            unavailable: "Недоступно",
          },
          runs: {
            sessionNode: "Сессия",
            context: "Контекст узла",
            attempt: "Попытка {attempt}",
          },
          decisions: {
            ...applicationI18n.global.getLocaleMessage("ru").decisions,
            title: "Решения",
            subtitle: "Вопросы, ожидающие ответа",
            pending: "Ожидают",
            pendingAccessible: "Решения, ожидающие ответа",
            history: "История",
            projectFilter: "Проект",
            allProjects: "Все Проекты",
            pendingCount: "Ожидают ответа: {count}",
            historyCount: "В истории: {count}",
            emptyTitle: "Нет ожидающих решений",
            emptyText: "Вопросов нет",
            historyEmpty: "История решений пуста",
            historyEmptyText: "Завершённых решений нет",
            urgency: {
              OVERDUE: "Срок истёк",
              SOON: "Срочно",
              NORMAL: "Обычный приоритет",
            },
            projectUnavailable: "Название Проекта недоступно",
            runUnavailable: "Название запуска недоступно",
            question: "Решение человека",
            fullQuestion: "Что нужно решить",
            questionUnavailable: "Текст вопроса недоступен",
            consequences: "Что произойдёт",
            consequencesUnavailable: "Последствия недоступны",
            process: "Запуск и точный узел",
            openNode: "Открыть точный узел",
            requestedBy: "Запросил",
            openedAt: "Запрошено",
            deadline: "Срок ответа",
            noDeadline: "Без срока",
            evidence: "Материалы",
            evidenceCount: "Открыть материалы: {count}",
            noEvidence: "Материалы не приложены",
            comment: "Комментарий",
            commentPlaceholder: "Добавьте комментарий",
            outcome: "Принятое решение",
            actionsUnavailable: "Ответ недоступен",
            actionsUnavailableText: "Нет разрешённого действия",
          },
          serverMessages:
            applicationI18n.global.getLocaleMessage("ru").serverMessages,
          states: {
            OPEN: "Открыто",
            CLEAN: "Проверен",
            APPROVED: "Одобрено",
            CHANGES_REQUESTED: "Нужны изменения",
            REJECTED: "Отклонено",
          },
        },
      },
    });

    const app = createSSRApp(DecisionsPage);
    app.use(pinia);
    app.use(router);
    app.use(i18n);
    const html = await renderToString(app);

    expect(html).toContain("Продажи");
    expect(html).toContain("Согласование коммерческого предложения");
    expect(html).toContain("Проверены цена, срок и состав работ.");
    expect(html).toContain(
      "Проверьте последствия внешнего действия и примите решение",
    );
    expect(html).toContain("Менеджер продаж");
    expect(html).toContain("Согласование коммерческого предложения");
    expect(html).toContain("nodeRef=nod_offer_gate");
    expect(html).toContain("Сессия");
    expect(html).toContain("Попытка 1");
    expect(html).toContain("Инициатор Run");
    expect(html).toContain("Проверьте результат и примите решение");
    expect(html).not.toContain("i18n:");
    expect(html).toContain("OWNER_GATE_OPENED · SUCCEEDED");
    expect(html.match(/type="radio"/g)).toHaveLength(3);
    expect(html).toContain('data-state="APPROVED"');
    expect(html).toContain('data-state="CHANGES_REQUESTED"');
    expect(html).toContain('data-state="REJECTED"');
    expect(html.match(/Комментарий обязателен/g)).toHaveLength(2);
    expect(html.match(/button--primary/g)).toHaveLength(1);
    expect(html).toContain("Запросить изменения");
    expect(html).toContain("Отклонить");
  });

  it("убирает внутренние идентификаторы интеграции из основного слоя решения", async () => {
    gate.integrationIntent = {
      connectionRef: "intconn_fixture",
      connectionName: "Тестовый сервис",
      definitionKey: "fixture",
      capabilityKey: "openapi.op.internal",
      operation: "op.internal",
      resourceScope: {
        kind: "HTTPS_RESOURCE",
        values: {},
        digest: "b".repeat(64),
      },
      effectPreview: {
        risk: "WRITE",
        inputDigest: "a".repeat(64),
        inputBytes: 42,
        fields: [
          { path: "/body/value", type: "string", value: "новое значение" },
        ],
        approvalScope: {
          selected: [
            { path: "/body/value", type: "string", value: "новое значение" },
          ],
          mutablePaths: ["/body/comment"],
        },
      },
      effectKey: "eff_internal",
    };
    const originalContextSummary = gate.contextSummary;
    gate.contextSummary = 'op.internal {"body":{"value":"новое значение"}}';
    try {
      const pinia = createPinia();
      const router = createRouter({
        history: createMemoryHistory(),
        routes: [
          { path: "/decisions", component: DecisionsPage },
          {
            path: "/:pathMatch(.*)*",
            component: defineComponent({ render: () => h("div") }),
          },
        ],
      });
      await router.push("/decisions");
      await router.isReady();
      const platform = usePlatformStore(pinia);
      platform.projects[project.ref] = project;
      platform.runs[run.ref] = run;
      platform.gates[gate.ref] = gate;
      const i18n = createI18n({
        legacy: false,
        locale: "ru",
        messages: { ru: applicationI18n.global.getLocaleMessage("ru") },
      });
      const app = createSSRApp(DecisionsPage);
      app.use(pinia);
      app.use(router);
      app.use(i18n);
      const html = await renderToString(app);

      expect(html).toContain("Изменить данные через");
      expect(html).toContain("Тестовый сервис");
      expect(html).toContain("body.value");
      expect(html).toContain("body.comment");
      expect(html).toContain("Технические сведения");
      expect(html).not.toContain("op.internal {&quot;body&quot;");
      expect(html.indexOf("Технические сведения")).toBeLessThan(
        html.indexOf("eff_internal"),
      );
    } finally {
      gate.integrationIntent = undefined;
      gate.contextSummary = originalContextSummary;
    }
  });
});
