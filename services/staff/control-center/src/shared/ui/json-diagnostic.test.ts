import { describe, expect, it } from "vitest";
import { jsonSyntaxIssue } from "./json-diagnostic";

describe("JSON diagnostic", () => {
  it("принимает только полностью разобранный JSON", () => {
    expect(jsonSyntaxIssue('{"value": [true, 3, null]}')).toBeUndefined();
    expect(jsonSyntaxIssue("null")).toBeUndefined();
    expect(jsonSyntaxIssue('{"value": 3} trailing')).toBeDefined();
  });
  it("возвращает line/column без содержимого значения", () => {
    const diagnostic = jsonSyntaxIssue('{\n  "value": secret_fixture\n}');
    expect(diagnostic?.line).toBe(2);
    expect(diagnostic?.column).toBeGreaterThan(1);
    expect(JSON.stringify(diagnostic)).not.toContain("secret_fixture");
  });
});
