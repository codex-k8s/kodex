import { beforeEach, describe, expect, it, vi } from "vitest";

import type { IntegrationDefinition } from "@/shared/api/generated/openapi/types.gen";

const sdk = vi.hoisted(() => ({ list: vi.fn() }));
vi.mock("@/shared/api/generated/openapi/sdk.gen", () => ({
  listIntegrationDefinitions: sdk.list,
}));
vi.mock("@/shared/api/client", () => ({
  requestSignal: (signal: AbortSignal) => signal,
}));

import { loadExactIntegrationDefinition } from "./definition-lookup";

const definition = {
  nextActions: ["COPY"],
  connectionCount: 0,
  healthyConnectionCount: 0,
  version: 1,
  key: "github",
  name: "GitHub",
  description: "Repository access",
  category: "CODE",
  builtIn: true,
  available: true,
  capabilities: [],
  configurationFields: [],
  schemaVersion: "1",
  definitionVersion: "1",
  origin: "SHIPPED",
  digest: "a".repeat(64),
  adapter: "GITHUB",
  adapterOwner: "integration-gateway",
  executionRoute: "MANAGED_MCP",
  adapterReadiness: "READY",
} satisfies IntegrationDefinition;

function page(items: IntegrationDefinition[], nextPageToken = "") {
  return {
    data: { items, nextPageToken },
    response: new Response(null, { status: 200 }),
  };
}

describe("точный поиск схемы интеграции", () => {
  beforeEach(() => vi.resetAllMocks());

  it("не принимает неточную строку и читает следующую страницу", async () => {
    sdk.list
      .mockResolvedValueOnce(
        page([{ ...definition, key: "github-enterprise" }], "next"),
      )
      .mockResolvedValueOnce(page([definition]));
    const signal = new AbortController().signal;
    await expect(
      loadExactIntegrationDefinition("github", signal),
    ).resolves.toEqual(definition);
    expect(sdk.list).toHaveBeenNthCalledWith(2, {
      query: { query: "github", pageSize: 100, pageToken: "next" },
      signal,
    });
  });

  it("закрыто отклоняет недопустимый ключ и повторение cursor", async () => {
    const signal = new AbortController().signal;
    await expect(
      loadExactIntegrationDefinition("../secret", signal),
    ).rejects.toThrow();
    expect(sdk.list).not.toHaveBeenCalled();
    sdk.list.mockResolvedValue(page([], "loop"));
    await expect(
      loadExactIntegrationDefinition("github", signal),
    ).rejects.toThrow("cursor repeated");
    expect(sdk.list).toHaveBeenCalledTimes(2);
  });
});
