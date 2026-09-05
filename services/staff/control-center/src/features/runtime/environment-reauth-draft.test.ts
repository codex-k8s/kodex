import { describe, expect, it } from "vitest";

import {
  consumeRuntimeEnvironmentPolicyDraft,
  createRuntimeEnvironmentPolicyDraft,
  requiresRuntimeEnvironmentPolicyReauth,
  runtimeEnvironmentPolicyDraftStorageKey,
  storeRuntimeEnvironmentPolicyDraft,
} from "./environment-reauth-draft";
import { defaultRuntimeEnvironmentPolicy } from "./environment-form";

import type { RuntimeEnvironmentInput } from "@/shared/api/generated/openapi/types.gen";

function environmentInput(): RuntimeEnvironmentInput {
  return {
    name: "  Development  ",
    description: "  Local runtime  ",
    imageArtifactRef: "artifact_runtime_main",
    tools: [
      {
        name: "  GitHub CLI  ",
        command: "gh",
        description: "  GitHub operations  ",
        usageHint: "  Use typed credentials  ",
      },
    ],
    values: [],
    secretBindings: [{ name: "TOKEN", secretRef: "secret_one", revision: 7 }],
    policy: defaultRuntimeEnvironmentPolicy(),
  };
}

function storage(): Storage {
  const values = new Map<string, string>();
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

describe("runtime environment re-auth draft", () => {
  it("отличает fresh-auth 403 от общей 401 и прочих запретов", () => {
    expect(
      requiresRuntimeEnvironmentPolicyReauth({
        code: "FRESH_AUTHENTICATION_REQUIRED",
        status: 403,
      }),
    ).toBe(true);
    expect(
      requiresRuntimeEnvironmentPolicyReauth({
        code: "UNAUTHENTICATED",
        status: 401,
      }),
    ).toBe(false);
    expect(
      requiresRuntimeEnvironmentPolicyReauth({
        code: "FORBIDDEN",
        status: 403,
      }),
    ).toBe(false);
  });

  it("сохраняет нормализованный publish draft и потребляет его один раз", () => {
    const stateStorage = storage();
    const draft = createRuntimeEnvironmentPolicyDraft({
      environmentRef: "environment_main",
      expectedVersion: 7,
      form: environmentInput(),
      operation: "PUBLISH",
      projectRef: "project_sales",
      now: 1_000,
    });
    storeRuntimeEnvironmentPolicyDraft(draft, stateStorage);

    expect(
      consumeRuntimeEnvironmentPolicyDraft(
        stateStorage,
        {
          environmentRef: "environment_main",
          expectedVersion: 7,
          operation: "PUBLISH",
          projectRef: "project_sales",
        },
        1_000,
      ),
    ).toMatchObject({
      name: "Development",
      description: "Local runtime",
      tools: [
        {
          name: "GitHub CLI",
          description: "GitHub operations",
          usageHint: "Use typed credentials",
        },
      ],
    });
    expect(() =>
      consumeRuntimeEnvironmentPolicyDraft(
        stateStorage,
        {
          environmentRef: "environment_main",
          expectedVersion: 7,
          operation: "PUBLISH",
          projectRef: "project_sales",
        },
        1_000,
      ),
    ).toThrow("unavailable");
  });

  it("отклоняет изменившуюся expected version и удаляет draft", () => {
    const stateStorage = storage();
    storeRuntimeEnvironmentPolicyDraft(
      createRuntimeEnvironmentPolicyDraft({
        environmentRef: "environment_main",
        expectedVersion: 7,
        form: environmentInput(),
        operation: "PUBLISH",
        projectRef: "project_sales",
        now: 1_000,
      }),
      stateStorage,
    );

    expect(() =>
      consumeRuntimeEnvironmentPolicyDraft(
        stateStorage,
        {
          environmentRef: "environment_main",
          expectedVersion: 8,
          operation: "PUBLISH",
          projectRef: "project_sales",
        },
        1_000,
      ),
    ).toThrow("does not match");
    expect(
      stateStorage.getItem(runtimeEnvironmentPolicyDraftStorageKey),
    ).toBeNull();
  });

  it("отклоняет протухший create draft", () => {
    const stateStorage = storage();
    storeRuntimeEnvironmentPolicyDraft(
      createRuntimeEnvironmentPolicyDraft({
        form: environmentInput(),
        operation: "CREATE",
        projectRef: "project_sales",
        now: 1_000,
      }),
      stateStorage,
    );

    expect(() =>
      consumeRuntimeEnvironmentPolicyDraft(
        stateStorage,
        { operation: "CREATE", projectRef: "project_sales" },
        5 * 60 * 1_000 + 1_001,
      ),
    ).toThrow("does not match");
  });
});
