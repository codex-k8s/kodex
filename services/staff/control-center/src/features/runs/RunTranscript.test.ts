import { readFileSync } from "node:fs";
import { renderToString } from "@vue/server-renderer";
import { createSSRApp, h } from "vue";
import { describe, expect, it, vi } from "vitest";

import { i18n } from "@/app/i18n";
import RunTranscript from "@/features/runs/RunTranscript.vue";
import type { Artifact } from "@/shared/api/generated/openapi/types.gen";
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
  state: NonNullable<RunActivityItem["toolCall"]>["state"] = "SUCCEEDED",
  working = false,
  capabilityRef?: string,
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
        capabilityRef,
        state,
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
      render: () =>
        h(RunTranscript, {
          items: [item],
          embedded: true,
          activeItemId: working ? item.id : undefined,
        }),
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

describe("RunTranscript: результат интеграции, а не успех обёртки", () => {
  it("оставляет malformed результат видимым, не переопределяя статус обёртки", async () => {
    const malformed = JSON.stringify({
      version: 1,
      invocationRef: "inv_fixture123",
      state: "FAILED",
      inputSHA256: "a".repeat(64),
      extra: "Данные",
    });
    const html = await render("invoke_integration", {}, "ru", malformed);
    const visibleHeader = (
      html.match(/<header[^>]*>[^]*?<\/header>/)?.[0] ?? ""
    ).split("<details")[0];
    expect(visibleHeader).toContain('data-state="SUCCEEDED"');
    expect(html).toContain("run-transcript__preview");
  });
  it("показывает ошибку вложенной интеграции в свёрнутой группе инструментов", async () => {
    const items: RunActivityItem[] = [
      "SUCCEEDED",
      "FAILED",
      "SUCCEEDED",
      "SUCCEEDED",
    ].map((state, index) => ({
      id: `tool_${String(index)}`,
      kind: "tool",
      historical: false,
      actor: "Сотрудник",
      occurredAt: "2026-10-04T10:00:00Z",
      execution: {
        runRef: "run_fixture",
        nodeRef: "nod_fixture",
        sessionRef: "ses_fixture",
        turnRef: "trn_fixture",
        turnNumber: 1,
        attempt: 1,
      },
      toolCall: {
        ref: `tcl_${String(index)}`,
        tool: "invoke_integration",
        state: "SUCCEEDED",
        durationMs: 10,
        safeParameters: {},
        auditRef: "aud_fixture",
        safeResult: JSON.stringify({
          version: 1,
          invocationRef: `inv_fixture000${String(index)}`,
          state,
          inputSHA256: "a".repeat(64),
        }),
      },
    }));
    const html = await renderToString(
      createSSRApp({
        render: () =>
          h(RunTranscript, { items, embedded: true, activeItemId: null }),
      }).use(i18n),
    );
    const summary =
      html.match(
        /class="run-transcript__tool-group"[^]*?<summary[^>]*>[^]*?<\/summary>/,
      )?.[0] ?? "";
    expect(summary).toContain('data-state="FAILED"');
    expect(summary).toContain("status-badge--danger");
    expect(summary).not.toContain('data-state="SUCCEEDED"');
    expect(items.every((item) => item.toolCall?.state === "SUCCEEDED")).toBe(
      true,
    );
  });
  it.each(["ru", "en"] as const)(
    "показывает FAILED/REJECTED exact квитанции, не скрывая детали (%s)",
    async (locale) => {
      for (const tool of [
        "invoke_integration",
        "context7_resolve_library_id",
        "context7_query_docs",
      ]) {
        for (const state of ["FAILED", "REJECTED"] as const) {
          const receipt = JSON.stringify({
            version: 1,
            invocationRef: "inv_fixture123",
            state,
            inputSHA256: "a".repeat(64),
          });
          const html = await render(tool, {}, locale, receipt);
          const header =
            (html.match(/<header[^>]*>[^]*?<\/header>/)?.[0] ?? "").split(
              "<details",
            )[0] ?? "";
          expect(header).toContain(`data-state="${state}"`);
          expect(header).toContain("status-badge--danger");
          expect(header).not.toContain('data-state="SUCCEEDED"');
          expect(html).not.toContain("run-transcript__preview");
          expect(html).toContain("Invocation Ref");
          expect(html).toContain("Input SHA256");
          expect(html).not.toMatch(/<details[^>]*\bopen\b/);
          expect(html).not.toMatch(
            /RAW_COMMAND_SENTINEL|RAW_OUTPUT_SENTINEL|HIDDEN_REASONING_SENTINEL/,
          );
        }
      }
    },
  );
});

