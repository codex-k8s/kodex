import { renderToString } from "@vue/server-renderer";
import { compileStyle } from "@vue/compiler-sfc";
import {
  createRenderer,
  createSSRApp,
  defineComponent,
  h,
  nextTick,
  reactive,
  ssrContextKey,
  type ComputedRef,
  type Ref,
  type SetupContext,
} from "vue";
import { createI18n } from "vue-i18n";
import { describe, expect, it, vi } from "vitest";

import RunSessionDetailsDialog from "@/features/runs/RunSessionDetailsDialog.vue";
import dialogSource from "@/features/runs/RunSessionDetailsDialog.vue?raw";
import RunTranscript from "@/features/runs/RunTranscript.vue";
import type {
  PresentedRunEvent,
  RunActivityItem,
} from "@/features/runs/run-activity";
import type {
  Artifact,
  Run,
  RunNode,
} from "@/shared/api/generated/openapi/types.gen";

const execution = {
  runRef: "run_example",
  nodeRef: "nod_agent",
  sessionRef: "ses_example",
  turnRef: "trn_example",
  turnNumber: 1,
  attempt: 1,
};

async function renderActivity(
  events: PresentedRunEvent[],
  currentRun: Run = run,
  currentNode: RunNode = node,
  additionalNodes: RunNode[] = [],
  rootRun?: Run,
): Promise<string> {
  const app = createSSRApp({
    render: () =>
      h(RunSessionDetailsDialog, {
        run: currentRun,
        rootRun,
        node: currentNode,
        nodes: [currentNode, toolNode, ...additionalNodes],
        events,
        artifacts: [],
      }),
  });
  app.use(
    createI18n({
      legacy: false,
      locale: "ru",
      missingWarn: false,
      fallbackWarn: false,
      messages: { ru: {} },
    }),
  );
  return renderToString(app);
}

const run: Run = {
  ref: "run_example",
  version: 1,
  projectRef: "prj_example",
  sessionRef: "ses_example",
  rootRunRef: "run_example",
  target: {
    type: "AGENT",
    ref: "agt_example",
    displayName: "Аналитик",
    version: 1,
  },
  title: "Подготовка отчёта",
  titleSource: "USER_EDITED",
  activitySummary: "Сбор подтверждённых фактов",
  inputSummary: "Проверь отчёт",
  state: "RUNNING",
  source: "CONTROL_CENTER",
  initiator: { ref: "usr_example", displayName: "Владелец" },
  attempt: 1,
  graphRevision: 1,
  lastEventSequence: 1,
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
  gateRefs: [],
  createdAt: "2026-08-29T08:00:00Z",
  nextActions: [],
};

const node: RunNode = {
  ref: "nod_agent",
  runRef: run.ref,
  type: "AGENT_EXECUTION",
  state: "RUNNING",
  displayName: "Сессия аналитика",
  role: "Аналитик продаж",
  attempt: 1,
  inputSummary: "Проверь квартальный отчёт",
  progressSummary: "Собираю факты",
  integrationNames: ["Файлы Проекта"],
  artifactRefs: [],
  childRunRefs: [],
  createdAt: "2026-08-29T08:00:01Z",
  startedAt: "2026-08-29T08:00:02Z",
  nextActions: [],
};
const toolNode: RunNode = {
  ...node,
  ref: "nod_tool",
  parentNodeRef: node.ref,
  type: "EXTERNAL_ACTION",
  displayName: "Поиск по файлам",
  artifactRefs: ["art_report"],
};

const artifact: Artifact = {
  ref: "art_report",
  currentRevisionRef: "arv_fixture_report",
  version: 1,
  projectRef: run.projectRef,
  runRef: run.ref,
  fileName: "report.md",
  mediaType: "text/markdown",
  sizeBytes: 2048,
  digest: "sha256:example",
  scanState: "CLEAN",
  source: "AGENT_RESULT",
  revision: 1,
  lifecycleState: "ACTIVE",
  agentBindings: [],
  previewAvailable: true,
  createdAt: "2026-08-29T08:00:05Z",
  nextActions: ["DOWNLOAD"],
};

