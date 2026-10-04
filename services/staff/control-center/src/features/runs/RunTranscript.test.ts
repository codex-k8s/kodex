import { readFileSync } from "node:fs";
import { renderToString } from "@vue/server-renderer";
import { createSSRApp, h } from "vue";
import { describe, expect, it, vi } from "vitest";

import { i18n } from "@/app/i18n";
import RunTranscript from "@/features/runs/RunTranscript.vue";
import {
  executionKey,
  type RunActivityItem,
} from "@/features/runs/run-activity";

vi.mock("@/shared/locale", () => ({ currentLocale: () => "ru" }));

async function render(
  tool: string,
  safeParameters: Record<string, unknown> = {},
  locale: "ru" | "en" = "ru",
  safeResult = "",
): Promise<string> {
  const item: RunActivityItem = {
    id: "tool-example",
    kind: "tool",
    historical: false,
    actor: "Помощник",
    occurredAt: "2026-10-04T10:00:00Z",
    toolCall: Object.assign(
      {
        ref: "call_example",
        tool,
        safeParameters,
        state: "SUCCEEDED" as const,
        revision: 1,
        durationMs: 10,
        safeResult,
        auditRef: "audit_example",
      },
      {
        arguments: "RAW_COMMAND_SENTINEL",
        output: "RAW_OUTPUT_SENTINEL",
        reasoning: "HIDDEN_REASONING_SENTINEL",
      },
    ),
  };
  const previous = i18n.global.locale.value;
  i18n.global.locale.value = locale;
  try {
    const app = createSSRApp({
      render: () => h(RunTranscript, { items: [item], embedded: true }),
    });
    app.use(i18n);
    return await renderToString(app);
  } finally {
    i18n.global.locale.value = previous;
  }
}

function title(html: string): string {
  return (
    /class="run-activity-item__content"[^]*?<header[^>]*>\s*<strong[^>]*>([^]*?)<\/strong>/.exec(
      html,
    )?.[1] ?? ""
  );
}

