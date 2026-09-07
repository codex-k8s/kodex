import { beforeEach, describe, expect, it, vi } from "vitest";
import type {
  ManagedConfiguration,
  ManagedConfigurationResult,
  IntegrationDefinition,
  RoleImageRecipe,
} from "@/shared/api/generated/openapi/types.gen";
const sdk = vi.hoisted(() => ({
  copyRoleImageConfiguration: vi.fn(),
  copyIntegrationDefinitionConfiguration: vi.fn(),
  archiveRoleImageConfiguration: vi.fn(),
  archiveIntegrationDefinitionConfiguration: vi.fn(),
  listManagedConfigurationHistory: vi.fn(),
}));
vi.mock("@/shared/api/generated/openapi/sdk.gen", () => sdk);
vi.mock("@/shared/api/client", () => ({
  requestSignal: () => new AbortController().signal,
}));
vi.mock("@/shared/api/mutation", async () => {
  const { unwrap } = await import("@/shared/api/problem");
  return {
    etag: (version: number) => `"${String(version)}"`,
    mutate: (
      request: (
        headers: Record<string, string>,
      ) => Parameters<typeof unwrap>[0],
      version: number,
    ) =>
      unwrap(
        request({
          "If-Match": `"${String(version)}"`,
          "Idempotency-Key": "fixture-key",
          "X-CSRF-Token": "fixture-csrf",
        }),
      ),
  };
});
import { copyConfiguration, archiveConfiguration } from "./lifecycle";
import {
  canArchiveConfiguration,
  integrationCopySource,
  managedCopySource,
  recipeCopySource,
} from "./copy-source";

