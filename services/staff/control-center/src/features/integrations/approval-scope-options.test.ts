import { describe, expect, it } from "vitest";
import {
  approvalScopeOptions,
  validApprovalScopeSelection,
} from "./approval-scope-options";

describe("область повторного согласования", () => {
  const schema = JSON.stringify({
    type: "object",
    properties: {
      action: { type: "string" },
      issue: { type: "object", properties: { id: { type: "integer" } } },
      "a/b": { type: "boolean" },
      unsupported: { type: "null" },
    },
  });

  it("предлагает только типизированные JSON Pointer из схемы", () => {
    expect(approvalScopeOptions(schema)).toEqual([
      "/action",
      "/a~1b",
      "/issue",
      "/issue/id",
    ]);
    expect(approvalScopeOptions("{")).toEqual([]);
    expect(approvalScopeOptions(JSON.stringify({ type: "array" }))).toEqual([]);
  });

  it("закрывает пустой, повторный и неописанный выбор", () => {
    const choices = approvalScopeOptions(schema);
    expect(validApprovalScopeSelection(["/action", "/issue/id"], choices)).toBe(
      true,
    );
    expect(validApprovalScopeSelection([], choices)).toBe(false);
    expect(validApprovalScopeSelection(["/action", "/action"], choices)).toBe(
      false,
    );
    expect(validApprovalScopeSelection(["/secret"], choices)).toBe(false);
  });

  it("сохраняет обязательный объект инструмента и отдельно поддерживает область самого подключения", () => {
    const choices = ["/project_id", "/merge_request_iid", "/body"];
    const required = ["/project_id", "/merge_request_iid"];
    expect(validApprovalScopeSelection(required, choices, required)).toBe(true);
    expect(
      validApprovalScopeSelection([...required, "/body"], choices, required),
    ).toBe(true);
    expect(validApprovalScopeSelection(["/body"], choices, required)).toBe(
      false,
    );
    expect(
      validApprovalScopeSelection(["/project_id"], choices, required),
    ).toBe(false);
    expect(validApprovalScopeSelection(required, choices, ["/unknown"])).toBe(
      false,
    );
    expect(
      validApprovalScopeSelection(required, choices, [
        "/project_id",
        "/project_id",
      ]),
    ).toBe(false);
    expect(validApprovalScopeSelection([], [], [])).toBe(true);
    expect(validApprovalScopeSelection([], [])).toBe(false);
  });
});
