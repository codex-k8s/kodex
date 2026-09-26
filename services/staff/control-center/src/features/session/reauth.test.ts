import { describe, expect, it } from "vitest";

import {
  consumeOidcIntent,
  createEmailReconciliationIntent,
  parseEmailReconciliationIntent,
  consumeRuntimeEnvironmentPolicyReauthCompletion,
  createRuntimeEnvironmentPolicyIntent,
  createRuntimeSecretRevealIntent,
  createRuntimeSecretDraftIntent,
  oidcReauthIntentStorageKey,
  parseRuntimeEnvironmentPolicyIntent,
  parseRuntimeSecretRevealIntent,
  parseRuntimeSecretDraftIntent,
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
  it("возвращает форму секрета после fresh re-auth без значения в state", () => {
    const intent = createRuntimeSecretDraftIntent(
      "project_sales",
      "create",
      undefined,
      1000,
    );
    expect(intent.returnPath).toBe(
      "/projects/project_sales/secrets?assistantCreateSecret=1",
    );
    expect(consumeOidcIntent(intent, pendingStorage(intent), 1100)).toEqual(
      intent,
    );
    expect(JSON.stringify(intent)).not.toContain("secret-value");
    expect(() =>
      parseRuntimeSecretDraftIntent(
        { ...intent, returnPath: "https://attacker.example" },
        1100,
      ),
    ).toThrow();
    expect(() =>
      parseRuntimeSecretDraftIntent(
        { ...intent, targetRef: "secret_other" },
        1100,
      ),
    ).toThrow();
    expect(() =>
      createRuntimeSecretDraftIntent("project_sales", "draft"),
    ).toThrow();
  });
  it("возвращает защищённую форму внутрь помощника только в текущем проекте", () => {
    const create = createRuntimeSecretDraftIntent(
      "project_sales",
      "create",
      undefined,
      1000,
      "assistant",
    );
    expect(create.returnPath).toBe(
      "/projects/project_sales?assistantCreateSecret=1",
    );
    expect(consumeOidcIntent(create, pendingStorage(create), 1100)).toEqual(
      create,
    );
    const draft = createRuntimeSecretDraftIntent(
      "project_sales",
      "draft",
      "draft_synthetic",
      1000,
      "assistant",
    );
    expect(draft.returnPath).toBe(
      "/projects/project_sales?assistantSecretDraftRef=draft_synthetic",
    );
    expect(consumeOidcIntent(draft, pendingStorage(draft), 1100)).toEqual(
      draft,
    );
    expect(() =>
      parseRuntimeSecretDraftIntent(
        {
          ...draft,
          returnPath:
            "/projects/project_other?assistantSecretDraftRef=draft_synthetic",
        },
        1100,
      ),
    ).toThrow();
    expect(() =>
      createRuntimeSecretDraftIntent(
        "project_sales",
        "secret",
        "secret_synthetic",
        1000,
        "assistant",
      ),
    ).toThrow();

    const contextual = createRuntimeSecretDraftIntent(
      "project_sales",
      "create",
      undefined,
      1000,
      "assistant",
      "/projects/project_sales/agents/agent_main?tab=runtime&assistantForm=1",
    );
    expect(contextual.returnPath).toBe(
      "/projects/project_sales/agents/agent_main?tab=runtime&assistantCreateSecret=1",
    );
    expect(
      consumeOidcIntent(contextual, pendingStorage(contextual), 1100),
    ).toEqual(contextual);
    expect(() =>
      createRuntimeSecretDraftIntent(
        "project_sales",
        "create",
        undefined,
        1000,
        "assistant",
        "/projects/project_other/agents/agent_main",
      ),
    ).toThrow();
    expect(() =>
      parseRuntimeSecretDraftIntent(
        {
          ...contextual,
          returnPath: "https://attacker.example/projects/project_sales",
        },
        1100,
      ),
    ).toThrow();
  });
  it("связывает email state с exact receipt без project и secret", () => {
    const intent = createEmailReconciliationIntent(
      {
        receiptRef: "receipt_synthetic",
        receiptVersion: 7,
        receiptDigest: "a".repeat(64),
        connectionRef: "connection_synthetic",
        invocationRef: "invocation_synthetic",
      },
      1000,
    );
    expect(consumeOidcIntent(intent, pendingStorage(intent), 1100)).toEqual(
      intent,
    );
    expect(intent.returnPath).toBe(
      "/integrations?connectionRef=connection_synthetic&invocationRef=invocation_synthetic",
    );
    expect(() =>
      parseEmailReconciliationIntent(
        { ...intent, projectRef: "project_synthetic" },
        1100,
      ),
    ).toThrow();
    expect(() =>
      parseEmailReconciliationIntent(
        { ...intent, secretRef: "secret_synthetic" },
        1100,
      ),
    ).toThrow();
    expect(() =>
      consumeOidcIntent(
        { ...intent, receiptVersion: 8 },
        pendingStorage(intent),
        1100,
      ),
    ).toThrow();
    expect(() => parseEmailReconciliationIntent(intent, 301001)).toThrow();
  });
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
  it("возвращает protected policy editor в чат только по точному маршруту", () => {
    const intent = createRuntimeEnvironmentPolicyIntent(
      "project_sales",
      "PUBLISH",
      "environment_main",
      1_000,
      "assistant",
    );
    expect(intent.returnPath).toBe(
      "/projects/project_sales/environments/environment_main?assistantForm=1",
    );
    expect(consumeOidcIntent(intent, pendingStorage(intent), 1_100)).toEqual(
      intent,
    );
    expect(() =>
      parseRuntimeEnvironmentPolicyIntent(
        {
          ...intent,
          returnPath: "/projects/project_sales/environments/environment_main",
        },
        1_100,
      ),
    ).toThrow();
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
  it("не переносит assistant completion в обычный редактор", () => {
    const intent = createRuntimeEnvironmentPolicyIntent(
      "project_sales",
      "PUBLISH",
      "environment_main",
      1_000,
      "assistant",
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
    ).toThrow();
    recordRuntimeEnvironmentPolicyReauthCompletion(intent, stateStorage, 1_000);
    expect(
      consumeRuntimeEnvironmentPolicyReauthCompletion(
        stateStorage,
        {
          environmentRef: "environment_main",
          operation: "PUBLISH",
          projectRef: "project_sales",
          surface: "assistant",
        },
        1_000,
      ),
    ).toBe(true);
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
