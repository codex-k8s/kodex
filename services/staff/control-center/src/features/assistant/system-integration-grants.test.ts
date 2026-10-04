import { beforeEach, describe, expect, it, vi } from "vitest";
import type {
  IntegrationConnection,
  SystemAssistantIntegrationGrantCandidates,
  SystemAssistantIntegrationGrantCandidate,
} from "@/shared/api/generated/openapi/types.gen";
const sdk = vi.hoisted(() => ({
  changeSystemAssistantIntegrationGrant: vi.fn(),
  getSystemAssistantIntegrationGrantCandidates: vi.fn(),
  listIntegrationConnections: vi.fn(),
}));
vi.mock("@/shared/api/generated/openapi/sdk.gen", () => sdk);
vi.mock("@/shared/api/client", () => ({
  requestSignal: (signal: AbortSignal) => signal,
}));
vi.mock("@/shared/api/mutation", () => ({
  mutate: async (
    request: (headers: Record<string, string>) => Promise<unknown>,
    version: number,
  ) =>
    request({
      "If-Match": `"${String(version)}"`,
      "Idempotency-Key": "synthetic",
      "X-CSRF-Token": "synthetic",
    }),
}));
import {
  assertSystemGrantCandidates,
  readSystemGrantCandidates,
  saveSystemGrant,
  selectedSystemGrantPolicy,
  validSystemGrantSelection,
} from "./system-integration-grants";

const owner = {
  organizationRef: "organization_test",
  assistantRef: "assistant_test",
  assistantVersion: 3,
};
const connection: IntegrationConnection = {
  ref: "connection_test",
  version: 5,
  definitionKey: "github",
  definitionVersion: "1.0",
  definitionDigest: "a".repeat(64),
  name: "GitHub",
  state: "CONNECTED",
  credentialsConfigured: true,
  credentialsHint: "",
  capabilities: [],
  grants: [],
  nextActions: [],
  publicConfiguration: {},
};
const candidate: SystemAssistantIntegrationGrantCandidate = {
  capability: {
    key: "create",
    name: "Создание",
    description: "",
    risk: "WRITE",
    approvalRequired: true,
    operation: "CREATE",
    resourceKind: "GITHUB_REPOSITORY",
    inputFields: [],
    inputSchema:
      '{"type":"object","properties":{"repository":{"type":"string"}}}',
    approvalPolicy: "HUMAN_EACH_EFFECT",
    allowedApprovalPolicies: ["HUMAN_EACH_EFFECT", "HUMAN_SCOPED"],
  },
  grantable: true,
  reason: "READY",
  currentGrantVersion: 0,
  currentGrantEnabled: false,
  currentApprovalScopePaths: [],
};
const page: SystemAssistantIntegrationGrantCandidates = {
  scopeKind: "ORGANIZATION",
  ...owner,
  connectionRef: connection.ref,
  connectionVersion: connection.version,
  definitionVersion: connection.definitionVersion,
  definitionDigest: connection.definitionDigest,
  items: [candidate],
  total: 1,
  nextPageToken: "",
};

