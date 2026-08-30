import { describe, expect, it } from "vitest";

import {
  consumeOidcIntent,
  consumeRuntimeEnvironmentPolicyReauthCompletion,
  createRuntimeEnvironmentPolicyIntent,
  createRuntimeSecretRevealIntent,
  oidcReauthIntentStorageKey,
  parseRuntimeEnvironmentPolicyIntent,
  parseRuntimeSecretRevealIntent,
  recordRuntimeEnvironmentPolicyReauthCompletion,
} from "./reauth";

function storage(initial?: Record<string, string>): Storage {
  const values = new Map<string, string>(Object.entries(initial ?? {}));
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

function pendingStorage(intent: unknown): Storage {
  return storage({
    [oidcReauthIntentStorageKey]: JSON.stringify(intent),
  });
}

describe("OIDC re-auth intents", () => {
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

  it("формирует разные фиксированные пути create и publish окружения", () => {
    expect(
      createRuntimeEnvironmentPolicyIntent(
        "project_sales",
        "CREATE",
        undefined,
        1_000,
      ),
    ).toMatchObject({
      issuedAt: 1_000,
      kind: "runtime-environment-policy",
      operation: "CREATE",
      projectRef: "project_sales",
      returnPath: "/projects/project_sales/environments/new",
      version: 1,
    });
    expect(
      createRuntimeEnvironmentPolicyIntent(
        "project_sales",
        "PUBLISH",
        "environment_main",
        1_000,
      ),
    ).toMatchObject({
      environmentRef: "environment_main",
      operation: "PUBLISH",
      returnPath: "/projects/project_sales/environments/environment_main",
    });
  });

  it("закрыто отклоняет несовместимые operation и environmentRef", () => {
    expect(() =>
      createRuntimeEnvironmentPolicyIntent(
        "project_sales",
        "CREATE",
        "environment_main",
      ),
    ).toThrow("operation is invalid");
    expect(() =>
      createRuntimeEnvironmentPolicyIntent("project_sales", "PUBLISH"),
    ).toThrow("operation is invalid");
  });

  it("закрыто отклоняет внешний return path и лишние поля", () => {
    const secretIntent = createRuntimeSecretRevealIntent(
      "project_sales",
      "secret_main",
      1_000,
    );
    const environmentIntent = createRuntimeEnvironmentPolicyIntent(
      "project_sales",
      "PUBLISH",
      "environment_main",
      1_000,
    );

    expect(() =>
      parseRuntimeSecretRevealIntent(
        { ...secretIntent, returnPath: "https://attacker.example" },
        1_000,
      ),
    ).toThrow("invalid");
    expect(() =>
      parseRuntimeEnvironmentPolicyIntent(
        { ...environmentIntent, next: "/" },
        1_000,
      ),
    ).toThrow("shape");
  });

  it("потребляет совпавший environment state ровно один раз", () => {
    const intent = createRuntimeEnvironmentPolicyIntent(
      "project_sales",
      "PUBLISH",
      "environment_main",
      1_000,
    );
    const stateStorage = pendingStorage(intent);

    expect(consumeOidcIntent(intent, stateStorage, 1_000)).toEqual(intent);
    expect(() => consumeOidcIntent(intent, stateStorage, 1_000)).toThrow(
      "missing or already consumed",
    );
  });

  it("отклоняет mismatch и протухший state", () => {
    const intent = createRuntimeEnvironmentPolicyIntent(
      "project_sales",
      "PUBLISH",
      "environment_main",
      1_000,
    );
    const changed = {
      ...intent,
      environmentRef: "environment_other",
      returnPath: "/projects/project_sales/environments/environment_other",
    };

    expect(() =>
      consumeOidcIntent(changed, pendingStorage(intent), 1_000),
    ).toThrow("does not match");
    expect(() =>
      consumeOidcIntent(intent, pendingStorage(intent), 5 * 60 * 1_000 + 1_001),
    ).toThrow("expired");
  });

  it("потребляет completion marker только в совпавшем редакторе", () => {
    const intent = createRuntimeEnvironmentPolicyIntent(
      "project_sales",
      "PUBLISH",
      "environment_main",
      1_000,
    );
    const stateStorage = storage();
    recordRuntimeEnvironmentPolicyReauthCompletion(intent, stateStorage, 1_000);

    expect(
      consumeRuntimeEnvironmentPolicyReauthCompletion(
        stateStorage,
        {
          environmentRef: "environment_main",
          operation: "PUBLISH",
          projectRef: "project_sales",
        },
        1_000,
      ),
    ).toBe(true);
    expect(
      consumeRuntimeEnvironmentPolicyReauthCompletion(
        stateStorage,
        {
          environmentRef: "environment_main",
          operation: "PUBLISH",
          projectRef: "project_sales",
        },
        1_000,
      ),
    ).toBe(false);
  });

  it("удаляет completion marker при route mismatch", () => {
    const intent = createRuntimeEnvironmentPolicyIntent(
      "project_sales",
      "CREATE",
      undefined,
      1_000,
    );
    const stateStorage = storage();
    recordRuntimeEnvironmentPolicyReauthCompletion(intent, stateStorage, 1_000);

    expect(() =>
      consumeRuntimeEnvironmentPolicyReauthCompletion(
        stateStorage,
        {
          environmentRef: "environment_main",
          operation: "PUBLISH",
          projectRef: "project_sales",
        },
        1_000,
      ),
    ).toThrow("does not match");
    expect(
      consumeRuntimeEnvironmentPolicyReauthCompletion(
        stateStorage,
        { operation: "CREATE", projectRef: "project_sales" },
        1_000,
      ),
    ).toBe(false);
  });
});
