import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { TelegramSubscription } from "@/shared/api/generated/openapi/types.gen";

interface Request {
  path?: { projectRef: string };
  body?: unknown;
  headers?: Record<string, string>;
  signal?: AbortSignal;
  query?: unknown;
}
const sdk = vi.hoisted(() => {
  const method = () =>
    vi.fn<
      (request: Request) => Promise<{ data: unknown; response: Response }>
    >();
  return {
    getTelegramSubscription: method(),
    configureTelegramSubscription: method(),
    listInteractionDeliveries: method(),
    listIntegrationConnections: method(),
  };
});
vi.mock("@/shared/api/generated/openapi/sdk.gen", () => sdk);
vi.mock("@/shared/api/client", () => ({
  requestSignal: (signal: AbortSignal) => signal,
}));
import {
  readSubscription,
  saveSubscription,
  readDeliveries,
  readConnections,
} from "./api";
const current: TelegramSubscription = {
  ref: "tgs_fixture01",
  projectRef: "prj_fixture01",
  connectionRef: "int_fixture01",
  version: 2,
  enabled: true,
  canManage: true,
  state: "ACTIVE",
  eventTypes: ["FAILED"],
  scopeDigest: "a".repeat(64),
  botId: 12345678,
  chatDisplay: "…1234",
  topicDisplay: "NONE",
  expiresAt: "2026-11-15T12:00:00Z",
  updatedAt: "2026-11-14T12:00:00Z",
  maxEffects: 100,
  usedEffects: 0,
};
beforeEach(() => {
  vi.resetAllMocks();
  vi.stubGlobal("document", { cookie: `__Host-kodex-csrf=${"c".repeat(43)}` });
});
afterEach(() => vi.unstubAllGlobals());
describe("Telegram HTTP adapter", () => {
  it("binds single explicit command to project/version/CSRF and never sends destination or actor selectors", async () => {
    sdk.configureTelegramSubscription.mockResolvedValue({
      data: current,
      response: new Response(),
    });
    const signal = new AbortController().signal;
    const input = {
      enabled: true as const,
      connectionRef: current.connectionRef,
      eventTypes: ["FAILED" as const],
      durationSeconds: 86400,
      maxEffects: 100,
    };
    await expect(
      saveSubscription(current.projectRef, input, 1, signal),
    ).resolves.toEqual(current);
    expect(sdk.configureTelegramSubscription).toHaveBeenCalledOnce();
    const call = sdk.configureTelegramSubscription.mock.calls[0]?.[0];
    expect(call?.path).toEqual({ projectRef: current.projectRef });
    expect(call?.body).toEqual(input);
    expect(call?.signal).toBe(signal);
    expect(call?.headers?.["If-Match"]).toBe('"1"');
    expect(call?.headers?.["X-CSRF-Token"]).toBe("c".repeat(43));
    expect(call?.headers?.["Idempotency-Key"]).toBeTruthy();
    sdk.configureTelegramSubscription.mockRejectedValue(
      new Error("Synthetic connection closed"),
    );
    await expect(
      saveSubscription(current.projectRef, { enabled: false }, 2, signal),
    ).rejects.toThrow();
    expect(sdk.configureTelegramSubscription).toHaveBeenCalledTimes(2);
  });
  it("preserves unconfigured false and rejects mismatched eligibility, partial receipts and unbounded history", async () => {
    const signal = new AbortController().signal;
    sdk.getTelegramSubscription.mockResolvedValue({
      data: { canManage: false },
      response: new Response(),
    });
    await expect(readSubscription(current.projectRef, signal)).resolves.toEqual(
      { canManage: false, subscription: undefined },
    );
    sdk.getTelegramSubscription.mockResolvedValue({
      data: { canManage: false, subscription: current },
      response: new Response(),
    });
    await expect(
      readSubscription(current.projectRef, signal),
    ).rejects.toThrow();
    sdk.configureTelegramSubscription.mockResolvedValue({
      data: { ...current, projectRef: "prj_foreign01" },
      response: new Response(),
    });
    await expect(
      saveSubscription(current.projectRef, { enabled: false }, 1, signal),
    ).rejects.toThrow();
    sdk.listInteractionDeliveries.mockResolvedValue({
      data: { items: [], nextPageToken: "x".repeat(513) },
      response: new Response(),
    });
    await expect(
      readDeliveries(current.projectRef, undefined, signal),
    ).rejects.toThrow();
  });
  it("offers only checked service Telegram connections and keeps cursor bounded", async () => {
    const base = {
      ref: "int_fixture01",
      name: "Fixture",
      definitionKey: "telegram",
      nextActions: ["EDIT"],
      credentialPolicy: "PROJECT_SHARED",
      state: "CONNECTED",
      credentialsConfigured: true,
    };
    sdk.listIntegrationConnections.mockResolvedValue({
      data: {
        items: [
          base,
          {
            ...base,
            ref: "int_personal01",
            credentialPolicy: "INITIATOR_REQUIRED",
          },
          { ...base, ref: "int_unchecked01", state: "NOT_CONNECTED" },
        ],
        nextPageToken: "page-fixture",
      },
      response: new Response(),
    });
    const signal = new AbortController().signal;
    await expect(readConnections(undefined, signal)).resolves.toEqual({
      items: [{ ref: base.ref, name: base.name }],
      nextPageToken: "page-fixture",
    });
    expect(sdk.listIntegrationConnections.mock.calls[0]?.[0].query).toEqual({
      definitionKey: "telegram",
      pageSize: 20,
      pageToken: undefined,
    });
  });
});
