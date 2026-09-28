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
});