describe("RunTranscript: компактные файлы результата", () => {
  const artifact: Artifact = {
    ref: "art_fixture_result",
    version: 2,
    revision: 3,
    projectRef: "prj_fixture",
    runRef: "run_fixture",
    sessionRef: "ses_fixture",
    fileName: "result-fixture.md",
    mediaType: "text/markdown",
    sizeBytes: 1234,
    digest: `sha256:${"a".repeat(64)}`,
    scanState: "CLEAN",
    lifecycleState: "ACTIVE",
    source: "AGENT_RESULT",
    agentBindings: [],
    previewAvailable: true,
    createdAt: "2026-10-04T10:00:00Z",
    nextActions: ["DOWNLOAD"],
  };
  const execution = {
    runRef: "run_fixture",
    nodeRef: "node_fixture",
    sessionRef: "ses_fixture",
    turnRef: "turn_fixture",
    turnNumber: 2,
    attempt: 1,
  };
  async function fileHtml(
    nextArtifact: Artifact | undefined,
    locale: "ru" | "en" = "ru",
    embedded = true,
    historical = false,
    overrides: Partial<RunActivityItem> = {},
  ): Promise<string> {
    const item: RunActivityItem = {
      id: "artifact-fixture",
      kind: "system",
      historical,
      actor: "Платформа",
      state: "SUCCEEDED",
      summary: "i18n:FILE_RESULT_AVAILABLE",
      progress: "DUPLICATE_ARTIFACT_PROGRESS",
      messageKind: "ARTIFACT",
      occurredAt: artifact.createdAt,
      artifact: nextArtifact,
      execution,
      ...overrides,
    };
    const previous = i18n.global.locale.value;
    i18n.global.locale.value = locale;
    try {
      const app = createSSRApp({
        render: () => h(RunTranscript, { items: [item], embedded }),
      });
      app.use(i18n);
      return await renderToString(app);
    } finally {
      i18n.global.locale.value = previous;
    }
  }

  it.each(["ru", "en"] as const)(
    "показывает имя, размер, scan status и доступное скачивание без повторов (%s)",
    async (locale) => {
      const before = structuredClone(artifact);
      const html = await fileHtml(artifact, locale);
      const details =
        /<details\b[^>]*class="run-file-event__details"[^>]*>([^]*?)<\/details>/.exec(
          html,
        );
      expect(details).not.toBeNull();
      const visible = html.replace(details?.[0] ?? "", "");
      expect(visible).toContain("result-fixture.md");
      expect(visible).toContain('class="run-file-event__metadata"');
      expect(visible).toContain(locale === "ru" ? "Проверен" : "Clean");
      expect(visible).toMatch(/1[,.]2/);
      expect(visible).toContain(
        `aria-label="${locale === "ru" ? "Скачать" : "Download"}: result-fixture.md"`,
      );
      expect(visible).not.toMatch(
        /<header|data-state="SUCCEEDED"|run-activity-item__icon|run-activity-item__message/,
      );
      expect(html).not.toMatch(
        /Файл результата доступен|FILE_RESULT_AVAILABLE|DUPLICATE_ARTIFACT_PROGRESS/,
      );
      expect(details?.[1]).toContain("text/markdown");
      expect(details?.[1]).toContain("v3");
      expect(details?.[1]).toContain(
        locale === "ru" ? "Ход 2 · попытка 1" : "Turn 2 · attempt 1",
      );
      expect(details?.[0]).not.toMatch(/<details[^>]*\bopen\b/);
      expect(html.match(/data-state="CLEAN"/g)).toHaveLength(1);
      expect(artifact).toEqual(before);
    },
  );

  it.each(["ru", "en"] as const)(
    "сворачивает exact служебную квитанцию, сохраняя download, metadata и tuple (%s)",
    async (locale) => {
      const receipt = {
        ...artifact,
        fileName: "workspace-write-result.json",
        mediaType: "application/json",
      };
      const before = structuredClone(receipt);
      const html = await fileHtml(receipt, locale);
      expect(html).toMatch(
        /<details[^>]*class="run-file-receipt"[^>]*><summary/,
      );
      expect(html).not.toMatch(/<details[^>]*\bopen\b/);
      expect(html).toContain(
        locale === "ru" ? "Служебная квитанция" : "Service receipt",
      );
      expect(html).toContain(
        `aria-label="${locale === "ru" ? "Скачать" : "Download"}: workspace-write-result.json"`,
      );
      expect(html).toContain("application/json");
      expect(html).toContain("v3");
      expect(html).toContain('data-turn-ref="turn_fixture"');
      expect(html).toContain('data-attempt="1"');
      const visible = html.replace(/<details[^>]*>[^]*?<\/details>/g, "");
      expect(visible).not.toContain("workspace-write-result.json");
      expect(receipt).toEqual(before);
    },
  );

  it.each([
    { source: "CONTROL_CENTER" },
    { source: "INTEGRATION_RESULT" },
    { fileName: "other-workspace-write-result.json" },
    { mediaType: "text/plain" },
    { scanState: "PENDING" },
    { scanState: "SCANNING" },
    { scanState: "QUARANTINED" },
    { scanState: "FAILED" },
    { runRef: "run_other" },
    { sessionRef: "ses_other" },
    { runRef: undefined },
    { sessionRef: undefined },
  ] satisfies Partial<Artifact>[])(
    "оставляет unmatched или небезопасную квитанцию видимой: %j",
    async (patch) => {
      const receipt = {
        ...artifact,
        fileName: "workspace-write-result.json",
        mediaType: "application/json",
        ...patch,
      };
      const html = await fileHtml(receipt);
      expect(html).not.toContain('class="run-file-receipt"');
      expect(html).toContain(receipt.fileName);
      expect(html).toContain(`data-state="${receipt.scanState}"`);
    },
  );

  it("не сворачивает квитанцию без exact execution", async () => {
    const html = await fileHtml(
      {
        ...artifact,
        fileName: "workspace-write-result.json",
        mediaType: "application/json",
      },
      "ru",
      true,
      false,
      { execution: undefined },
    );
    expect(html).not.toContain('class="run-file-receipt"');
    expect(html).toContain("workspace-write-result.json");
  });

  it.each([
    [true, false],
    [false, false],
    [true, true],
    [false, true],
  ])(
    "применяет общую строку в embedded=%s, historical=%s без потери execution tuple",
    async (embedded, historical) => {
      const html = await fileHtml(artifact, "ru", embedded, historical);
      expect(html).toContain("run-activity-item--file");
      expect(html).toContain('data-turn-ref="turn_fixture"');
      expect(html).toContain('data-attempt="1"');
      expect(html).toContain(
        'class="button button--ghost run-file-event__download',
      );
      expect(html).not.toContain("Файл результата доступен");
    },
  );

  it.each(["PENDING", "QUARANTINED", "FAILED"] as const)(
    "сохраняет серверный scan state %s и не выдаёт download без nextAction",
    async (scanState) => {
      const html = await fileHtml({ ...artifact, scanState, nextActions: [] });
      expect(html).toContain(`data-state="${scanState}"`);
      expect(html).not.toContain('data-state="CLEAN"');
      expect(html).not.toContain(
        'class="button button--ghost run-file-event__download',
      );
    },
  );

  it("сохраняет unavailable fallback без выдуманной file metadata", async () => {
    const html = await fileHtml(undefined);
    expect(html).toContain(i18n.global.t("runs.artifactUnavailable"));
    expect(html).not.toContain('class="run-file-event"');
    expect(html).not.toContain("result-fixture.md");
  });

  it("оставляет недоверенное имя текстом, а не active HTML", async () => {
    const html = await fileHtml({
      ...artifact,
      fileName: '<img src=x onerror="unexpected()">.md',
    });
    expect(html).toContain("&lt;img");
    expect(html).not.toContain("<img src=x");
    expect(html).toContain('class="run-file-event__name"');
  });
});

