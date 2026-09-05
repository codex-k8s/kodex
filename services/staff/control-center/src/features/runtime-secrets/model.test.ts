import { describe, expect, it } from "vitest";

import {
  canRuntimeSecretAction,
  maskedSecretHint,
  normalizeSecretPage,
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
  nextActions: ["ROTATE", "REVOKE", "REVEAL"],
  createdAt: "2026-08-29T08:00:00Z",
  updatedAt: "2026-08-29T09:00:00Z",
};

describe("runtime secret model", () => {
  it("нормализует пустой cursor и отклоняет поврежденные страницы до публикации", () => {
    expect(normalizeSecretPage({ items: [secret] }).nextPageToken).toBe("");
    for (const page of [
      null,
      {},
      { items: {} },
      { items: [null] },
      { items: [secret, secret] },
      { items: [{ ...secret, displayHint: { prefix: 42 } }] },
      { items: [{ ...secret, createdAt: "invalid" }] },
      { items: [{ ...secret, updatedAt: "invalid" }] },
      { items: [{ ...secret, projectRef: "" }] },
      { items: [secret], nextPageToken: 1 },
    ]) {
      expect(() => normalizeSecretPage(page)).toThrow();
    }
  });

  it("не переносит неизвестные поля ответа в metadata state", () => {
    const result = normalizeSecretPage({
      items: [
        { ...secret, value: "private", credentials: { token: "private" } },
      ],
    });
    expect(result.items).toEqual([secret]);
    expect(JSON.stringify(result)).not.toContain("private");
  });

  it("принимает только канонический Base64 без пробелов и неявного padding", () => {
    for (const value of ["Zg==", "Zm8=", "Zm9v"])
      expect(validateSecretValue("BINARY", value)).toBeUndefined();
    for (const value of ["Zg", "Zg=", "Zh==", "Z g==", "____", "===="])
      expect(validateSecretValue("BINARY", value)).toBe("invalid-base64");
  });
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

  it("разрешает действие только по authoritative nextActions активного секрета", () => {
    expect(canRuntimeSecretAction(secret, "REVEAL")).toBe(true);
    expect(
      canRuntimeSecretAction({ ...secret, nextActions: [] }, "REVEAL"),
    ).toBe(false);
    expect(
      canRuntimeSecretAction({ ...secret, state: "REVOKED" }, "REVEAL"),
    ).toBe(false);
  });
});