const configuration: ManagedConfiguration = {
  ref: "mcfg_source",
  name: "Fixture",
  version: 7,
  kind: "INTEGRATION_DEFINITION",
  managedBy: "UI",
  archived: false,
  nextActions: ["COPY", "ARCHIVE"],
  source: "ui",
  sourceRevision: "1",
  updatedAt: "2026-09-07T00:00:00Z",
  currentRevision: {
    ref: "mrev_source",
    revision: 1,
    state: "PUBLISHED",
    content: "key: fixture",
    contentFormat: "YAML",
    digest: "a".repeat(64),
    createdAt: "2026-09-07T00:00:00Z",
    validationDiagnostics: [],
  },
};
const definition: IntegrationDefinition = {
  key: "fixture",
  name: "Fixture",
  description: "Fixture",
  category: "fixture",
  builtIn: true,
  available: true,
  capabilities: [],
  configurationFields: [],
  schemaVersion: "1",
  definitionVersion: "1.0.0",
  version: 5,
  nextActions: ["COPY"],
  origin: "SHIPPED",
  digest: "a".repeat(64),
  adapter: "SYNTHETIC_HTTP",
  adapterOwner: "integration-gateway",
  executionRoute: "MANAGED_MCP",
  adapterReadiness: "READY",
};
function response(data: unknown, version = 1) {
  return {
    data,
    response: new Response(null, { headers: { ETag: `"${String(version)}"` } }),
  };
}
function required<T>(value: T | undefined): T {
  if (value === undefined) throw new Error("Missing fixture value");
  return value;
}
function receipt(
  origin: "UI" | "GIT" | "SHIPPED" = "SHIPPED",
): ManagedConfigurationResult {
  return {
    configuration: {
      ...configuration,
      ref: "mcfg_copy",
      version: 1,
      copyProvenance: {
        origin,
        sourceRef: definition.key,
        sourceRevision: definition.definitionVersion,
        sourceVersion: definition.version,
        sourceDigest: definition.digest,
      },
    },
    revision: {
      ...required(configuration.currentRevision),
      ref: "mrev_copy",
      state: "DRAFT",
    },
  };
}
describe("CFG copy/archive consumer", () => {
  beforeEach(() => vi.clearAllMocks());
  it("SHIPPED copy отправляет точный selector и OCC без YAML/create", async () => {
    const source = required(integrationCopySource(definition));
    sdk.copyIntegrationDefinitionConfiguration.mockResolvedValue(
      response(receipt()),
    );
    await copyConfiguration(source, "Fixture", new AbortController().signal);
    expect(sdk.copyIntegrationDefinitionConfiguration).toHaveBeenCalledOnce();
    expect(
      sdk.copyIntegrationDefinitionConfiguration.mock.calls[0]?.[0],
    ).toMatchObject({
      body: {
        name: "Fixture",
        shipped: {
          key: "fixture",
          definitionVersion: "1.0.0",
          digest: definition.digest,
        },
      },
      headers: { "If-Match": '"5"' },
    });
    expect(definition.origin).toBe("SHIPPED");
  });
  it("COPY требует server action, но не право редактирования immutable исходника", () => {
    expect(
      integrationCopySource({ ...definition, nextActions: [] }),
    ).toBeUndefined();
    expect(
      managedCopySource({ ...configuration, nextActions: [] }),
    ).toBeUndefined();
    expect(
      managedCopySource({
        ...configuration,
        kind: "ROLE_IMAGE",
        projectRef: "project_fixture",
        sourceEditable: false,
        archived: true,
        managedBy: "GIT",
      }),
    ).toMatchObject({
      configurationRef: configuration.ref,
      revision: "mrev_source",
      origin: "GIT",
    });
    expect(
      canArchiveConfiguration({
        ...configuration,
        kind: "ROLE_IMAGE",
        sourceEditable: false,
      }),
    ).toBe(true);
    expect(canArchiveConfiguration({ ...configuration, archived: true })).toBe(
      false,
    );
    expect(
      canArchiveConfiguration({ ...configuration, managedBy: "GIT" }),
    ).toBe(false);
    expect(canArchiveConfiguration({ ...configuration, nextActions: [] })).toBe(
      false,
    );
  });
  it.each(["UI", "GIT"] as const)(
    "%s копируется по set selector и сохраняет provenance",
    async (managedBy) => {
      const source = required(
        managedCopySource({ ...configuration, managedBy }),
      );
      const result = receipt(managedBy);
      result.configuration.copyProvenance = {
        origin: managedBy,
        sourceRef: configuration.ref,
        sourceRevision: required(configuration.currentRevision).ref,
        sourceVersion: configuration.version,
        sourceDigest: required(configuration.currentRevision).digest,
      };
      sdk.copyIntegrationDefinitionConfiguration.mockResolvedValue(
        response(result),
      );
      await copyConfiguration(source, "Fixture", new AbortController().signal);
      expect(
        sdk.copyIntegrationDefinitionConfiguration.mock.calls[0]?.[0],
      ).toMatchObject({
        body: { name: "Fixture", configurationRef: configuration.ref },
        headers: { "If-Match": '"7"' },
      });
    },
  );
  it.each([
    [403, "PERMISSION_DENIED"],
    [409, "STATE_CONFLICT"],
    [412, "VERSION_MISMATCH"],
  ] as const)(
    "отказ %s сохраняется без нового intent",
    async (status, code) => {
      sdk.archiveIntegrationDefinitionConfiguration.mockResolvedValue({
        error: { status, code },
        response: new Response(null, { status }),
      });
      await expect(
        archiveConfiguration(configuration, new AbortController().signal),
      ).rejects.toMatchObject({ status, code });
      expect(
        sdk.archiveIntegrationDefinitionConfiguration,
      ).toHaveBeenCalledOnce();
      expect(sdk.listManagedConfigurationHistory).not.toHaveBeenCalled();
      expect(configuration.archived).toBe(false);
    },
  );
  it("recipe selector не подменяет set OCC и не выдаёт source permission", () => {
    const recipe = {
      ref: "recipe_fixture",
      version: 11,
      generation: 3,
      projectRef: "project_fixture",
      name: "Fixture",
      sourceAvailable: true,
      nextActions: ["COPY"],
      managedLineage: {
        managedBy: "SHIPPED",
        sourceRef: "baseline",
        sourceRevision: "release",
      },
    } as RoleImageRecipe;
    expect(recipeCopySource(recipe)).toMatchObject({
      recipeRef: recipe.ref,
      ref: recipe.ref,
      revision: "3",
      version: 11,
    });
    expect(
      recipeCopySource({ ...recipe, sourceAvailable: false }),
    ).toBeUndefined();
    expect(
      recipeCopySource({
        ...recipe,
        managedLineage: { ...required(recipe.managedLineage), managedBy: "UI" },
      }),
    ).toBeUndefined();
  });
  it.each(["sourceRef", "sourceRevision", "sourceDigest"] as const)(
    "отклоняет mismatched provenance %s",
    async (field) => {
      const result = receipt();
      required(result.configuration.copyProvenance)[field] = "wrong";
      sdk.copyIntegrationDefinitionConfiguration.mockResolvedValue(
        response(result),
      );
      await expect(
        copyConfiguration(
          required(integrationCopySource(definition)),
          "Fixture",
          new AbortController().signal,
        ),
      ).rejects.toThrow("receipt mismatch");
    },
  );
  it("UNKNOWN не вызывает повторную отправку", async () => {
    sdk.copyIntegrationDefinitionConfiguration.mockRejectedValue(
      new Error("Network unavailable"),
    );
    await expect(
      copyConfiguration(
        required(integrationCopySource(definition)),
        "Fixture",
        new AbortController().signal,
      ),
    ).rejects.toThrow();
    expect(sdk.copyIntegrationDefinitionConfiguration).toHaveBeenCalledOnce();
  });
  it("архив подтверждается history readback без изменения старой revision", async () => {
    const archived = {
      ...configuration,
      archived: true,
      version: 8,
      nextActions: [],
    };
    sdk.archiveIntegrationDefinitionConfiguration.mockResolvedValue(
      response({ configuration: archived }, 8),
    );
    sdk.listManagedConfigurationHistory.mockResolvedValue(
      response(
        {
          configuration: archived,
          items: [configuration.currentRevision],
          total: 1,
        },
        8,
      ),
    );
    expect(
      (await archiveConfiguration(configuration, new AbortController().signal))
        .configuration.archived,
    ).toBe(true);
    expect(
      sdk.archiveIntegrationDefinitionConfiguration.mock.calls[0]?.[0],
    ).toMatchObject({ headers: { "If-Match": '"7"' } });
    expect(configuration.currentRevision?.state).toBe("PUBLISHED");
    sdk.listManagedConfigurationHistory.mockResolvedValue(
      response({ configuration }, 7),
    );
    await expect(
      archiveConfiguration(configuration, new AbortController().signal),
    ).rejects.toThrow("readback mismatch");
  });
});
