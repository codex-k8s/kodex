import { describe, expect, it } from "vitest";

import {
  maskedSecretHint,
  validateSecretValue,
  type RuntimeSecret,
} from "./model";

const secret: RuntimeSecret = {
  ref: "secret_main",
  version: 3,
  projectRef: "project_sales",
  name: "CRM_TOKEN",
  description: "Токен CRM",
  valueType: "STRING",
  state: "ACTIVE",
  currentRevision: 2,
  createdAt: "2026-08-29T08:00:00Z",
  updatedAt: "2026-08-29T09:00:00Z",
};

describe("runtime secret model", () => {
  it("показывает только серверную маску и не пытается восстановить значение", () => {
    expect(
      maskedSecretHint({
        ...secret,
        displayHint: { prefix: "tok", suffix: "9z" },
      }),
    ).toBe("tok••••••9z");
    expect(maskedSecretHint(secret)).toBe("••••••");
  });

  it("проверяет обязательность и синтаксис JSON без сохранения значения", () => {
    expect(validateSecretValue("STRING", "")).toBe("required");
    expect(validateSecretValue("JSON", "{")).toBe("invalid-json");
    expect(validateSecretValue("JSON", '{"enabled":true}')).toBeUndefined();
  });
});
