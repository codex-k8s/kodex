import { describe, expect, it } from "vitest";

import {
  agentInitials,
  extractTemplateVariables,
  insertTextAtSelection,
  runtimeModels,
  runtimeProviders,
  runtimeRefForSelection,
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
  });

  it("выделяет переменные и синтаксические токены без HTML", () => {
    expect(
      extractTemplateVariables(
        "# Роль\n{{project.name}} и {{ run.task }} и {{project.name}}",
      ),
    ).toEqual(["{{project.name}}", "{{run.task}}"]);
    expect(tokenizeCodeLine('model = "gpt-5.1"', "toml")).toEqual([
      { text: "model", tone: "keyword" },
      { text: " = ", tone: "plain" },
      { text: '"gpt-5.1"', tone: "string" },
    ]);
    expect(
      tokenizeCodeLine("- Проверь {{run.task}}", "markdown").map(
        (token) => token.tone,
      ),
    ).toContain("variable");
    expect(agentInitials("Аналитик продаж")).toBe("АП");
  });

  it("вставляет server-owned template variable строго в текущее выделение", () => {
    const item = toTemplateVariablePickerItem({
      name: "project.name",
      valueType: "string",
      description: "Имя проекта",
      example: "Продажи",
      source: "PROJECT",
    });

    expect(item.scope).toBe("PROJECT");
    expect(templateVariableInsertion(item.variable.name)).toBe(
      "{{project.name}}",
    );
    expect(insertTextAtSelection("До после", "{{project.name}}", 3, 3)).toEqual(
      {
        value: "До {{project.name}}после",
        selectionStart: 19,
        selectionEnd: 19,
      },
    );
    expect(insertTextAtSelection("До X после", "{{run.ref}}", 3, 4).value).toBe(
      "До {{run.ref}} после",
    );
  });
});
