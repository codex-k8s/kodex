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
  it("возвращает fresh re-auth к форме создания секрета", () => {
    expect(
      callbackReturnPath({
        kind: "runtime-secret-draft",
        returnPath: "/projects/project_sales/secrets?assistantCreateSecret=1",
      }),
    ).toBe("/projects/project_sales/secrets?assistantCreateSecret=1");
  });

  it("возвращает fresh re-auth в редактор окружения", () => {
    expect(
      callbackReturnPath({
        kind: "runtime-environment-policy",
        returnPath: "/projects/project_sales/environments/environment_main",
      }),
    ).toBe("/projects/project_sales/environments/environment_main");
  });

  it("сохраняет обычный onboarding routing", () => {
    expect(callbackReturnPath({ kind: "login" }, false)).toBe("/onboarding");
    expect(callbackReturnPath({ kind: "login" }, true)).toBe("/");
  });

  it("закрыто отклоняет любой re-auth callback без return path", () => {
    expect(() => callbackReturnPath({ kind: "runtime-secret" })).toThrow(
      "return path is unavailable",
    );
    expect(() =>
      callbackReturnPath({ kind: "runtime-environment-policy" }),
    ).toThrow("return path is unavailable");
  });
});
