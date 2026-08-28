import { describe, expect, it } from "vitest";

import {
  agentInitials,
  createLocalEnvironmentLoader,
  extractTemplateVariables,
  runtimeModels,
  runtimeProviders,
  runtimeRefForSelection,
  runtimesForSelection,
  tokenizeCodeLine,
  type EnvironmentPickerItem,
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

  it("разбивает уже загруженный каталог на cursor-страницы и ищет по ПО", async () => {
    const items: EnvironmentPickerItem[] = [
      {
        id: "standard",
        label: "Стандартное",
        description: "Общие задачи",
        software: ["bash"],
        environment: {
          key: "standard",
          nameMessageKey: "standard.name",
          descriptionMessageKey: "standard.description",
          softwareMessageKeys: ["software.bash"],
          platforms: [{ os: "linux", architecture: "amd64" }],
          recommended: true,
          available: true,
          customInstallationAllowed: false,
        },
      },
      {
        id: "documents",
        label: "Документы",
        description: "PDF и OCR",
        software: ["pdftotext", "tesseract"],
        environment: {
          key: "documents",
          nameMessageKey: "documents.name",
          descriptionMessageKey: "documents.description",
          softwareMessageKeys: ["software.pdf"],
          platforms: [{ os: "linux", architecture: "amd64" }],
          recommended: false,
          available: true,
          customInstallationAllowed: false,
        },
      },
    ];
    const loader = createLocalEnvironmentLoader(() => items, 1);

    const first = await loader({
      query: "",
      signal: new AbortController().signal,
    });
    expect(first.items.map((item) => item.id)).toEqual(["standard"]);
    expect(first.nextCursor).toBe("1");

    const second = await loader({
      query: "",
      cursor: first.nextCursor ?? undefined,
      signal: new AbortController().signal,
    });
    expect(second.items.map((item) => item.id)).toEqual(["documents"]);
    expect(second.nextCursor).toBeNull();

    const search = await loader({
      query: "tesseract",
      signal: new AbortController().signal,
    });
    expect(search.items.map((item) => item.id)).toEqual(["documents"]);
  });
});
