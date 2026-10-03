import { describe, expect, it } from "vitest";
import {
  assertRuntimeResourceScope,
  runtimeResourceScopeKey,
} from "./resource-scope";

describe("Явная область runtime-ресурса", () => {
  it("различает организацию и проекты без служебного projectRef", () => {
    expect(runtimeResourceScopeKey({ kind: "ORGANIZATION" })).toBe(
      "ORGANIZATION",
    );
    expect(
      runtimeResourceScopeKey({ kind: "PROJECT", projectRef: "prj_alpha" }),
    ).toBe("PROJECT:prj_alpha");
    expect(() =>
      runtimeResourceScopeKey({ kind: "PROJECT", projectRef: "" }),
    ).toThrow();
  });
  it("не выдаёт проектный ресурс за системный", () => {
    expect(() =>
      assertRuntimeResourceScope({ kind: "ORGANIZATION" }, "prj_alpha"),
    ).toThrow();
    expect(() =>
      assertRuntimeResourceScope(
        { kind: "PROJECT", projectRef: "prj_alpha" },
        undefined,
      ),
    ).toThrow();
    expect(() =>
      assertRuntimeResourceScope(
        { kind: "PROJECT", projectRef: "prj_alpha" },
        "prj_beta",
      ),
    ).toThrow();
  });
});