describe("RunTranscript: названия native инструментов", () => {
  it.each(["ru", "en"] as const)(
    "компактно локализует safe shell поля, оставляя provenance и lifecycle в закрытой диагностике (%s)",
    async (locale) => {
      const parameters = {
        action_count: 1,
        action_kinds: ["UNKNOWN"],
        cwd_scope: "WORKSPACE",
        exit_code: "ZERO",
        source: "UNIFIED_EXEC_STARTUP",
        codex_item_id: "item_fixture",
      };
      const before = structuredClone(parameters);
      const html = await render("CODEX_SHELL", parameters, locale, "COMPLETED");
      const diagnostic =
        /<details\b[^>]*class="run-native-tool__diagnostics"[^>]*>([^]*?)<\/details>/.exec(
          html,
        );
      expect(diagnostic).not.toBeNull();
      const useful = html.replace(diagnostic?.[0] ?? "", "");
      for (const text of locale === "ru"
        ? [
            "Количество действий",
            "Действия",
            "Не определено",
            "Рабочая папка",
            "Внутри рабочей папки",
            "Код завершения",
            "0 (успешно)",
            "Завершён",
          ]
        : [
            "Action count",
            "Actions",
            "Unspecified",
            "Working directory",
            "Inside workspace",
            "Exit code",
            "0 (successful)",
            "Succeeded",
          ])
        expect(useful).toContain(text);
      expect(diagnostic?.[1]).toContain(
        locale === "ru" ? "Технические сведения" : "Technical details",
      );
      expect(diagnostic?.[1]).toContain(
        locale === "ru" ? "Идентификатор Codex" : "Codex item identifier",
      );
      expect(diagnostic?.[1]).toContain(
        locale === "ru" ? "Источник" : "Source",
      );
      expect(diagnostic?.[1]).toContain("item_fixture");
      expect(diagnostic?.[1]).toContain("UNIFIED_EXEC_STARTUP");
      expect(diagnostic?.[1]).toContain("COMPLETED");
      expect(useful).not.toMatch(
        /CODEX_SHELL|item_fixture|UNIFIED_EXEC_STARTUP|COMPLETED|WORKSPACE|ZERO|UNKNOWN/,
      );
      expect(html).not.toMatch(/<details[^>]*\bopen\b/);
      expect(html).not.toMatch(
        /RAW_COMMAND_SENTINEL|RAW_OUTPUT_SENTINEL|HIDDEN_REASONING_SENTINEL/,
      );
      expect(parameters).toEqual(before);
    },
  );

  it("не скрывает ошибку и не выдумывает числовой ненулевой exit code", async () => {
    const html = await render(
      "CODEX_SHELL",
      {
        action_count: 2,
        action_kinds: ["READ", "SEARCH"],
        cwd_scope: "OUTSIDE_WORKSPACE",
        exit_code: "NONZERO",
        source: "AGENT",
      },
      "ru",
      "FAILED",
      "FAILED",
    );
    expect(html).toContain("чтение файлов");
    expect(html).toContain("поиск");
    expect(html).toContain("Вне рабочей папки");
    expect(html).toContain("Ненулевой (ошибка)");
    expect(html).toContain("Не удалось выполнить действие");
    expect(html).toContain('data-state="FAILED"');
  });

  it("сохраняет неизвестные безопасные поля и содержательный результат прежними viewers", async () => {
    const html = await render(
      "CODEX_SHELL",
      {
        cwd_scope: "FUTURE_SCOPE",
        action_kinds: ["FUTURE_ACTION"],
        future: {
          count: 3,
          note: "Безопасный результат",
          ref: "agt_fixture12345678",
        },
      },
      "ru",
      "Проверены 3 файла",
    );
    expect(html).toContain("FUTURE_SCOPE");
    expect(html).toContain("FUTURE_ACTION");
    expect(html).toContain("Future");
    expect(html).toContain("Безопасный результат");
    expect(html).toContain("Проверены 3 файла");
    expect(html).toContain("run-transcript__preview");
    expect(html).not.toContain("agt_fixture12345678");
  });

  it("не снимает границу глубины redaction при выделении native параметра", async () => {
    const html = await render("CODEX_SHELL", {
      action_count: {
        one: { two: { three: { four: { five: "DEPTH_SENTINEL" } } } },
      },
      codex_item_id: "agt_fixture12345678",
    });
    expect(html).not.toMatch(/DEPTH_SENTINEL|agt_fixture12345678/);
  });

  it.each(["ru", "en"] as const)(
    "не дублирует native COMPLETED под локализованным статусом (%s)",
    async (locale) => {
      const html = await render("CODEX_SHELL", {}, locale, "COMPLETED");
      expect(title(html)).toBe(
        locale === "ru" ? "Работа в терминале" : "Terminal action",
      );
      expect(html).toContain('data-state="SUCCEEDED"');
      expect(html).toContain(locale === "ru" ? "Завершён" : "Succeeded");
      expect(html).not.toContain("run-transcript__preview");
      expect(html).toMatch(/<details[^>]*>[^]*?COMPLETED[^]*?<\/details>/);
      expect(html).not.toMatch(/<details[^>]*\bopen\b/);
      expect(html.replace(/<details[^>]*>[^]*?<\/details>/g, "")).not.toContain(
        "COMPLETED",
      );
      expect(html).not.toMatch(
        /RAW_COMMAND_SENTINEL|RAW_OUTPUT_SENTINEL|HIDDEN_REASONING_SENTINEL/,
      );
    },
  );

  it.each(["ru", "en"] as const)(
    "не повторяет native RUNNING только при видимом индикаторе работы (%s)",
    async (locale) => {
      const active = await render(
        "CODEX_SHELL",
        {},
        locale,
        "RUNNING",
        "RUNNING",
        true,
      );
      expect(active).toContain("run-transcript__work");
      expect(active).not.toContain("run-transcript__preview");
      expect(active).toMatch(/<details[^>]*>[^]*?RUNNING[^]*?<\/details>/);
      const inactive = await render(
        "CODEX_SHELL",
        {},
        locale,
        "RUNNING",
        "RUNNING",
      );
      expect(inactive).toContain("run-transcript__preview");
    },
  );

  it.each([
    ["CODEX_SHELL", "COMPLETED", "RUNNING"],
    ["CODEX_SHELL", "RUNNING", "SUCCEEDED"],
    ["CODEX_SHELL", "COMPLETED: найдено 4 файла", "SUCCEEDED"],
    ["CODEX_SHELL", "Изменений нет", "SUCCEEDED"],
    ["project_files.search", "COMPLETED", "SUCCEEDED"],
  ] as const)(
    "сохраняет содержательный/несовпадающий/не-native результат %s: %s",
    async (tool, result, state) => {
      const html = await render(tool, {}, "ru", result, state);
      expect(html).toContain("run-transcript__preview");
      expect(html).toContain(result);
    },
  );

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
  it.each(["ru", "en"] as const)(
    "называет действие интеграции по опубликованной capability, без input (%s)",
    async (locale) => {
      const capability = "github.repository.pull_requests.list";
      const html = await render(
        "invoke_integration",
        {
          capability_key: capability,
          connection_ref: "icn_fixture123",
          input: { command: "RAW_INPUT_SENTINEL" },
          displayName: "UNTRUSTED_NAME_SENTINEL",
        },
        locale,
        "",
        "RUNNING",
        true,
        capability,
      );
      const expected = `${locale === "ru" ? "Вызов интеграции" : "Integration call"} · ${capability}`;
      expect(title(html)).toBe(expected);
      expect(html).toContain(
        `aria-label="${locale === "ru" ? "Подробности" : "Details"}: ${expected}"`,
      );
      const compactHeader = (
        html.match(/<header[^>]*>[^]*?<\/header>/)?.[0] ?? ""
      ).split("<details")[0];
      expect(compactHeader).not.toMatch(
        /RAW_INPUT_SENTINEL|UNTRUSTED_NAME_SENTINEL|icn_fixture123/,
      );
      expect(html).not.toMatch(/<details[^>]*\bopen\b/);
    },
  );
  it.each([
    undefined,
    "",
    "github.read\nunsafe",
    "<script>unsafe</script>",
    "https://example.invalid/?token=sentinel",
    "a".repeat(161),
  ])(
    "не берёт название из input при невалидной capability %s",
    async (capabilityRef) => {
      const html = await render(
        "invoke_integration",
        {
          capability_key: "github.repository.read",
          operation: "UNTRUSTED_OPERATION",
        },
        "ru",
        "",
        "SUCCEEDED",
        false,
        capabilityRef,
      );
      expect(title(html)).toBe("Вызов интеграции");
    },
  );
  it.each(["ru", "en"] as const)(
    "сворачивает служебные сведения в одно доступное раскрытие внутри header (%s)",
    async (locale) => {
      const html = await render(
        "get_configuration_catalog",
        { catalogKind: "ROLE_ENVIRONMENTS" },
        locale,
        "get_configuration_catalog:completed",
      );
      const header = /<header[^>]*>([^]*?)<\/header>/.exec(html)?.[1] ?? "";
      const details =
        /<details[^>]*>([^]*?)<\/details>/.exec(header)?.[1] ?? "";
      expect(html.match(/<details\b/g)).toHaveLength(1);
      expect(html.match(/<summary\b/g)).toHaveLength(1);
      expect(header).toContain('data-state="SUCCEEDED"');
      expect(header).toContain("<time");
      expect(header).toContain("run-tool-event__details");
      expect(details).toContain(
        locale === "ru"
          ? 'aria-label="Подробности: Каталог окружений"'
          : 'aria-label="Details: Environment catalog"',
      );
      expect(details).toContain(
        locale === "ru" ? "Безопасные параметры" : "Safe parameters",
      );
      expect(details).toContain(
        locale === "ru" ? "Безопасный результат" : "Safe result",
      );
      const duration =
        locale === "ru" ? "Длительность: 10 мс" : "Duration: 10 ms";
      expect(details).toContain(duration);
      expect(
        header.replace(/<details[^>]*>[^]*?<\/details>/, ""),
      ).not.toContain(duration);
      expect(html).not.toMatch(/<details[^>]*\bopen\b/);
      expect(html).not.toContain("run-transcript__preview");
      expect(html).not.toMatch(
        /RAW_COMMAND_SENTINEL|RAW_OUTPUT_SENTINEL|HIDDEN_REASONING_SENTINEL/,
      );
    },
  );
  it("не добавляет длительность работающему инструменту и сохраняет содержательный preview вне деталей", async () => {
    const running = await render(
      "get_configuration_catalog",
      {},
      "ru",
      "",
      "RUNNING",
    );
    expect(running).not.toContain("Длительность:");
    expect(title(running)).toBe("Каталог настроек");
    const meaningful = await render(
      "get_configuration_catalog",
      {},
      "ru",
      "Доступны две модели",
    );
    expect(meaningful).toMatch(
      /<\/header>[^]*?run-transcript__preview[^]*?Доступны две модели/,
    );
  });
  it.each([
    "invoke_integration",
    "context7_resolve_library_id",
    "context7_query_docs",
  ])(
    "оставляет успешную машинную квитанцию %s только в закрытых деталях",
    async (tool) => {
      const receipt = JSON.stringify({
        version: 1,
        invocationRef: "inv_fixture123",
        state: "SUCCEEDED",
        inputSHA256: "a".repeat(64),
      });
      const html = await render(tool, {}, "ru", receipt);
      expect(html).not.toContain("run-transcript__preview");
      expect(html).toContain("Invocation Ref");
      expect(html).toContain("Input SHA256");
      expect(html).not.toMatch(/<details[^>]*\bopen\b/);
      expect(html).toMatch(
        /<details[^>]*><summary[^>]*>Подробности[^]*?Безопасный результат[^]*?Invocation Ref/,
      );
    },
  );

  it("оставляет exact successful plan ref в деталях без дублирующего машинного preview", async () => {
    const html = await render(
      "propose_configuration_plan",
      {},
      "ru",
      "propose_configuration_plan:pln_fixtureplan123",
    );
    expect(title(html)).toBe("Настройки помощника");
    expect(html).not.toContain("run-transcript__preview");
    expect(html).toContain("propose_configuration_plan:—");
    expect(html).toContain("<details");
  });
  it.each([
    "propose_configuration_plan:pln_short",
    "propose_configuration_plan:run_fixtureplan123",
    "propose_configuration_plan:pln_fixtureplan123\nПроверьте настройки",
    "propose_configuration_plan:pln_fixtureplan123:Ошибка",
  ])(
    "сохраняет информативное или неподтверждённое описание %s",
    async (result) => {
      const html = await render("propose_configuration_plan", {}, "ru", result);
      expect(html).toContain("run-transcript__preview");
    },
  );
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
  it.each([
    ["ASSISTANTS", "Каталог помощников", "Assistant catalog"],
    [
      "RUNTIME_PROFILES",
      "Каталог профилей выполнения",
      "Runtime profile catalog",
    ],
    [
      "PROVIDER_ACCOUNTS",
      "Каталог учётных записей провайдера",
      "Provider account catalog",
    ],
    ["MODELS", "Каталог моделей", "Model catalog"],
    ["ROLE_IMAGE_RECIPES", "Каталог рецептов образов", "Image recipe catalog"],
    ["IMAGE_ARTIFACTS", "Каталог образов", "Image catalog"],
    ["ROLE_ENVIRONMENTS", "Каталог окружений", "Environment catalog"],
    ["CURRENT_CONFIGURATION", "Текущие настройки", "Current settings"],
  ])(
    "показывает закрытый вид каталога %s в компактной строке",
    async (catalogKind, ru, en) => {
      for (const [locale, expected] of [
        ["ru", ru],
        ["en", en],
      ] as const) {
        const html = await render(
          "get_configuration_catalog",
          { catalogKind },
          locale,
          "get_configuration_catalog:completed",
        );
        expect(title(html)).toBe(expected);
        expect(html).not.toContain("RAW_COMMAND_SENTINEL");
        expect(html).not.toContain("RAW_OUTPUT_SENTINEL");
        expect(html).not.toContain("HIDDEN_REASONING_SENTINEL");
        expect(html).not.toContain(
          'class="safe-markdown run-transcript__preview"',
        );
      }
    },
  );
  it.each([
    undefined,
    null,
    1,
    ["ROLE_ENVIRONMENTS"],
    {},
    "UNKNOWN_KIND",
    "role_environments",
    "ROLE_ENVIRONMENTS\nPRIVATE_SENTINEL",
    "__proto__",
  ])(
    "сохраняет общий заголовок для неизвестного вида каталога %j",
    async (catalogKind) => {
      expect(
        title(await render("get_configuration_catalog", { catalogKind })),
      ).toBe("Каталог настроек");
      expect(
        title(await render("get_configuration_catalog", { catalogKind }, "en")),
      ).toBe("Configuration catalog");
    },
  );
  it("не использует вид каталога в подписи другого инструмента", async () => {
    expect(
      title(
        await render("propose_configuration_plan", {
          catalogKind: "ROLE_ENVIRONMENTS",
        }),
      ),
    ).toBe("Настройки помощника");
    expect(
      title(
        await render("get_configuration_catalog", {
          assistant_configuration_catalog: { kind: "ROLE_ENVIRONMENTS" },
        }),
      ),
    ).toBe("Каталог настроек");
  });
});

