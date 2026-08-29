import { describe, expect, it } from "vitest";

import { callbackReturnPath } from "./callback";

describe("OIDC callback return", () => {
  it("возвращает fresh re-auth на исходный project secrets route", () => {
    expect(
      callbackReturnPath({
        kind: "runtime-secret",
        returnPath: "/projects/project_sales/secrets",
      }),
    ).toBe("/projects/project_sales/secrets");
  });

  it("сохраняет обычный onboarding routing", () => {
    expect(callbackReturnPath({ kind: "login" }, false)).toBe("/onboarding");
    expect(callbackReturnPath({ kind: "login" }, true)).toBe("/");
  });

  it("закрыто отклоняет runtime callback без return path", () => {
    expect(() => callbackReturnPath({ kind: "runtime-secret" })).toThrow(
      "return path is unavailable",
    );
  });
});