const event: PresentedRunEvent = {
  ref: "evt_progress",
  runRef: run.ref,
  sequence: 1,
  type: "TURN_PROGRESS",
  nodeRef: node.ref,
  summary: "ignored",
  displaySummary: "Собираю подтверждённые факты",
  actor: { kind: "AGENT", ref: "agt_example", name: "Аналитик продаж" },
  occurredAt: "2026-08-29T08:00:03Z",
  graphRevision: 1,
  run: {
    ref: run.ref,
    version: 1,
    state: "RUNNING",
    graphRevision: 1,
    lastEventSequence: 1,
    usage: run.usage,
    artifactRefs: [],
    gateRefs: [],
    nextActions: [],
  },
};

const userEvent: PresentedRunEvent = {
  ...event,
  ref: "evt_user",
  sequence: 2,
  type: "TURN_PROGRESS",
  summary: "Добавь сравнение с прошлым кварталом",
  displaySummary: "Добавь сравнение с прошлым кварталом",
  actor: { kind: "USER", ref: "usr_example", name: "Владелец" },
  occurredAt: "2026-08-29T08:00:04Z",
};

const toolCall: NonNullable<PresentedRunEvent["toolCall"]> = {
  ref: "tool_example",
  tool: "project_files.search",
  safeParameters: { query: "квартальный отчёт" },
  state: "SUCCEEDED",
  durationMs: 180,
  safeResult: "Найдено 4 подтверждённых фрагмента",
  auditRef: "audit_example",
};
const toolEvent: PresentedRunEvent = {
  ...event,
  ref: "evt_tool",
  sequence: 3,
  type: "TOOL_CALL_RECORDED",
  nodeRef: toolNode.ref,
  summary: "Проверен источник",
  displaySummary: "Проверен источник",
  messageKind: "TOOL_CALL",
  toolCall,
  occurredAt: "2026-08-29T08:00:05Z",
};

