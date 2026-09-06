import { beforeEach, describe, expect, it, vi } from "vitest";

import type { ProviderAccount } from "./model";

const sdk = vi.hoisted(() => ({
  authorizeProviderAccountApiKey: vi.fn(),
  createProviderAccount: vi.fn(),
  deleteProviderAccount: vi.fn(),
  getProviderAccount: vi.fn(),
  listProviderAccounts: vi.fn(),
  listProviderDefinitions: vi.fn(),
  reauthorizeProviderAccountDeviceCode: vi.fn(),
  revokeProviderAccount: vi.fn(),
  setProviderAccountEnabled: vi.fn(),
  startProviderAccountDeviceAuthorization: vi.fn(),
  verifyProviderAccountDeviceAuthorization: vi.fn(),
}));
const mutateMock = vi.hoisted(() =>
  vi.fn(
    async (
      request: (headers: Record<string, string>) => Promise<{ data: unknown }>,
      version?: number,
    ) => ({
      data: (
        await request({
          "Idempotency-Key": "request-key",
          "If-Match": `"${String(version ?? 1)}"`,
          "X-CSRF-Token": "csrf-token",
        })
      ).data,
    }),
  ),
);

vi.mock("@/shared/api/generated/openapi/sdk.gen", () => sdk);
vi.mock("@/shared/api/client", () => ({
  requestSignal: (signal?: AbortSignal) => signal,
}));
vi.mock("@/shared/api/mutation", () => ({ mutate: mutateMock }));

import {
  authorizeProviderApiKey,
  createProviderAccount,
  deleteProviderAccountRecord,
  loadProviderAccounts,
  reauthorizeProviderDevice,
  setProviderAccountEnabled,
  verifyDeviceAuthorization,
} from "./api";

const account: ProviderAccount = {
  ref: "pacc_primary",
  version: 4,
  definitionKey: "openai-codex",
  name: "Основная запись",
  externalAccountMasked: "ow***er",
  state: "AUTHORIZED",
  enabled: true,
  ready: true,
  nextActions: ["DISABLE", "REVOKE", "TEST", "CONFIGURE_CREDENTIAL"],
  createdAt: "2026-08-30T08:00:00Z",
  updatedAt: "2026-08-30T08:00:00Z",
};

describe("provider API adapter", () => {
  beforeEach(() => vi.clearAllMocks());

  it("передаёт server-side search и cursor без ручной сборки URL", async () => {
    sdk.listProviderAccounts.mockResolvedValue({
      data: { items: [account], nextPageToken: "next", nextActions: [] },
      response: new Response(null, { status: 200 }),
    });
    const signal = new AbortController().signal;

    await loadProviderAccounts(
      "  основной  ",
      "cursor",
      signal,
      "openai-codex",
    );

    expect(sdk.listProviderAccounts).toHaveBeenCalledWith({
      query: {
        definitionKey: "openai-codex",
        pageSize: 40,
        query: "основной",
        pageToken: "cursor",
      },
      signal,
    });
  });

  it("создаёт account с definitionKey", async () => {
    sdk.createProviderAccount.mockResolvedValue({ data: account });

    await createProviderAccount({
      definitionKey: "openai-codex",
      name: "Основная запись",
    });

    expect(sdk.createProviderAccount).toHaveBeenCalledWith(
      expect.objectContaining({
        body: {
          definitionKey: "openai-codex",
          name: "Основная запись",
        },
        headers: {
          "Idempotency-Key": "request-key",
          "X-CSRF-Token": "csrf-token",
        },
      }),
    );
  });

  it("передаёт API key только в write-only body и не включает в readback", async () => {
    sdk.authorizeProviderAccountApiKey.mockResolvedValue({ data: account });

    const result = await authorizeProviderApiKey(account, "secret-api-key");

    expect(sdk.authorizeProviderAccountApiKey).toHaveBeenCalledWith(
      expect.objectContaining({
        path: { providerAccountRef: account.ref },
        body: { apiKey: "secret-api-key" },
        headers: {
          "Idempotency-Key": "request-key",
          "If-Match": '"4"',
          "X-CSRF-Token": "csrf-token",
        },
      }),
    );
    expect(JSON.stringify(result)).not.toContain("secret-api-key");
  });

  it("использует отдельный verify endpoint и PUT для enabled", async () => {
    sdk.verifyProviderAccountDeviceAuthorization.mockResolvedValue({
      data: account,
    });
    sdk.setProviderAccountEnabled.mockResolvedValue({
      data: { ...account, enabled: false },
    });

    await verifyDeviceAuthorization(account);
    await setProviderAccountEnabled(account, false);

    expect(sdk.verifyProviderAccountDeviceAuthorization).toHaveBeenCalledWith(
      expect.objectContaining({
        path: { providerAccountRef: account.ref },
      }),
    );
    expect(sdk.setProviderAccountEnabled).toHaveBeenCalledWith(
      expect.objectContaining({
        path: { providerAccountRef: account.ref },
        body: { enabled: false },
      }),
    );
  });

  it("использует отдельные reauthorize и API-key delete endpoints", async () => {
    sdk.reauthorizeProviderAccountDeviceCode.mockResolvedValue({
      data: account,
    });
    sdk.deleteProviderAccount.mockResolvedValue({ data: account });

    await reauthorizeProviderDevice(account);
    await deleteProviderAccountRecord(account);

    expect(sdk.reauthorizeProviderAccountDeviceCode).toHaveBeenCalledWith(
      expect.objectContaining({ path: { providerAccountRef: account.ref } }),
    );
    expect(sdk.deleteProviderAccount).toHaveBeenCalledWith(
      expect.objectContaining({ path: { providerAccountRef: account.ref } }),
    );
  });
});
