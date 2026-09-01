import { beforeEach, describe, expect, it, vi } from "vitest";

const { listTemplateVariables, previewPromptTemplate } = vi.hoisted(() => ({
  listTemplateVariables: vi.fn(),
  previewPromptTemplate: vi.fn(),
}));

vi.mock("@/shared/api/generated/openapi/sdk.gen", () => ({
  listTemplateVariables,
  previewPromptTemplate,
}));
vi.mock("@/shared/api/mutation", () => ({
  csrfToken: () => "c".repeat(43),
}));
vi.mock("@/shared/api/problem", () => ({
  unwrap: (value: unknown) => Promise.resolve(value),
}));

import {
  createTemplateVariableLoader,
  loadMaterializedTemplatePreview,
} from "@/features/agents/detail/api";

describe("agent detail api", () => {
  beforeEach(() => {
    listTemplateVariables.mockReset();
    previewPromptTemplate.mockReset();
  });

  it("передаёт серверу поиск и cursor, сохраняя scope переменной", async () => {
    listTemplateVariables.mockResolvedValue({
      data: {
        items: [
          {
            name: "runtime.environment.tools",
            valueType: "collection",
            description: "Разрешённые инструменты",
            example:
              "{{ range .runtime.environment.tools }}{{ .name }}{{ end }}",
            source: "RUNTIME",
          },
        ],
        nextPageToken: "runtime.environment.tools",
      },
    });
    const signal = new AbortController().signal;
    const page = await createTemplateVariableLoader("project_sales")({
      query: "tools",
      cursor: "agent.ref",
      signal,
    });

    expect(listTemplateVariables).toHaveBeenCalledWith({
      path: { projectRef: "project_sales" },
      query: {
        pageSize: 50,
        query: "tools",
        pageToken: "agent.ref",
      },
      signal,
    });
    expect(page.items[0]).toMatchObject({
      id: "runtime.environment.tools",
      scope: "RUNTIME",
      variable: {
        collection: true,
        itemFields: [],
        rangeExample:
          "{{ range .runtime.environment.tools }}{{ .name }}{{ end }}",
        valueType: "COLLECTION",
      },
    });
    expect(page.nextCursor).toBe("runtime.environment.tools");
  });

  it("получает synthetic materialized preview без локальной подстановки", async () => {
    previewPromptTemplate.mockResolvedValue({
      data: {
        safePreview: "Проект: demo",
        fullMaterializedPrompt: "Проект: demo\nИнструменты: gh",
        diagnostics: [],
      },
    });
    const signal = new AbortController().signal;
    const preview = await loadMaterializedTemplatePreview(
      "Проект: {{ .project.name }}",
      signal,
    );

    expect(previewPromptTemplate).toHaveBeenCalledWith({
      body: {
        template: "Проект: {{ .project.name }}",
        targetKind: "SYNTHETIC",
        includeFullMaterialization: true,
      },
      headers: { "X-CSRF-Token": "c".repeat(43) },
      signal,
    });
    expect(preview.fullMaterializedPrompt).toContain("Инструменты: gh");
  });
});
