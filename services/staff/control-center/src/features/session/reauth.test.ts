import { describe, expect, it } from "vitest";

import {
  consumeOidcIntent,
  createRuntimeSecretRevealIntent,
  parseRuntimeSecretRevealIntent,
  runtimeSecretRevealIntentStorageKey,
} from "./reauth";

function storage(initial?: string): Storage {
  const values = new Map<string, string>();
  if (initial !== undefined)
    values.set(runtimeSecretRevealIntentStorageKey, initial);
  return {
    clear: () => values.clear(),
    getItem: (key) => values.get(key) ?? null,
    key: (index) => [...values.keys()][index] ?? null,
    get length() {
      return values.size;
    },
    removeItem: (key) => values.delete(key),
    setItem: (key, value) => values.set(key, value),
  };
}

describe("runtime secret OIDC intent", () => {
  it("формирует только внутренний project secrets return path", () => {
    const intent = createRuntimeSecretRevealIntent(
      "project_sales",
      "secret_main",
      1_000,
    );

    expect(intent).toMatchObject({
      action: "reveal",
      issuedAt: 1_000,
      kind: "runtime-secret",
      projectRef: "project_sales",
      returnPath: "/projects/project_sales/secrets",
      secretRef: "secret_main",
      version: 1,
    });
  });

  it("закрыто отклоняет внешний return path и лишние поля", () => {
    const intent = createRuntimeSecretRevealIntent(
      "project_sales",
      "secret_main",
      1_000,
    );

    expect(() =>
      parseRuntimeSecretRevealIntent(
        { ...intent, returnPath: "https://attacker.example" },
        1_000,
      ),
    ).toThrow("invalid");
    expect(() =>
      parseRuntimeSecretRevealIntent({ ...intent, next: "/" }, 1_000),
    ).toThrow("shape");
  });

  it("потребляет совпавший state ровно один раз", () => {
    const intent = createRuntimeSecretRevealIntent(
      "project_sales",
      "secret_main",
      1_000,
    );
    const stateStorage = storage(JSON.stringify(intent));

    expect(consumeOidcIntent(intent, stateStorage, 1_000)).toEqual(intent);
    expect(() => consumeOidcIntent(intent, stateStorage, 1_000)).toThrow(
      "missing or already consumed",
    );
  });

  it("отклоняет протухший state", () => {
    const intent = createRuntimeSecretRevealIntent(
      "project_sales",
      "secret_main",
      1_000,
    );
    const stateStorage = storage(JSON.stringify(intent));

    expect(() =>
      consumeOidcIntent(intent, stateStorage, 5 * 60 * 1_000 + 1_001),
    ).toThrow("expired");
  });
});