describe("RunTranscript: безопасный runtime text", () => {
  it("показывает статус ошибки без служебного preview, а closed code только в закрытых деталях", async () => {
    for (const locale of ["ru", "en"] as const) {
      for (const code of ["TOOL_UNAVAILABLE", "FUTURE_TOOL_ERROR"]) {
        const html = await render(
          "get_configuration_catalog",
          {},
          locale,
          code,
          "FAILED",
        );
        expect(html).not.toContain("run-transcript__preview");
        expect(html).toMatch(
          new RegExp(`<details[^>]*><summary[^>]*>[^]*?${code}`),
        );
        expect(html).not.toMatch(/<details[^>]*\bopen\b/);
        expect(html.match(/data-state="FAILED"/g)).toHaveLength(1);
      }
    }
    const normal = await render(
      "get_configuration_catalog",
      {},
      "ru",
      "Не удалось прочитать выбранный файл",
      "FAILED",
    );
    expect(normal).toContain("Не удалось прочитать выбранный файл");
  });

  it("служебные этапы не создают пустую author/time шапку рядом с exact commentary или tool", async () => {
    const execution = {
      runRef: "run_exact",
      nodeRef: "nod_exact",
      sessionRef: "ses_exact",
      turnRef: "trn_exact",
      turnNumber: 1,
      attempt: 1,
    };
    const service: RunActivityItem = {
      id: "service",
      kind: "system",
      actor: "Системный помощник",
      occurredAt: "2026-10-04T10:00:00Z",
      historical: false,
      execution,
      eventType: "TURN_PROGRESS",
      messageKind: "STATE",
      state: "RUNNING",
      summary: "MODEL_REQUEST_RUNNING",
    };
    const comment: RunActivityItem = {
      ...service,
      id: "comment",
      kind: "agent",
      phase: "COMMENTARY",
      summary: "Проверяю настройки",
    };
    const tool: RunActivityItem = {
      ...service,
      id: "tool",
      kind: "tool",
      toolCall: {
        ref: "call_example",
        tool: "get_configuration_catalog",
        safeParameters: {},
        state: "SUCCEEDED",
        revision: 1,
        durationMs: 10,
        safeResult: "get_configuration_catalog:completed",
        auditRef: "aud_example",
      },
    };
    const renderItems = async (items: RunActivityItem[]) => {
      const app = createSSRApp({
        render: () =>
          h(RunTranscript, {
            items,
            embedded: true,
            activeItemId: items.at(-1)?.id ?? null,
          }),
      });
      app.use(i18n);
      return renderToString(app);
    };
    for (const semantic of [comment, tool]) {
      const html = await renderItems([service, semantic]);
      const serviceHtml =
        /<li[^>]*run-activity-item--service[^>]*>([^]*?)<\/article>/.exec(
          html,
        )?.[1];
      expect(serviceHtml).toBeDefined();
      expect(serviceHtml).not.toMatch(/<header\b|run-activity-item__icon/);
      expect(serviceHtml).toContain("run-transcript__service-history");
      expect(serviceHtml).toContain("Этапы выполнения: 1");
      expect(html.match(/<header\b/g)).toHaveLength(1);
    }
    const foreign = { ...comment, execution: { ...execution, attempt: 2 } };
    expect(
      (await renderItems([service, foreign])).match(/<header\b/g),
    ).toHaveLength(2);
    expect(
      (await renderItems([{ ...service, state: "FAILED" }, comment])).match(
        /<header\b/g,
      ),
    ).toHaveLength(2);
    expect(await renderItems([service])).toContain('role="status"');
    const source = readFileSync(
      new URL("./RunTranscript.vue", import.meta.url),
      "utf8",
    );
    expect(source).toMatch(
      /\.run-activity-item--service > article\s*\{\s*grid-column: 2;/,
    );
  });

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

  it.each(["ru", "en"] as const)(
    "сохраняет самостоятельный успешный результат интеграции в компактных details (%s)",
    async (locale) => {
      const previous = i18n.global.locale.value;
      i18n.global.locale.value = locale;
      try {
        for (const summary of [
          "i18n:INTEGRATION_ACTION_SUCCEEDED",
          i18n.global.t("serverMessages.INTEGRATION_ACTION_SUCCEEDED"),
        ]) {
          const item = progress("integration-success", {
            messageKind: "INTERMEDIATE_MESSAGE",
            summary,
            integrationInvocationRef: "inv_fixture123",
          });
          const key = executionKey(item.execution);
          if (!key) throw new Error("Missing synthetic execution key");
          const html = await transcript([item], [key]);
          expect(html.match(/class="run-activity-item /g)).toHaveLength(1);
          expect(html).toContain("run-activity-item--service");
          expect(html).toContain("run-transcript__service-history");
          expect(html).toContain(
            i18n.global.t("serverMessages.INTEGRATION_ACTION_SUCCEEDED"),
          );
          expect(html).toContain('data-turn-ref="trn_exact"');
          expect(html).toContain('data-attempt="1"');
          expect(html).not.toContain("<header");
          expect(html).not.toContain('class="run-transcript__execution"');
          expect(html).not.toContain('class="run-activity-item__message"');
          expect(html).not.toMatch(/<details[^>]*\bopen\b/);
          expect(item.summary).toBe(summary);
        }
      } finally {
        i18n.global.locale.value = previous;
      }
    },
  );

  it("не сворачивает неизвестный/failed исход, полезный текст, сообщения и записи без exact pins", async () => {
    const success = progress("integration-result", {
      messageKind: "INTERMEDIATE_MESSAGE",
      summary: "i18n:INTEGRATION_ACTION_SUCCEEDED",
      integrationInvocationRef: "inv_fixture123",
    });
    const key = executionKey(success.execution);
    if (!key) throw new Error("Missing synthetic execution key");
    for (const changes of [
      { summary: "i18n:INTEGRATION_ACTION_FAILED" },
      { summary: "i18n:INTEGRATION_ACTION_OUTCOME_UNKNOWN" },
      { summary: "Действие интеграции выполнено успешно. Важный результат" },
      { state: "FAILED" as const },
      { state: "CANCELLED" as const },
      { execution: undefined, historical: true },
      { execution: { ...execution, attempt: 0 } },
      { integrationInvocationRef: undefined },
      { integrationInvocationRef: "inv_short" },
      { progress: "Важный результат" },
      { phase: "COMMENTARY" as const, kind: "agent" as const },
      { eventType: "TOOL_CALL_RECORDED" as const },
      { messageKind: "TOOL_CALL" as const },
      { artifactRef: "art_fixture123" },
    ]) {
      const html = await transcript([{ ...success, ...changes }], [key]);
      expect(html).toContain("<header");
      expect(html).not.toContain("run-transcript__service-history");
    }
  });

  it("не объединяет compact success с соседним tool без совпадающего invocation pin", async () => {
    const success = progress("integration-success", {
      messageKind: "INTERMEDIATE_MESSAGE",
      summary: "Действие интеграции выполнено успешно",
      integrationInvocationRef: "inv_fixture123",
    });
    const tool = progress("integration-tool", {
      kind: "tool",
      eventType: "TOOL_CALL_RECORDED",
      messageKind: "TOOL_CALL",
      summary: undefined,
      toolCall: {
        ref: "tcl_fixture123",
        tool: "invoke_integration",
        state: "SUCCEEDED",
        revision: 2,
        durationMs: 10,
        safeParameters: {},
        safeResult: JSON.stringify({
          version: 1,
          invocationRef: "inv_fixture123",
          state: "SUCCEEDED",
          inputSHA256: "a".repeat(64),
        }),
        auditRef: "aud_fixture123",
      },
    });
    const html = await transcript([success, tool]);
    expect(html.match(/class="run-activity-item /g)).toHaveLength(2);
    expect(html.match(/<header\b/g)).toHaveLength(1);
    expect(html).toContain("run-transcript__service-history");
    expect(html).toContain("Вызов интеграции");
    expect(html).not.toContain('class="run-transcript__execution"');
    expect(html).not.toContain('role="status"');
    expect(success).not.toHaveProperty("serviceHistory");
    expect(tool.integrationInvocationRef).toBeUndefined();
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

  it("закрытые служебные этапы без semantic сообщения не получают пустую author/time шапку", async () => {
    const scope = executionKey(execution);
    if (!scope) throw new Error("Synthetic execution scope is missing");
    const html = await transcript(
      [progress("start"), progress("model")],
      [scope],
    );
    expect(html).not.toMatch(/<header\b|run-activity-item__icon/);
    expect(html).toContain("Этапы выполнения: 2");
    expect(html).not.toContain('role="status"');
  });

  it("показывает одну primary отмену exact node/intermediate, сохраняя commentary и unbound историю", async () => {
    const html = await transcript([
      progress("start"),
      progress("comment", {
        kind: "agent",
        phase: "COMMENTARY",
        summary: "Проверяю настройки",
      }),
      progress("cancel-node", {
        eventType: "NODE_STATE_CHANGED",
        messageKind: "STATE",
        state: "CANCELLED",
        summary: "Шаг выполнения отменён",
        serviceCancellationCode: "RUN_NODE_CANCELLED",
      }),
      progress("cancel-intermediate", {
        eventType: "TURN_PROGRESS",
        messageKind: "INTERMEDIATE_MESSAGE",
        state: "CANCELLED",
        summary: "Запуск отменён",
        serviceCancellationCode: "RUN_CANCELLED",
      }),
      progress("unbound-run-cancel", {
        historical: true,
        execution: undefined,
        eventType: "RUN_STATE_CHANGED",
        messageKind: "STATE",
        state: "CANCELLED",
        summary: "Запуск отменён",
        serviceCancellationCode: "RUN_CANCELLED",
      }),
    ]);
    const primary = html.replace(/<details\b[^]*?<\/details>/g, "");
    expect(primary.match(/Запуск отменён/g)).toHaveLength(1);
    expect(primary).not.toContain("Шаг выполнения отменён");
    expect(primary).toContain("Проверяю настройки");
    expect(html).toContain("Шаг выполнения отменён");
    expect(html).toContain("Этапы выполнения: 3");
    expect(html).toContain("Этапы выполнения: 1");
    expect(html.match(/Запуск отменён/g)).toHaveLength(3);
    expect(html).not.toMatch(/<details[^>]*\bopen\b/);
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