describe("RunSessionDetailsDialog", () => {
  it("читает child transcript из root envelope только по точной owner graph привязке", async () => {
    const childRun: Run = { ...run, ref: "run_child", rootRunRef: run.ref };
    const childNode: RunNode = {
      ...node,
      ref: "nod_child",
      runRef: childRun.ref,
      turnRef: execution.turnRef,
    };
    const parentNode = { ...node, childRunRefs: [childRun.ref] };
    const childEvent: PresentedRunEvent = {
      ...event,
      nodeRef: childNode.ref,
      execution: { ...execution, runRef: childRun.ref, nodeRef: childNode.ref },
      messageKind: "INTERMEDIATE_MESSAGE",
      message: {
        ref: "msg_child",
        revision: 1,
        source: { origin: "ORDINARY" },
        phase: "COMMENTARY",
        text: "CHILD_COMMENTARY_SENTINEL",
      },
    };
    const render = (value: PresentedRunEvent, nodes = [parentNode]) =>
      renderActivity([value], childRun, childNode, nodes, run);
    expect(await render(childEvent)).toContain("CHILD_COMMENTARY_SENTINEL");
    const childHistory = await renderActivity(
      [
        childEvent,
        {
          ...childEvent,
          ref: "evt_child_tool",
          sequence: 2,
          message: undefined,
          messageKind: "TOOL_CALL",
          toolCall: { ...toolCall, revision: 1 },
        },
        {
          ...childEvent,
          ref: "evt_child_final",
          sequence: 3,
          messageKind: "FINAL_MESSAGE",
          message: {
            ref: "msg_child_final",
            revision: 1,
            source: { origin: "ORDINARY" },
            phase: "FINAL",
            text: "CHILD_FINAL_SENTINEL",
          },
        },
      ],
      childRun,
      childNode,
      [parentNode],
      run,
    );
    expect(childHistory).toContain("CHILD_FINAL_SENTINEL");
    expect(childHistory).toContain("project_files.search");
    for (const value of [
      { ...childEvent, runRef: "run_foreign" },
      { ...childEvent, nodeRef: "nod_foreign" },
      { ...childEvent, run: { ...childEvent.run, ref: "run_foreign" } },
      { ...childEvent, execution: undefined },
      ...(
        ["runRef", "nodeRef", "sessionRef", "turnRef", "attempt"] as const
      ).map((field) => ({
        ...childEvent,
        execution: {
          ...childEvent.execution,
          ...execution,
          runRef: childRun.ref,
          nodeRef: childNode.ref,
          [field]: field === "attempt" ? 2 : "foreign_pin",
        },
      })),
    ])
      expect(await render(value)).not.toContain("CHILD_COMMENTARY_SENTINEL");
    expect(await render(childEvent, [])).not.toContain(
      "CHILD_COMMENTARY_SENTINEL",
    );
    expect(await render(childEvent, [parentNode, childNode])).not.toContain(
      "CHILD_COMMENTARY_SENTINEL",
    );
    for (const value of [
      { ...childRun, rootRunRef: "run_foreign" },
      { ...childRun, projectRef: "prj_foreign" },
      { ...childRun, sessionRef: "ses_foreign" },
    ])
      expect(
        await renderActivity([childEvent], value, childNode, [parentNode], run),
      ).not.toContain("CHILD_COMMENTARY_SENTINEL");
    expect(dialogSource).toContain(':run="run"');
    expect(dialogSource).toContain(
      'v-if="sessionNode && node.runRef === run.ref"',
    );
  });

  it("показывает только размер контекстного окна без нулевых строк расхода", async () => {
    const html = await renderActivity([], {
      ...run,
      usage: { ...run.usage, modelContextWindow: 258400 },
    });
    expect(html).toContain("runs.usage.contextWindow");
    expect(html).toContain(new Intl.NumberFormat("ru").format(258400));
    for (const key of ["total", "input", "cached", "output", "reasoning"]) {
      expect(html).not.toContain(`runs.usage.${key}`);
    }
  });

  it("сохраняет все строки измеренного расхода и скрывает полностью пустую сводку", async () => {
    const empty = await renderActivity([]);
    expect(empty).not.toContain("runs.usage.title");

    const measured = await renderActivity([], {
      ...run,
      usage: {
        ...run.usage,
        totalTokens: 100,
        inputTokens: 80,
        cachedInputTokens: 20,
        outputTokens: 20,
        reasoningOutputTokens: 5,
        modelContextWindow: 258400,
      },
    });
    for (const key of [
      "total",
      "input",
      "cached",
      "output",
      "reasoning",
      "contextWindow",
    ]) {
      expect(measured).toContain(`runs.usage.${key}`);
    }
  });

  it("использует canonical компактный transcript с actual USER/FINAL и одной revision инструмента", async () => {
    const service = (
      ref: string,
      sequence: number,
      type: PresentedRunEvent["type"],
      state: PresentedRunEvent["nodeState"],
    ): PresentedRunEvent => ({
      ...event,
      ref,
      sequence,
      type,
      execution,
      nodeState: state,
      messageKind: "STATE",
      summary: type,
      displaySummary: type,
    });
    const user: PresentedRunEvent = {
      ...userEvent,
      execution,
      sequence: 4,
      messageKind: "USER_MESSAGE",
      message: {
        source: { origin: "ORDINARY" as const },
        ref: "msg_user",
        revision: 1,
        phase: "USER",
        text: "ACTUAL_USER_INPUT_SENTINEL",
      },
    };
    const tool: PresentedRunEvent = {
      ...toolEvent,
      nodeRef: node.ref,
      execution,
      sequence: 5,
      toolCall: {
        ...toolCall,
        revision: 1,
        state: "RUNNING",
        safeResult: "OLD_RUNNING_SENTINEL",
      },
    };
    const terminalTool: PresentedRunEvent = {
      ...tool,
      ref: "evt_tool_terminal",
      sequence: 6,
      toolCall: {
        ...toolCall,
        revision: 2,
        state: "SUCCEEDED",
        safeResult: "EXACT_TOOL_RESULT_SENTINEL",
      },
    };
    const final: PresentedRunEvent = {
      ...event,
      ref: "evt_final",
      sequence: 7,
      execution,
      messageKind: "FINAL_MESSAGE",
      nodeState: "SUCCEEDED",
      message: {
        source: { origin: "ORDINARY" as const },
        ref: "msg_final",
        revision: 1,
        phase: "FINAL",
        text: "ACTUAL_FINAL_RESULT_SENTINEL",
      },
    };
    const html = await renderActivity(
      [
        service("evt_created", 1, "RUN_CREATED", "QUEUED"),
        service("evt_queued", 2, "TURN_QUEUED", "QUEUED"),
        service("evt_started", 3, "TURN_STARTED", "RUNNING"),
        user,
        tool,
        terminalTool,
        final,
        service("evt_completed", 8, "TURN_COMPLETED", "SUCCEEDED"),
      ],
      { ...run, resultSummary: "ACTUAL_FINAL_RESULT_SENTINEL" },
    );
    expect(html).not.toContain("run-transcript--embedded");
    expect(html).toContain('role="log" aria-live="polite"');
    expect(html).toContain("run-activity-item--initiator");
    expect(html).toContain("run-activity-item--agent");
    expect(html.match(/ACTUAL_USER_INPUT_SENTINEL/g)).toHaveLength(1);
    expect(html.match(/ACTUAL_FINAL_RESULT_SENTINEL/g)).toHaveLength(1);
    expect(html).toContain("EXACT_TOOL_RESULT_SENTINEL");
    expect(html.match(/run-activity-item--tool/g)).toHaveLength(1);
    expect(html).not.toContain("OLD_RUNNING_SENTINEL");
    expect(html).not.toContain("session-details__event--");
    const visible = html.replace(/<details\b[^>]*>[^]*?<\/details>/g, "");
    expect(visible).not.toMatch(
      /RUN_CREATED|TURN_QUEUED|TURN_STARTED|TURN_COMPLETED/,
    );
    expect(html).toContain("run-transcript__service-history");
  });

  it("открывает последние события, следует новым и сохраняет ручную прокрутку до нажатия кнопки", async () => {
    const log = {
      scrollHeight: 1200,
      clientHeight: 300,
      scrollTop: 0,
      scrollTo: vi.fn(({ top }: { top: number }) => {
        log.scrollTop = Math.min(top, log.scrollHeight - log.clientHeight);
      }),
    };
    const renderer = createRenderer<object, object>({
      createElement: () => ({}),
      createText: () => ({}),
      createComment: () => ({}),
      setText() {},
      setElementText() {},
      patchProp() {},
      insert() {},
      remove() {},
      parentNode: () => null,
      nextSibling: () => null,
    });
    const message = (sequence: number): PresentedRunEvent => ({
      ...event,
      ref: `evt_message_${String(sequence)}`,
      sequence,
      execution,
      messageKind: "INTERMEDIATE_MESSAGE",
      message: {
        source: { origin: "ORDINARY" },
        ref: `msg_${String(sequence)}`,
        revision: 1,
        phase: "COMMENTARY",
        text: `Подтверждённое сообщение ${String(sequence)}`,
      },
    });
    const props = reactive({
      run,
      node,
      nodes: [node, toolNode],
      events: [message(1)],
      artifacts: [],
    });
    type TranscriptState = {
      log: Ref<HTMLElement | undefined>;
      unread: Ref<boolean>;
      latest(): void;
      onScroll(): void;
    };
    let state!: TranscriptState;
    const dialog = RunSessionDetailsDialog as unknown as {
      setup(
        props: object,
        context: SetupContext,
      ): {
        transcriptItems: ComputedRef<RunActivityItem[]>;
      };
    };
    const transcript = RunTranscript as unknown as {
      setup(props: object, context: SetupContext): TranscriptState;
    };
    const app = renderer.createApp(
      defineComponent({
        setup(_props, context) {
          const activity = dialog.setup(props, context);
          state = transcript.setup(
            {
              get items() {
                return activity.transcriptItems.value;
              },
              embedded: false,
              groupTools: true,
              closedExecutionKeys: [],
            },
            context,
          );
          state.log.value = log as unknown as HTMLElement;
          return () => null;
        },
      }),
    );
    app.use(
      createI18n({
        legacy: false,
        locale: "ru",
        missingWarn: false,
        fallbackWarn: false,
        messages: { ru: {} },
      }),
    );
    app.provide(ssrContextKey, {});
    try {
      app.mount({});
      await nextTick();
      expect(log.scrollTop).toBe(900);
      log.scrollHeight = 1400;
      props.events.push(message(2));
      await nextTick();
      await nextTick();
      expect(log.scrollTop).toBe(1100);

      log.scrollTop = 120;
      state.onScroll();
      log.scrollHeight = 1600;
      props.events.push(message(3));
      await nextTick();
      await nextTick();
      expect(log.scrollTop).toBe(120);
      expect(state.unread.value).toBe(true);
      state.latest();
      await nextTick();
      expect(log.scrollTop).toBe(1300);
      expect(state.unread.value).toBe(false);

      log.scrollHeight = 1800;
      props.events.push(message(4));
      await nextTick();
      await nextTick();
      expect(log.scrollTop).toBe(1500);
    } finally {
      app.unmount();
    }
  });

  it("ограничивает dialog и оставляет единственный scroll переписки на desktop и mobile", () => {
    expect(dialogSource).toMatch(
      /\.session-details-dialog > :deep\(\.modal > \.modal__body\) \{[^}]*overflow: hidden;/,
    );
    expect(dialogSource).toMatch(
      /\.session-details__activity \{[^}]*grid-template-rows: auto minmax\(0, 1fr\);[^}]*overflow: hidden;/,
    );
    expect(dialogSource).toContain(
      "grid-template-rows: minmax(0, 0.18fr) minmax(0, 0.82fr)",
    );
    expect(dialogSource).not.toContain("overflow: visible");
  });

  it("не передаёт высоту и горизонтальный flex внешнего dialog во вложенный preview", () => {
    const css = dialogSource.match(/<style scoped>([\s\S]*?)<\/style>/)?.[1];
    expect(css).toBeDefined();
    const compiled = compileStyle({
      source: css ?? "",
      filename: "RunSessionDetailsDialog.vue",
      id: "data-v-session-layout",
      scoped: true,
    });
    expect(compiled.errors).toEqual([]);
    const selectors = compiled.code.replace(/\s+/g, " ");
    expect(selectors).toContain(
      ".session-details-dialog[data-v-session-layout] > .modal {",
    );
    expect(selectors).toContain(
      ".session-details-dialog[data-v-session-layout] > .modal > .modal__body {",
    );
    expect(selectors).not.toMatch(
      /\.session-details-dialog\[data-v-session-layout\] \.modal(?:__body)? \{/,
    );
  });

  it("ограничивает краткую роль двумя строками и сохраняет компактную mobile сводку", () => {
    expect(dialogSource).toMatch(
      /\.session-details__summary strong \{[^}]*-webkit-line-clamp: 2;[^}]*overflow: hidden;[^}]*overflow-wrap: anywhere;/,
    );
    const mobile = dialogSource.split("@media (max-width: 760px)")[1];
    expect(mobile).toMatch(
      /\.session-details__summary p \{[^}]*-webkit-line-clamp: 2;/,
    );
    expect(mobile).toMatch(
      /\.session-details__summary \{[^}]*gap: 8px;[^}]*padding: 10px;/,
    );
  });

  it("сохраняет несвязанные диагностики/ошибки и не придумывает USER из inputSummary", async () => {
    const html = await renderActivity([
      {
        ...event,
        ref: "evt_unbound",
        nodeRef: undefined,
        displaySummary: "Несвязанная диагностика sentinel",
      },
      {
        ...event,
        ref: "evt_unknown_node",
        nodeRef: "nod_unknown",
        sequence: 2,
        displaySummary: "Диагностика неизвестного узла sentinel",
      },
      {
        ...event,
        ref: "evt_failure",
        execution,
        sequence: 3,
        type: "TURN_COMPLETED",
        messageKind: "STATE",
        nodeState: "FAILED",
        displaySummary: "Проверка источника завершилась ошибкой sentinel",
      },
      {
        ...event,
        ref: "evt_foreign_run",
        runRef: "run_foreign",
        sequence: 4,
        displaySummary: "FOREIGN_RUN_SENTINEL",
      },
    ]);
    expect(html).toContain("Несвязанная диагностика sentinel");
    expect(html).toContain("Диагностика неизвестного узла sentinel");
    expect(html).toContain("Проверка источника завершилась ошибкой sentinel");
    expect(html).not.toContain("FOREIGN_RUN_SENTINEL");
    expect(html).not.toContain("run-activity-item--initiator");
  });

  it("не скрывает иной авторитетный результат RUN при наличии FINAL выбранной сессии", async () => {
    const html = await renderActivity(
      [
        {
          ...event,
          execution,
          messageKind: "FINAL_MESSAGE",
          message: {
            source: { origin: "ORDINARY" as const },
            ref: "msg_final",
            revision: 1,
            phase: "FINAL",
            text: "Финальный ответ выбранной сессии",
          },
        },
      ],
      { ...run, resultSummary: "Иной авторитетный результат RUN" },
    );
    expect(html).toContain("Финальный ответ выбранной сессии");
    expect(html).toContain("Иной авторитетный результат RUN");
  });

  it("ограничивает сводку шапки тремя строками на desktop, не усекая полный результат", () => {
    const headerStyle = dialogSource.match(
      /\.session-details__summary p \{([^}]+)\}/,
    )?.[1];
    expect(headerStyle).toContain("-webkit-line-clamp: 3");
    expect(headerStyle).toContain("-webkit-box-orient: vertical");
    expect(headerStyle).toContain("overflow: hidden");
    expect(headerStyle).toContain("min-width: 0");
    for (const selector of [
      "session-details__input",
      "session-details__long-value",
    ]) {
      const fullTextStyle = dialogSource.match(
        new RegExp(`\\.${selector} \\{([^}]+)\\}`),
      )?.[1];
      expect(fullTextStyle).not.toContain("-webkit-line-clamp");
    }
    expect(dialogSource).toContain("node.progressSummary ||");
    expect(dialogSource).toContain("<SafeMarkdown");
  });

  it("ставит длинные входные данные на всю ширину и сохраняет полный текст за раскрытием", async () => {
    const input = "Подготовь полный проверяемый отчёт для команды. ".repeat(80);
    const html = await renderActivity([], run, {
      ...node,
      inputSummary: input,
    });
    expect(html).toContain("session-details__long-field");
    expect(html).toContain(input.trim());
    expect(html).toContain('id="run-session-input"');
    expect(html).toMatch(
      /class="button button--ghost session-details__input-toggle"[^>]*aria-expanded="false"[^>]*aria-controls="run-session-input"/,
    );
    expect(html).not.toContain("session-details__input--expanded");
  });

  it("сохраняет компактные метаданные, ограниченный preview и кнопку контекста32px", () => {
    expect(dialogSource).toMatch(
      /\.session-details dl > \.session-details__long-field \{[^}]*grid-template-columns: minmax\(0, 1fr\)/,
    );
    expect(dialogSource).toContain(
      "grid-template-columns: minmax(120px, 0.42fr) minmax(0, 1fr)",
    );
    expect(dialogSource).toMatch(
      /\.session-details__input \{[^}]*max-height: 150px;[^}]*overflow: auto;/,
    );
    expect(dialogSource).toMatch(
      /\.session-details__input--expanded \{[^}]*max-height: 320px;[^}]*overflow: auto;/,
    );
    expect(dialogSource).toMatch(
      /\.session-details :deep\(\.run-prompt-preview > \.button\) \{[^}]*height: 32px;[^}]*white-space: nowrap;/,
    );
    expect(dialogSource).toContain(":title=\"$t('promptContext.preview')\"");
  });

  it("показывает полные описание роли и источник в широких доступных прокручиваемых рядах", async () => {
    const role =
      "Менеджер отвечает за постановку задач и проверяемый результат. "
        .repeat(40)
        .trim();
    const source =
      "Проверка полного процесса разработки с подтверждением владельца. "
        .repeat(20)
        .trim();
    const parent: RunNode = {
      ...node,
      ref: "nod_parent",
      type: "ROOT_PROCESS",
      displayName: source,
    };
    const html = await renderActivity(
      [],
      run,
      { ...node, role, parentNodeRef: parent.ref },
      [parent],
    );

    expect(html).toContain(role);
    expect(html).toContain(source);
    expect(html.match(/class="session-details__long-value"/g)).toHaveLength(2);
    expect(html).toMatch(
      /class="session-details__long-value"[^>]*tabindex="0"[^>]*role="region"/,
    );
    expect(dialogSource).toMatch(
      /\.session-details__long-value \{[^}]*max-height: 150px;[^}]*overflow: auto;/,
    );
    expect(dialogSource).toContain("session-details__long-value:focus-visible");
  });

  it("показывает доступные launch данные и честные runtime/prompt states", async () => {
    const app = createSSRApp({
      render: () =>
        h(RunSessionDetailsDialog, {
          run,
          node,
          nodes: [node, toolNode],
          events: [
            {
              ...toolEvent,
              execution,
              toolCall: { ...toolCall, revision: 1 },
            },
            {
              ...userEvent,
              execution,
              messageKind: "USER_MESSAGE",
              message: {
                source: { origin: "ORDINARY" as const },
                ref: "msg_user",
                revision: 1,
                phase: "USER",
                text: userEvent.displaySummary,
              },
            },
            {
              ...event,
              execution,
              messageKind: "INTERMEDIATE_MESSAGE",
              message: {
                source: { origin: "ORDINARY" as const },
                ref: "msg_commentary",
                revision: 1,
                phase: "COMMENTARY",
                text: event.displaySummary,
              },
            },
          ],
          artifacts: [artifact],
        }),
    });
    app.use(
      createI18n({
        legacy: false,
        locale: "ru",
        messages: {
          ru: {
            common: {
              close: "Закрыть",
              status: "Состояние",
              source: "Источник",
              input: "Входные данные",
              empty: "Пусто",
              noData: "Нет данных",
              unavailable: "Функция временно недоступна",
            },
            agents: {
              profile: "Профиль сотрудника",
              runtime: "Модель выполнения",
              instructions: "Инструкции",
              integrations: "Разрешённые интеграции",
              role: "Роль",
            },
            runs: {
              launchSummary: "Что будет запущено",
              waitingForActivity: "Ожидает начала работы",
              attempt: "Попытка {attempt}",
              startedAt: "Начало",
              finishedAt: "Завершение",
              nodeConversation: "Работа ИИ-сотрудника",
              noNodeActivity: "Сообщений пока нет",
              toolParameters: "Безопасные параметры",
              toolResult: "Безопасный результат",
              toolDuration: "Длительность: {duration} мс",
              sessionNode: "Сессия",
              controlNode: "Контрольный этап",
              runContext: "Контекст запуска",
              artifacts: "Результаты и файлы",
              promptPreviewSafeHint:
                "Безопасный состав и версии, не полный ввод модели.",
              source: { CONTROL_CENTER: "Control Center" },
              nodeTypes: { AGENT_EXECUTION: "ИИ-сотрудник" },
            },
            promptContext: { preview: "Просмотреть контекст исполнения" },
            states: {
              RUNNING: "Выполняется",
              SUCCEEDED: "Завершено",
              CLEAN: "Проверен",
            },
          },
        },
      }),
    );

    const html = await renderToString(app);

    expect(html).toContain("Сессия аналитика");
    expect(html).toContain("Проверь квартальный отчёт");
    expect(html).toContain("Control Center");
    expect(html).toContain("Файлы Проекта");
    expect(html).toContain("Модель выполнения");
    expect(html).toContain("Инструкции");
    expect(html).toContain(
      "Безопасный состав и версии, не полный ввод модели.",
    );
    expect(html).toContain("Просмотреть контекст исполнения");
    expect(html).not.toContain('type="checkbox"');
    expect(html).toContain("Собираю подтверждённые факты");
    expect(html).toContain("session-details__workspace");
    expect(html).toContain("run-activity-item--agent");
    expect(html).toContain("run-activity-item--initiator");
    expect(html).toContain("run-activity-item--tool");
    expect(html).toContain("project_files.search");
    expect(html).toContain("Найдено 4 подтверждённых фрагмента");
    expect(html).toContain("180 мс");
    expect(html).toContain("report.md");
    expect(html.indexOf("Собираю подтверждённые факты")).toBeLessThan(
      html.indexOf("Добавь сравнение с прошлым кварталом"),
    );
    expect(html.indexOf("Добавь сравнение с прошлым кварталом")).toBeLessThan(
      html.indexOf("Найдено 4 подтверждённых фрагмента"),
    );
    expect(html).not.toContain("ses_example");
  });
});
