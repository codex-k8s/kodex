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
            name: "environment.tools",
            valueType: "array",
            description: "Разрешённые инструменты",
            example: "[]",
            source: "ENVIRONMENT",
          },
        ],
        nextPageToken: "environment.tools",
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
      id: "environment.tools",
      scope: "ENVIRONMENT",
    });
    expect(page.nextCursor).toBe("environment.tools");
  });
});
