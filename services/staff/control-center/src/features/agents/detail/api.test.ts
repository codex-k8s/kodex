import { beforeEach, describe, expect, it, vi } from "vitest";

const { listTemplateVariables } = vi.hoisted(() => ({
  listTemplateVariables: vi.fn(),
}));

vi.mock("@/shared/api/generated/openapi/sdk.gen", () => ({
  listTemplateVariables,
}));
vi.mock("@/shared/api/problem", () => ({
  unwrap: (value: unknown) => Promise.resolve(value),
}));

import { createTemplateVariableLoader } from "@/features/agents/detail/api";

describe("agent detail api", () => {
  beforeEach(() => listTemplateVariables.mockReset());

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
});