describe("grant общесистемного помощника", () => {
  beforeEach(() => vi.clearAllMocks());
  it("принимает только точную организацию, помощника и опубликованный connection tuple", () => {
    expect(() =>
      assertSystemGrantCandidates(page, owner, connection),
    ).not.toThrow();
    for (const changed of [
      { ...page, scopeKind: "PROJECT" },
      { ...page, organizationRef: "foreign" },
      { ...page, assistantRef: "foreign" },
      { ...page, assistantVersion: 4 },
      { ...page, connectionVersion: 4 },
      { ...page, definitionDigest: "b".repeat(64) },
    ])
      expect(() =>
        assertSystemGrantCandidates(
          changed as SystemAssistantIntegrationGrantCandidates,
          owner,
          connection,
        ),
      ).toThrow("Invalid system assistant grant candidate pins");
  });
  it("отклоняет несовместимые current grant и незнакомый reason", () => {
    for (const changed of [
      { ...candidate, currentGrantVersion: 1 },
      { ...candidate, currentGrantEnabled: true },
      { ...candidate, reason: "UNKNOWN" },
      { ...candidate, reason: "GRANT_UNAVAILABLE" },
      { ...candidate, currentApprovalScopePaths: ["/repository"] },
    ])
      expect(() =>
        assertSystemGrantCandidates(
          {
            ...page,
            items: [changed as SystemAssistantIntegrationGrantCandidate],
          },
          owner,
          connection,
        ),
      ).toThrow("Invalid system assistant grant candidate");
  });
  it("явно выбирает catalog default или разрешённую текущую policy", () => {
    expect(selectedSystemGrantPolicy(candidate)).toBe("HUMAN_EACH_EFFECT");
    const current = {
      ...candidate,
      currentGrantRef: "grant_test",
      currentGrantVersion: 1,
      currentApprovalPolicy: "HUMAN_SCOPED" as const,
      currentApprovalScopePaths: ["/repository"],
    };
    expect(selectedSystemGrantPolicy(current)).toBe("HUMAN_SCOPED");
    expect(
      selectedSystemGrantPolicy({ ...current, currentApprovalPolicy: "NONE" }),
    ).toBeUndefined();
    expect(validSystemGrantSelection(candidate, undefined, [])).toBe(false);
    expect(validSystemGrantSelection(candidate, "NONE", [])).toBe(false);
    expect(validSystemGrantSelection(candidate, "HUMAN_EACH_EFFECT", [])).toBe(
      true,
    );
    expect(
      validSystemGrantSelection(candidate, "HUMAN_SCOPED", ["/repository"]),
    ).toBe(true);
    expect(
      validSystemGrantSelection(candidate, "HUMAN_SCOPED", ["/unknown"]),
    ).toBe(false);
    expect(
      validSystemGrantSelection(
        { ...candidate, grantable: false },
        "HUMAN_EACH_EFFECT",
        [],
      ),
    ).toBe(false);
  });
  it("читает capabilities без agent, profile и project payload", async () => {
    sdk.getSystemAssistantIntegrationGrantCandidates.mockResolvedValue({
      data: page,
      response: new Response(),
    });
    await expect(
      readSystemGrantCandidates(
        owner,
        connection,
        "",
        undefined,
        new AbortController().signal,
      ),
    ).resolves.toEqual(page);
    expect(
      sdk.getSystemAssistantIntegrationGrantCandidates,
    ).toHaveBeenCalledWith(
      expect.objectContaining({
        query: {
          connectionRef: connection.ref,
          query: "",
          pageToken: undefined,
          pageSize: 40,
        },
        cache: "no-store",
      }),
    );
  });
  it("передаёт selected policy и connection If-Match в специализированную команду", async () => {
    const result = { ...connection, version: 6 };
    sdk.changeSystemAssistantIntegrationGrant.mockResolvedValue({
      data: result,
    });
    const input = {
      connectionRef: connection.ref,
      capabilityKey: candidate.capability.key,
      enabled: true,
      approvalPolicy: "HUMAN_EACH_EFFECT" as const,
    };
    await expect(
      saveSystemGrant(connection, input, new AbortController().signal),
    ).resolves.toEqual(result);
    expect(sdk.changeSystemAssistantIntegrationGrant).toHaveBeenCalledWith(
      expect.objectContaining({
        body: input,
        headers: {
          "If-Match": '"5"',
          "Idempotency-Key": "synthetic",
          "X-CSRF-Token": "synthetic",
        },
      }),
    );
  });
  it("не принимает чужую квитанцию и поздний результат после owner reset", async () => {
    sdk.changeSystemAssistantIntegrationGrant.mockResolvedValue({
      data: { ...connection, ref: "foreign" },
    });
    const input = {
      connectionRef: connection.ref,
      capabilityKey: candidate.capability.key,
      enabled: true,
      approvalPolicy: "HUMAN_EACH_EFFECT" as const,
    };
    await expect(
      saveSystemGrant(connection, input, new AbortController().signal),
    ).rejects.toThrow("System assistant grant receipt mismatch");
    const controller = new AbortController();
    sdk.changeSystemAssistantIntegrationGrant.mockImplementation(() => {
      controller.abort();
      return Promise.resolve({ data: connection });
    });
    await expect(
      saveSystemGrant(connection, input, controller.signal),
    ).rejects.toMatchObject({ name: "AbortError" });
  });
});
