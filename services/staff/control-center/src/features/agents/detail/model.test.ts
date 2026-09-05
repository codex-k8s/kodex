import { describe, expect, it } from "vitest";

import {
  agentDetailTabFromQuery,
  agentInitials,
  extractTemplateVariables,
  insertTextAtSelection,
  normalizeTemplateVariable,
  runtimeModels,
  runtimeProviders,
  runtimeRefForSelection,
  runtimeSelectionByRef,
  runtimesForSelection,
  templateVariableInsertion,
  tokenizeCodeLine,
  toTemplateVariablePickerItem,
} from "@/features/agents/detail/model";
import type { RuntimeSelection } from "@/shared/api/generated/openapi/types.gen";

const runtimes: RuntimeSelection[] = [
  {
    ref: "runtime_openai_large",
    name: "Standard",
    revision: "runtime-v2",
    ready: true,
    provider: "openai-codex",
    model: "gpt-5.1",
  },
  {
    ref: "runtime_openai_small",
    name: "Economy",
    revision: "runtime-v1",
    ready: true,
    provider: "openai-codex",
    model: "gpt-5.1-mini",
  },
  {
    ref: "runtime_unready",
    name: "Unavailable",
    revision: "runtime-v3",
    ready: false,
    provider: "other",
    model: "model-x",
  },
];

describe("agentDetailTabFromQuery", () => {
  it.each(["profile", "instructions", "runtime", "environment", "access"])(
    "принимает вкладку %s",
    (tab) => {
      expect(agentDetailTabFromQuery(tab)).toBe(tab);
    },
  );

  it.each([undefined, null, "", "unknown", ["runtime"]])(
    "возвращает профиль для некорректного значения %j",
    (value) => {
      expect(agentDetailTabFromQuery(value)).toBe("profile");
    },
  );
});

describe("agent detail model", () => {
  it("строит provider/model/runtime выбор только из готового каталога", () => {
    expect(runtimeProviders(runtimes)).toEqual(["openai-codex"]);
    expect(runtimeModels(runtimes, "openai-codex")).toEqual([
      "gpt-5.1",
      "gpt-5.1-mini",
    ]);
    expect(runtimesForSelection(runtimes, "openai-codex", "gpt-5.1")).toEqual([
      runtimes[0],
    ]);
    expect(
      runtimeRefForSelection(runtimes, "openai-codex", "gpt-5.1-mini"),
    ).toBe("runtime_openai_small");
    expect(runtimeRefForSelection(runtimes, "other")).toBeUndefined();
    expect(runtimeSelectionByRef(runtimes, "runtime_unready")).toEqual(
      expect.objectContaining({ ready: false }),
    );
    expect(runtimeSelectionByRef(runtimes, "runtime_missing")).toBeUndefined();
  });

  it("выделяет переменные и синтаксические токены без HTML", () => {
    expect(
      extractTemplateVariables(
        "# Роль\n{{project.ref}} и {{ .run.ref }} и {{ range .runtime.environment.tools }}{{ end }}",
      ),
    ).toEqual([
      "{{ .project.ref }}",
      "{{ .run.ref }}",
      "{{ .runtime.environment.tools }}",
    ]);
    expect(tokenizeCodeLine('model = "gpt-5.1"', "toml")).toEqual([
      { text: "model", tone: "keyword" },
      { text: " = ", tone: "plain" },
      { text: '"gpt-5.1"', tone: "string" },
    ]);
    expect(
      tokenizeCodeLine("- Проверь {{run.ref}}", "markdown").map(
        (token) => token.tone,
      ),
    ).toContain("variable");
    expect(agentInitials("Аналитик продаж")).toBe("АП");
  });

  it("вставляет server-owned template variable строго в текущее выделение", () => {
    const item = toTemplateVariablePickerItem({
      name: "project.ref",
      available: true,
      reason: "AVAILABLE",
      valueType: "OPAQUE_REF",
      description: "Ссылка Проекта",
      example: "{{ .project.ref }}",
      source: "PROJECT",
      collection: false,
      itemFields: [],
    });

    expect(item.scope).toBe("PROJECT");
    expect(item.variable).toMatchObject({
      valueType: "OPAQUE_REF",
      example: "{{ .project.ref }}",
      source: "PROJECT",
    });
    expect(templateVariableInsertion(item.variable)).toBe("{{ .project.ref }}");
    expect(insertTextAtSelection("До после", "{{project.ref}}", 3, 3)).toEqual({
      value: "До {{project.ref}}после",
      selectionStart: 18,
      selectionEnd: 18,
    });
    expect(insertTextAtSelection("До X после", "{{run.ref}}", 3, 4).value).toBe(
      "До {{run.ref}} после",
    );

    expect(
      templateVariableInsertion({
        name: "runtime.environment.tools",
        available: true,
        reason: "AVAILABLE",
        valueType: "COLLECTION",
        description: "Инструменты",
        example: "{{ .runtime.environment.tools }}",
        source: "RUNTIME",
        collection: true,
        itemValueType: "OBJECT",
        itemFields: [],
        rangeExample:
          "{{ range .runtime.environment.tools }}{{ .name }}{{ end }}",
      }),
    ).toBe("{{ range .runtime.environment.tools }}{{ .name }}{{ end }}");
  });

  it("сохраняет недоступную переменную для просмотра, запрещая вставку", () => {
    const item = toTemplateVariablePickerItem({
      name: "agent.ref",
      available: false,
      reason: "AGENT_CONTEXT_REQUIRED",
      collection: false,
      itemFields: [],
      valueType: "OPAQUE_REF",
      description: "Агент",
      example: "{{ .agent.ref }}",
      source: "AGENT",
    });
    expect(item.disabled).toBe(true);
    expect(item.variable.reason).toBe("AGENT_CONTEXT_REQUIRED");
    expect(() => templateVariableInsertion(item.variable)).toThrow();
  });

  it.each([
    { available: undefined, reason: "AVAILABLE" },
    { available: true, reason: "NOT_MATERIALIZED" },
    { available: false, reason: "AVAILABLE" },
    { available: false, reason: "UNKNOWN" },
  ])("закрыто отклоняет некорректную availability %j", (availability) => {
    const variable = {
      name: "agent.ref",
      valueType: "OPAQUE_REF",
      description: "Агент",
      example: "{{ .agent.ref }}",
      source: "AGENT",
      ...availability,
    } as unknown as Parameters<typeof normalizeTemplateVariable>[0];
    expect(() => normalizeTemplateVariable(variable)).toThrow();
  });

  it("нормализует wire-каталог без локального придумывания шаблона", () => {
    expect(
      normalizeTemplateVariable({
        name: "input.files",
        available: true,
        reason: "AVAILABLE",
        valueType: "collection",
        description: "Файлы входа",
        example: "{{ range .input.files }}{{ .path }}{{ end }}",
        source: "INPUT_FILES",
      }),
    ).toMatchObject({
      valueType: "COLLECTION",
      collection: true,
      itemFields: [],
      rangeExample: "{{ range .input.files }}{{ .path }}{{ end }}",
    });
  });
});