describe("RunTranscript: названия native инструментов", () => {
  it.each([
    ["CODEX_SHELL", "Работа в терминале", "Terminal action"],
    ["CODEX_FILE_CHANGE", "Изменение файлов", "File changes"],
    ["CODEX_WEB_SEARCH", "Поиск в интернете", "Web search"],
    ["CODEX_DYNAMIC_TOOL", "Вызов инструмента", "Tool call"],
    ["CODEX_IMAGE_VIEW", "Просмотр изображения", "View image"],
    ["CODEX_IMAGE_GENERATION", "Создание изображения", "Generate image"],
    ["CODEX_SLEEP", "Ожидание", "Waiting"],
  ])(
    "локализует %s, сохраняя exact id только в деталях",
    async (tool, ru, en) => {
      for (const [locale, label] of [
        ["ru", ru],
        ["en", en],
      ] as const) {
        const html = await render(tool, {}, locale);
        expect(title(html)).toBe(label);
        expect(html).toMatch(new RegExp(`<code[^>]*>${tool}</code>`));
        expect(html).toMatch(/<details[^>]*><summary[^>]*>[^]*?<code[^>]*>/);
        expect(html).not.toMatch(
          /RAW_COMMAND_SENTINEL|RAW_OUTPUT_SENTINEL|HIDDEN_REASONING_SENTINEL/,
        );
      }
    },
  );

  it("показывает только закрытые безопасные shell actions без повторений", async () => {
    const params = {
      action_kinds: ["SEARCH", "READ", "LIST_FILES", "READ", "UNKNOWN"],
    };
    expect(title(await render("CODEX_SHELL", params))).toBe(
      "Работа в терминале · чтение файлов, список файлов, поиск",
    );
    expect(title(await render("CODEX_SHELL", params, "en"))).toBe(
      "Terminal action · read files, list files, search",
    );
    expect(title(await render("CODEX_SHELL", { action_kinds: "READ" }))).toBe(
      "Работа в терминале",
    );
  });

  it("показывает actual dynamic tool только из safe namespace/tool", async () => {
    expect(
      title(
        await render("CODEX_DYNAMIC_TOOL", {
          namespace: "workspace.tools",
          tool: "inspect",
        }),
      ),
    ).toBe("Вызов инструмента · workspace.tools.inspect");
    expect(
      title(await render("CODEX_DYNAMIC_TOOL", { tool: "inspect" }, "en")),
    ).toBe("Tool call · inspect");
    expect(
      title(
        await render("CODEX_DYNAMIC_TOOL", { namespace: "workspace.tools" }),
      ),
    ).toBe("Вызов инструмента");
    expect(
      title(
        await render("CODEX_DYNAMIC_TOOL", { tool: "inspect\nnot-a-label" }),
      ),
    ).toBe("Вызов инструмента");
  });

  it("не локализует неизвестные ids и обычные MCP names как native", async () => {
    expect(title(await render("project_files.search"))).toBe(
      "project_files.search",
    );
    expect(title(await render("CODEX_FUTURE_TOOL"))).toBe("CODEX_FUTURE_TOOL");
  });

  it.each([
    ["CODEX_SHELL", "lucide-terminal"],
    ["CODEX_FILE_CHANGE", "lucide-file-text"],
    ["CODEX_WEB_SEARCH", "lucide-globe"],
    ["CODEX_IMAGE_VIEW", "lucide-image"],
    ["CODEX_SLEEP", "lucide-clock"],
  ])(
    "показывает отдельный значок и компактные закрытые детали для %s",
    async (tool, icon) => {
      const html = await render(tool);
      expect(html).toContain(icon);
      expect(html).toContain("run-activity-item--compact");
      expect(html).not.toMatch(/<details[^>]*\bopen\b/);
      expect(html.match(/data-state="SUCCEEDED"/g)).toHaveLength(1);
    },
  );
});

describe("RunTranscript: managed инструменты", () => {
  it.each([
    ["get_configuration_catalog", "Каталог настроек", "Configuration catalog"],
    ["propose_configuration_plan", "Настройки помощника", "Assistant settings"],
    ["get_integration_catalog", "Каталог интеграций", "Integration catalog"],
    ["find_platform_resources", "Поиск ресурсов", "Resource search"],
    ["propose_assistant_metadata", "Название диалога", "Conversation title"],
    ["propose_run_metadata", "Описание запуска", "Run description"],
    ["delegate_agent", "Передача задания", "Task delegation"],
    ["invoke_integration", "Вызов интеграции", "Integration call"],
    ["search_files", "Поиск файлов", "File search"],
    ["get_file_metadata", "Сведения о файле", "File information"],
    ["preview_file", "Просмотр файла", "File preview"],
    ["get_file_manifest", "Список файлов", "File list"],
    ["context7_resolve_library_id", "Поиск библиотеки", "Library search"],
    ["context7_query_docs", "Документация библиотеки", "Library documentation"],
  ])(
    "%s имеет понятное exact название и не повторяет completed в preview",
    async (tool, ru, en) => {
      for (const [locale, expected] of [
        ["ru", ru],
        ["en", en],
      ] as const) {
        const html = await render(tool, {}, locale, `${tool}:completed`);
        expect(title(html)).toBe(expected);
        expect(html).not.toContain(
          'class="safe-markdown run-transcript__preview"',
        );
        expect(html).toContain(`${tool}:completed`);
        expect(html).toMatch(new RegExp(`<code[^>]*>${tool}</code>`));
      }
    },
  );
  it("сохраняет содержательный результат и unknown completed", async () => {
    const meaningful = await render(
      "get_configuration_catalog",
      {},
      "ru",
      "Доступны две модели",
    );
    expect(meaningful).toContain("run-transcript__preview");
    expect(meaningful).toContain("Доступны две модели");
    expect(
      await render("custom_lookup", {}, "ru", "custom_lookup:completed"),
    ).toContain("run-transcript__preview");
    expect(title(await render("tool\nunsafe"))).toBe("Вызов инструмента");
  });
});

