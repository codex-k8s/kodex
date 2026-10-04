import { readFileSync } from "node:fs";
import { renderToString } from "@vue/server-renderer";
import { createSSRApp, h } from "vue";
import { describe, expect, it, vi } from "vitest";

import { i18n } from "@/app/i18n";
import RunTranscript from "@/features/runs/RunTranscript.vue";
import type { RunActivityItem } from "@/features/runs/run-activity";

vi.mock("@/shared/locale", () => ({ currentLocale: () => "ru" }));

async function render(
  tool: string,
  safeParameters: Record<string, unknown> = {},
  locale: "ru" | "en" = "ru",
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
        safeResult: "",
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
    /class="run-tool-event"[^]*?<header[^>]*>\s*<strong[^>]*>([^]*?)<\/strong>/.exec(
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
          /class="run-activity-item run-activity-item--([a-z]+)"/g,
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
