import { describe, expect, it, vi } from "vitest";

import {
  canConfigureCredential,
  executeConnectionSetup,
} from "@/features/integrations/connection-setup";
import type {
  IntegrationConnection,
  IntegrationDefinition,
} from "@/shared/api/generated/openapi/types.gen";

function definition(credentialSecretKey?: string): IntegrationDefinition {
  return {
    key: "github",
    name: "GitHub",
    description: "Репозитории и задачи",
    category: "DEVELOPMENT",
    builtIn: true,
    available: true,
    capabilities: [],
    configurationFields: [],
    schemaVersion: "v1",
    definitionVersion: "1.0.0",
    origin: "SHIPPED",
    digest: "sha256:definition",
    adapter: "GITHUB",
    ...(credentialSecretKey ? { credentialSecretKey } : {}),
  };
}

function connection(
  version: number,
  credentialsConfigured = false,
): IntegrationConnection {
  return {
    ref: "connection_github",
    version,
    definitionKey: "github",
    name: "Основная организация",
    state: credentialsConfigured ? "CONNECTED" : "NOT_CONNECTED",
    credentialsConfigured,
    credentialsHint: credentialsConfigured ? "••••••••" : "Не настроены",
    capabilities: [],
    grants: [],
    nextActions: (credentialsConfigured
      ? ["TEST"]
      : ["CONFIGURE_CREDENTIAL"]) as IntegrationConnection["nextActions"],
    definitionVersion: "1.0.0",
    definitionDigest: "sha256:definition",
    publicConfiguration: {},
  };
}

describe("двухфазная настройка подключения", () => {
  it("создаёт metadata один раз и повторяет только credential с тем же ключом", async () => {
    const rawCredential = "test-only-secret-value";
    const created = connection(4);
    const configured = connection(5, true);
    const create = vi.fn().mockResolvedValue(created);
    const configure = vi
      .fn()
      .mockRejectedValueOnce(new Error("temporary failure"))
      .mockResolvedValueOnce(configured);
    const dependencies = {
      create,
      configure,
      createIdempotencyKey: vi.fn(() => "credential-request-key"),
    };
    const input = {
      connection: {
        definitionKey: "github",
        name: "Основная организация",
      },
      credentialValue: rawCredential,
      requiresCredential: true,
    };

    const failed = await executeConnectionSetup(input, dependencies);
    expect(failed.status).toBe("CREDENTIAL_FAILED");
    if (failed.status !== "CREDENTIAL_FAILED") return;

    const completed = await executeConnectionSetup(
      { ...input, pending: failed.pending },
      dependencies,
    );

    expect(completed).toEqual({ status: "COMPLETE", connection: configured });
    expect(create).toHaveBeenCalledOnce();
    expect(configure).toHaveBeenCalledTimes(2);
    expect(configure).toHaveBeenNthCalledWith(
      1,
      { connectionRef: created.ref, version: created.version },
      rawCredential,
      "credential-request-key",
    );
    expect(configure).toHaveBeenNthCalledWith(
      2,
      { connectionRef: created.ref, version: created.version },
      rawCredential,
      "credential-request-key",
    );
    expect(JSON.stringify(failed)).not.toContain(rawCredential);
    expect(JSON.stringify(completed)).not.toContain(rawCredential);
  });

  it("не запускает credential-шаг для подключения без секрета", async () => {
    const created = connection(1, true);
    const create = vi.fn().mockResolvedValue(created);
    const configure = vi.fn();

    const result = await executeConnectionSetup(
      {
        connection: { definitionKey: "synthetic", name: "Тестовый сервис" },
        credentialValue: "",
        requiresCredential: false,
      },
      {
        create,
        configure,
        createIdempotencyKey: () => "unused",
      },
    );

    expect(result).toEqual({ status: "COMPLETE", connection: created });
    expect(configure).not.toHaveBeenCalled();
  });

  it("разрешает донастройку только по server-owned action", () => {
    const pending = connection(2);

    expect(canConfigureCredential(definition("github-token"), pending)).toBe(
      true,
    );
    expect(canConfigureCredential(definition(), pending)).toBe(false);
    expect(
      canConfigureCredential(definition("github-token"), {
        ...pending,
        nextActions: [],
      }),
    ).toBe(false);
    expect(
      canConfigureCredential(definition("github-token"), connection(3, true)),
    ).toBe(false);
  });
});