describe("RunTranscript: безопасный runtime text", () => {
  it.each(["ru", "en"] as const)(
    "локализует raw progress/failure в %s без второго fallback",
    async (locale) => {
      const items: RunActivityItem[] = [
        "WORKLOAD_SCHEDULED",
        "MODEL_REQUEST_RUNNING",
        "RUNTIME_PROVIDER_UNAVAILABLE",
      ].map((summary, index) => ({
        id: `evt_${String(index)}`,
        kind: "system",
        actor: "Помощник",
        summary,
        historical: false,
        occurredAt: "2026-10-04T10:00:00Z",
        messageKind: "STATE",
        state: index === 2 ? "FAILED" : "RUNNING",
        execution: {
          runRef: "run_exact",
          nodeRef: "nod_exact",
          sessionRef: "ses_exact",
          turnRef: "trn_exact",
          turnNumber: 1,
          attempt: 1,
        },
      }));
      const previous = i18n.global.locale.value;
      i18n.global.locale.value = locale;
      try {
        const app = createSSRApp({
          render: () => h(RunTranscript, { items, embedded: true }),
        });
        app.use(i18n);
        const html = await renderToString(app);
        expect(html).not.toMatch(
          /WORKLOAD_SCHEDULED|MODEL_REQUEST_RUNNING|RUNTIME_PROVIDER_UNAVAILABLE|runs\.runtimeProgress|unscopedHistory/,
        );
        expect(html).toContain(
          locale === "ru"
            ? "Подготовка и выполнение запроса к модели"
            : "Preparing and executing the model request",
        );
        expect(html).toContain(
          locale === "ru"
            ? "Провайдер модели временно недоступен"
            : "The model provider is temporarily unavailable",
        );
        expect(html.match(/data-state="FAILED"/g)).toHaveLength(1);
      } finally {
        i18n.global.locale.value = previous;
      }
    },
  );
});

describe("RunTranscript: стороны сообщений", () => {
  const source = readFileSync(
    new URL("./RunTranscript.vue", import.meta.url),
    "utf8",
  );
  const styles = source.slice(source.indexOf("<style scoped>"));
  const rule = (selector: string) =>
    styles.slice(styles.indexOf(`\n${selector} {`) + 1).split("}")[0];

  it("показывает USER справа, COMMENTARY/FINAL, инструменты и статусы слева без изменения порядка", async () => {
    const items: RunActivityItem[] = [
      { id: "owner", kind: "initiator", phase: "USER", summary: "Мой запрос" },
      { id: "comment", kind: "agent", phase: "COMMENTARY", summary: "Работаю" },
      { id: "final", kind: "agent", phase: "FINAL", summary: "Готово" },
      { id: "tool", kind: "tool", summary: "Действие инструмента" },
      { id: "status", kind: "system", summary: "Статус" },
    ].map((item) => ({
      ...item,
      actor: "Автор",
      historical: false,
      occurredAt: "2026-10-04T10:00:00Z",
    })) as RunActivityItem[];
    const app = createSSRApp({
      render: () => h(RunTranscript, { items, embedded: true }),
    });
    app.use(i18n);
    const html = await renderToString(app);
    expect(
      [
        ...html.matchAll(
          /class="run-activity-item run-activity-item--([a-z]+)(?: [^"]*)?"/g,
        ),
      ].map((match) => match[1]),
    ).toEqual(["initiator", "agent", "agent", "tool", "system"]);
    expect(rule(".run-activity-item")).toContain("justify-self: start");
    expect(rule(".run-activity-item--initiator")).toContain(
      "justify-self: end",
    );
    expect(
      rule(".run-activity-item--initiator > .run-activity-item__icon"),
    ).toContain("grid-column: 2");
    expect(
      rule(".run-activity-item--initiator > .run-activity-item__content"),
    ).toContain("grid-column: 1");
  });

  it("ограничивает ширину реплик и групп инструментов и переносит длинный текст на узком экране", () => {
    expect(rule(".run-activity-item")).toContain("width: min(86%, 760px)");
    expect(rule(".run-activity-item")).toContain("min-width: 0");
    expect(rule(".run-activity-item__content")).toContain(
      "overflow-wrap: anywhere",
    );
    expect(styles).toMatch(
      /\.run-transcript__tool-group\s*\{[^}]*width: min\(96%, 1180px\)[^}]*justify-self: start/,
    );
    expect(styles.slice(styles.indexOf("@media (max-width: 720px)"))).toMatch(
      /\.run-activity-item\s*\{\s*width: 94%/,
    );
  });
});

