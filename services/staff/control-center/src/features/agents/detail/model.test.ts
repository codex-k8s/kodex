import { describe, expect, it } from "vitest";

import {
  agentInitials,
  extractTemplateVariables,
  insertTextAtSelection,
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
        "# Роль\n{{project.ref}} и {{ run.ref }} и {{project.ref}}",
      ),
    ).toEqual(["{{project.ref}}", "{{run.ref}}"]);
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
    expect(templateVariableInsertion(item.variable.name)).toBe(
      "{{project.ref}}",
    );
    expect(insertTextAtSelection("До после", "{{project.ref}}", 3, 3)).toEqual({
      value: "До {{project.ref}}после",
      selectionStart: 18,
      selectionEnd: 18,
    });
    expect(insertTextAtSelection("До X после", "{{run.ref}}", 3, 4).value).toBe(
      "До {{run.ref}} после",
    );
  });
});
