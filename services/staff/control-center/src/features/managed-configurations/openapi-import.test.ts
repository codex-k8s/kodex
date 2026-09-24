import { describe, expect, it } from "vitest";
import type { OpenApiInspectionResult } from "@/shared/api/generated/openapi/types.gen";
import { openAPIImportContent } from "./openapi-import";

const inspection: OpenApiInspectionResult = {
  digest: "a".repeat(64),
  title: "Заявки",
  version: "1.0.0",
  operations: [
    {
      operationId: "getHealth",
      method: "GET",
      path: "/health",
      summary: "Проверка",
      serverOrigin: "https://api.example.test",
      candidate: true,
      healthCandidate: true,
      reason: "",
    },
    {
      operationId: "updateTicket",
      method: "PATCH",
      path: "/tickets/{id}",
      summary: "Изменить заявку",
      serverOrigin: "https://api.example.test",
      candidate: true,
      healthCandidate: false,
      reason: "",
    },
    {
      operationId: "unknown",
      method: "HEAD",
      path: "/unsupported",
      summary: "Недоступно",
      serverOrigin: "https://api.example.test",
      candidate: false,
      healthCandidate: false,
      reason: "HTTP_METHOD_UNSUPPORTED",
    },
  ],
};
const source = "openapi: 3.1.0";
const base = {
  source,
  inspectedSource: source,
  inspection,
  name: "Заявки",
  version: "1.0.0",
  selectedIds: ["getHealth", "updateTicket"],
  healthOperationId: "getHealth",
  risks: {} as Record<string, "WRITE" | "SENSITIVE" | "DESTRUCTIVE">,
  approvals: {} as Record<string, "HUMAN_EACH_EFFECT" | "HUMAN_SCOPED">,
};

describe("OpenAPI import selection", () => {
  it("назначает чтение без gate и запись с gate", () => {
    const content = JSON.parse(openAPIImportContent(base)) as {
      options: { choices: Array<{ risk: string; approvalPolicy: string }> };
    };
    expect(content.options.choices).toEqual([
      { operationId: "getHealth", risk: "READ", approvalPolicy: "NONE" },
      {
        operationId: "updateTicket",
        risk: "WRITE",
        approvalPolicy: "HUMAN_EACH_EFFECT",
      },
    ]);
  });

  it("не принимает устаревший документ, неподтвержденную health и чужую операцию", () => {
    expect(() =>
      openAPIImportContent({ ...base, source: source + " changed" }),
    ).toThrow();
    expect(() =>
      openAPIImportContent({ ...base, healthOperationId: "updateTicket" }),
    ).toThrow();
    expect(() =>
      openAPIImportContent({ ...base, selectedIds: ["getHealth", "unknown"] }),
    ).toThrow();
    expect(() =>
      openAPIImportContent({ ...base, selectedIds: ["getHealth", "foreign"] }),
    ).toThrow();
  });
});