describe("RunTranscript: компактная работа", () => {
  const execution = {
    runRef: "run_exact",
    nodeRef: "nod_exact",
    sessionRef: "ses_exact",
    turnRef: "trn_exact",
    turnNumber: 1,
    attempt: 1,
  };
  function progress(
    id: string,
    changes: Partial<RunActivityItem> = {},
  ): RunActivityItem {
    return {
      id,
      kind: "system",
      actor: "Kodex",
      summary: "MODEL_REQUEST_RUNNING",
      eventType: "TURN_PROGRESS",
      state: "RUNNING",
      historical: false,
      occurredAt: "2026-10-04T10:00:00Z",
      execution: { ...execution },
      sequence: 1,
      ...changes,
    };
  }
  async function transcript(
    items: RunActivityItem[],
    closedExecutionKeys: readonly string[] = [],
  ): Promise<string> {
    const app = createSSRApp({
      render: () =>
        h(RunTranscript, { items, embedded: true, closedExecutionKeys }),
    });
    app.use(i18n);
    return renderToString(app);
  }

  it("показывает один доступный индикатор с тремя точками, этапы закрыты по умолчанию", async () => {
    const html = await transcript([
      progress("start", { eventType: "TURN_STARTED" }),
      progress("schedule", { summary: "WORKLOAD_SCHEDULED" }),
      progress("model"),
    ]);
    expect(html.match(/class="run-activity-item /g)).toHaveLength(1);
    expect(html.match(/role="status"/g)).toHaveLength(1);
    expect(html.match(/<i[^>]*>/g)).toHaveLength(3);
    expect(html).toContain('aria-hidden="true"');
    expect(html).toContain('class="run-transcript__service-history"');
    expect(html).not.toMatch(/<details[^>]*\bopen\b/);
    expect(html).not.toContain('data-state="RUNNING"');
    const header = html.match(/<header[^>]*>([^]*?)<\/header>/)?.[1] ?? "";
    expect(header).not.toMatch(/#1|Ход 1|попытка 1/);
  });

  it("переносит работу на последнюю COMMENTARY, сохраняя полный FINAL и единственную terminal ошибку", async () => {
    const comment = progress("comment", {
      kind: "agent",
      phase: "COMMENTARY",
      summary: "Проверяю " + "длинный текст ".repeat(30),
    });
    const items = [progress("start"), comment];
    const html = await transcript(items);
    expect(html.match(/role="status"/g)).toHaveLength(1);
    expect(html).toMatch(/data-phase="COMMENTARY"[^]*?role="status"/);
    expect(html).toContain("run-activity-item__message--commentary");
    expect(html).toContain('aria-expanded="false"');
    const failed = await transcript([
      ...items,
      progress("failure", {
        eventType: "TURN_COMPLETED",
        messageKind: "FINAL_MESSAGE",
        state: "FAILED",
        summary: "RUNTIME_PROVIDER_UNAVAILABLE",
      }),
      progress("state", {
        eventType: "RUN_STATE_CHANGED",
        state: "FAILED",
        summary: "RUNTIME_PROVIDER_UNAVAILABLE",
      }),
    ]);
    expect(failed).not.toContain('role="status"');
    expect(failed.match(/data-state="FAILED"/g)).toHaveLength(1);
    const complete = await transcript([
      ...items,
      progress("final", {
        kind: "agent",
        phase: "FINAL",
        state: "SUCCEEDED",
        summary: "Итог " + "полный текст ".repeat(30),
      }),
    ]);
    expect(complete).not.toContain('role="status"');
    expect(complete).not.toMatch(
      /data-phase="FINAL"[^]*?run-activity-item__message--collapsed/,
    );
  });

  it("старую историю оставляет отдельной без технического баннера", async () => {
    const html = await transcript([
      progress("old", { execution: undefined, historical: true }),
    ]);
    expect(html).not.toContain('role="status"');
    expect(html).not.toMatch(
      /Служебная история без точной|Эти записи не объединяются|runs\.unscopedHistory/,
    );
    expect(html).toContain("run-transcript__historical");
  });

  it("actual INTERMEDIATE этапы с локализованным summary не возвращают отдельные progress cards", async () => {
    const items = [
      progress("start", {
        eventType: "TURN_STARTED",
        messageKind: "STATE",
        summary: "Выполняется",
      }),
      progress("schedule", {
        messageKind: "INTERMEDIATE_MESSAGE",
        summary: "Запуск передан исполнителю",
        serviceProgressCode: "WORKLOAD_SCHEDULED",
      }),
      progress("model", {
        messageKind: "INTERMEDIATE_MESSAGE",
        summary: "Подготовка и выполнение запроса к модели",
        serviceProgressCode: "MODEL_REQUEST_RUNNING",
      }),
    ];
    const html = await transcript(items);
    expect(html.match(/class="run-activity-item /g)).toHaveLength(1);
    expect(html.match(/role="status"/g)).toHaveLength(1);
    expect(html).not.toContain('class="run-transcript__execution"');
    expect(html).toContain("Этапы выполнения: 3");
  });

  it("closed exact receipt убирает dots до terminal event, не подавляя соседний running ход", async () => {
    const current = progress("current");
    const key = executionKey(current.execution);
    if (!key) throw new Error("Missing synthetic execution key");
    const html = await transcript([current], [key]);
    expect(html).not.toContain('role="status"');
    expect(html).toContain("run-transcript__service-history");
    const other = progress("other", {
      execution: {
        ...execution,
        runRef: "run_parallel",
        turnRef: "trn_parallel",
        turnNumber: 2,
      },
    });
    const parallel = await transcript([current, other], [key]);
    expect(parallel.match(/role="status"/g)).toHaveLength(1);
    expect(parallel).toContain('data-turn-ref="trn_parallel"');
  });

  it("завершённый tool не показывает Working рядом с badge и не зажигает старые service dots", async () => {
    const html = await transcript([
      progress("service"),
      progress("tool", {
        kind: "tool",
        toolCall: {
          ref: "call_exact",
          tool: "get_configuration_catalog",
          safeParameters: {},
          state: "SUCCEEDED",
          revision: 2,
          durationMs: 10,
          safeResult: "get_configuration_catalog:completed",
          auditRef: "audit_exact",
        },
      }),
    ]);
    expect(html).not.toContain('role="status"');
    expect(html.match(/data-state="SUCCEEDED"/g)).toHaveLength(1);
    expect(html).toContain("Каталог настроек");
    expect(html).not.toContain("run-transcript__preview");
  });

  it.each(["ru", "en"] as const)(
    "FAILED FINAL локализует exact machinecode в %s, не переписывая published user/commentary/success",
    async (locale) => {
      const previous = i18n.global.locale.value;
      i18n.global.locale.value = locale;
      try {
        for (const code of [
          "PROVIDER_RESULT_UNVERIFIABLE",
          "PROVIDER_RESULT_UNKNOWN",
          "PROVIDER_AUTHENTICATION_REQUIRED",
          "PROVIDER_USAGE_LIMIT_EXCEEDED",
          "PROVIDER_OVERLOADED",
          "PROVIDER_POLICY_DENIED",
          "RUNTIME_CONFIGURATION_STALE",
          "RUNTIME_PROVIDER_UNAVAILABLE",
        ]) {
          const html = await transcript([
            progress("failed", {
              kind: "agent",
              phase: "FINAL",
              state: "FAILED",
              summary: `i18n:${code}`,
            }),
          ]);
          expect(html).toContain(i18n.global.t(`serverMessages.${code}`));
          expect(html).not.toContain(code);
          expect(html).not.toContain('role="status"');
        }
        const code = "PROVIDER_RESULT_UNVERIFIABLE";
        for (const item of [
          progress("user", { kind: "initiator", phase: "USER", summary: code }),
          progress("comment", {
            kind: "agent",
            phase: "COMMENTARY",
            summary: code,
          }),
          progress("success", {
            kind: "agent",
            phase: "FINAL",
            state: "SUCCEEDED",
            summary: code,
          }),
          progress("prose", {
            kind: "agent",
            phase: "FINAL",
            state: "FAILED",
            summary: `Модель объяснила код ${code}`,
          }),
        ]) {
          expect(await transcript([item])).toContain(item.summary);
        }
      } finally {
        i18n.global.locale.value = previous;
      }
    },
  );

  it("пустой completed progress не создаёт отдельную строку: этапы остаются details полного FINAL", async () => {
    const html = await transcript([
      progress("start"),
      progress("model"),
      progress("final", {
        kind: "agent",
        phase: "FINAL",
        state: "SUCCEEDED",
        summary: "Готово\n\nПолный результат",
      }),
    ]);
    expect(html.match(/class="run-activity-item /g)).toHaveLength(1);
    expect(html).toContain('data-phase="FINAL"');
    expect(html).toContain("Полный результат");
    expect(html).toContain("Этапы выполнения: 2");
    expect(html).not.toContain("run-activity-item--compact");
    expect(html).not.toContain("run-activity-item--service");
    expect(html).not.toMatch(/<details[^>]*\bopen\b/);
    expect(html).not.toContain('role="status"');
  });
  it.each(["ru", "en"] as const)(
    "actual no-message SYSTEM TURN_COMPLETED failed summary локализуется в %s",
    async (locale) => {
      const previous = i18n.global.locale.value;
      i18n.global.locale.value = locale;
      try {
        const html = await transcript([
          progress("failed", {
            eventType: "TURN_COMPLETED",
            messageKind: "FINAL_MESSAGE",
            state: "FAILED",
            summary: "PROVIDER_RESULT_UNVERIFIABLE",
          }),
        ]);
        expect(html).toContain(
          i18n.global.t("serverMessages.PROVIDER_RESULT_UNVERIFIABLE"),
        );
        expect(html).not.toContain("PROVIDER_RESULT_UNVERIFIABLE");
        expect(html.match(/data-state="FAILED"/g)).toHaveLength(1);
        expect(html).not.toContain('role="status"');
      } finally {
        i18n.global.locale.value = previous;
      }
    },
  );

  it("сохраняет анимацию и отключает её при reduced motion, tool preview ограничен", () => {
    const source = readFileSync(
      new URL("./RunTranscript.vue", import.meta.url),
      "utf8",
    );
    expect(source).toContain("@keyframes transcript-working");
    expect(source).toMatch(
      /@media \(prefers-reduced-motion: reduce\)[^]*?animation: none/,
    );
    expect(source).toMatch(
      /\.run-transcript__preview\s*\{[^}]*-webkit-line-clamp: 2/,
    );
    expect(source).toMatch(
      /\.run-activity-item--compact > article\s*\{[^}]*padding: 6px 10px/,
    );
  });
});
