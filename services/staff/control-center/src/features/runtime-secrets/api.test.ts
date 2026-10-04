import { initializeRuntimeOwnerFixture } from "@/test-utils/runtime-owner-fixture";
import { beforeEach, describe, expect, it, vi } from "vitest";

const sdk = vi.hoisted(() => ({
  createRuntimeSecret: vi.fn(),
  listRuntimeSecrets: vi.fn(),
  getRuntimeSecret: vi.fn(),
  listSystemRuntimeSecrets: vi.fn(),
  revealRuntimeSecret: vi.fn(),
  revokeRuntimeSecret: vi.fn(),
  rotateRuntimeSecret: vi.fn(),
}));
const mutation = vi.hoisted(() => ({
  csrfToken: vi.fn(() => "c".repeat(43)),
  idempotencyKey: vi.fn(() => "idem_1"),
  mutate: vi.fn(),
}));

vi.mock("@/shared/api/generated/openapi/sdk.gen", () => sdk);
vi.mock("@/shared/api/client", () => ({
  requestSignal: () => new AbortController().signal,
}));
vi.mock("@/shared/api/mutation", () => mutation);

import {
  createRuntimeSecret,
  loadRuntimeSecretPage,
  revealRuntimeSecret,
  revokeRuntimeSecret,
  rotateRuntimeSecret,
  readRuntimeSecret,
} from "./api";
import type { RuntimeSecret } from "./model";
import { usePlatformStore } from "@/features/platform/store";
import type { BootstrapState } from "@/shared/api/generated/openapi/types.gen";

const secret: RuntimeSecret = {
  scopeKind: "PROJECT",
  organizationRef: "org_synthetic",
  ref: "secret_main",
  version: 3,
  projectRef: "project_sales",
  name: "CRM_TOKEN",
  description: "Токен CRM",
  valueType: "STRING",
  state: "ACTIVE",
  currentRevision: 2,
  nextActions: ["ROTATE", "REVOKE", "REVEAL"],
  createdAt: "2026-08-29T08:00:00Z",
  updatedAt: "2026-08-29T09:00:00Z",
};

function response<T>(
  data: T,
  status = 200,
  headers?: HeadersInit,
): Promise<{ data: T; error: undefined; response: Response }> {
  return Promise.resolve({
    data,
    error: undefined,
    response: new Response(null, { status, headers }),
  });
}

describe("runtime secrets API adapter", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    sdk.getRuntimeSecret.mockImplementation(() => response(secret));
    mutation.mutate.mockImplementation(
      async (request: (headers: Record<string, string>) => Promise<unknown>) =>
        request({
          "Idempotency-Key": "idem_1",
          "If-Match": '"3"',
          "X-CSRF-Token": "c".repeat(43),
        }),
    );
  });
  it("не делает HTTP без текущего bootstrap owner", async () => {
    usePlatformStore().bootstrap = undefined;
    await expect(
      readRuntimeSecret(
        secret.ref,
        secret.projectRef,
        new AbortController().signal,
      ),
    ).rejects.toThrow("anchor");
    expect(sdk.getRuntimeSecret).not.toHaveBeenCalled();
  });
  it("закрыто отклоняет поздний metadata ответ после смены owner", async () => {
    let resolve!: (
      value: Awaited<ReturnType<typeof response<RuntimeSecret>>>,
    ) => void;
    sdk.getRuntimeSecret.mockReturnValue(
      new Promise((ready) => {
        resolve = ready;
      }),
    );
    const read = readRuntimeSecret(
      secret.ref,
      secret.projectRef,
      new AbortController().signal,
    );
    usePlatformStore().bootstrap = {
      organizationRef: "org_foreign",
    } as BootstrapState;
    resolve(await response({ ...secret, organizationRef: "org_foreign" }));
    await expect(read).rejects.toThrow("changed");
  });

  it("передаёт серверу project, поиск и cursor", async () => {
    sdk.listRuntimeSecrets.mockReturnValueOnce(
      response({ items: [secret], nextPageToken: "page_2" }),
    );
    await expect(
      loadRuntimeSecretPage("project_sales", " crm ", "page_1"),
    ).resolves.toEqual({ items: [secret], nextPageToken: "page_2" });
    expect(sdk.listRuntimeSecrets).toHaveBeenCalledWith(
      expect.objectContaining({
        path: { projectRef: "project_sales" },
        query: { pageSize: 20, query: "crm", pageToken: "page_1" },
      }),
    );
  });

  it("использует idempotency и OCC для полного lifecycle", async () => {
    sdk.createRuntimeSecret.mockReturnValueOnce(response(secret, 201));
    sdk.rotateRuntimeSecret.mockReturnValueOnce(
      response({ ...secret, version: 4 }),
    );
    sdk.revokeRuntimeSecret.mockReturnValueOnce(
      response({
        ...secret,
        version: 5,
        state: "REVOKED",
        nextActions: [],
      }),
    );

    await createRuntimeSecret("project_sales", {
      name: "CRM_TOKEN",
      description: "Токен CRM",
      valueType: "STRING",
      value: "one-time-create-value",
    });
    await rotateRuntimeSecret(secret, {
      valueType: "STRING",
      value: "one-time-rotate-value",
    });
    await revokeRuntimeSecret(secret);

    expect(mutation.mutate).toHaveBeenCalledTimes(3);
    const rotateRequest: unknown = sdk.rotateRuntimeSecret.mock.calls[0]?.[0];
    const revokeRequest: unknown = sdk.revokeRuntimeSecret.mock.calls[0]?.[0];
    expect(rotateRequest).toMatchObject({
      path: { secretRef: "secret_main" },
      headers: { "If-Match": '"3"' },
      body: { valueType: "STRING", value: "one-time-rotate-value" },
    });
    expect(revokeRequest).toMatchObject({
      path: { secretRef: "secret_main" },
      headers: { "If-Match": '"3"' },
    });
  });

  it("принимает reveal только с точным no-store и не делает его неявно", async () => {
    sdk.revealRuntimeSecret.mockReturnValueOnce(
      response(
        { value: "ephemeral-value", valueType: "STRING" as const },
        200,
        { "Cache-Control": "no-store" },
      ),
    );
    await expect(
      revealRuntimeSecret("secret_main", "project_sales", "org_synthetic"),
    ).resolves.toEqual({
      value: "ephemeral-value",
      valueType: "STRING",
    });
    expect(sdk.revealRuntimeSecret).toHaveBeenCalledWith(
      expect.objectContaining({
        cache: "no-store",
        headers: {
          "Idempotency-Key": "idem_1",
          "X-CSRF-Token": "c".repeat(43),
          "X-Kodex-Project-ID": "project_sales",
        },
      }),
    );
  });

  it("закрыто отклоняет reveal без server no-store", async () => {
    const payload = { value: "must-be-cleared", valueType: "STRING" as const };
    sdk.revealRuntimeSecret.mockReturnValueOnce(response(payload));
    await expect(
      revealRuntimeSecret("secret_main", "project_sales", "org_synthetic"),
    ).rejects.toMatchObject({
      code: "SECRET_REVEAL_CACHE_POLICY_INVALID",
    });
    expect(payload.value).toBe("");
  });

  it("не передаёт project header при точном организационном reveal", async () => {
    sdk.getRuntimeSecret.mockReturnValueOnce(
      response({ ...secret, scopeKind: "ORGANIZATION", projectRef: "" }),
    );
    sdk.revealRuntimeSecret.mockReturnValueOnce(
      response({ value: "ephemeral-value", valueType: "STRING" }, 200, {
        "Cache-Control": "no-store",
      }),
    );
    await revealRuntimeSecret(
      "secret_main",
      { kind: "ORGANIZATION", organizationRef: "org_synthetic" },
      "org_synthetic",
    );
    const request: unknown = sdk.revealRuntimeSecret.mock.calls[0]?.[0];
    expect(request).not.toHaveProperty("headers.X-Kodex-Project-ID");
    expect(sdk.getRuntimeSecret).toHaveBeenCalledWith(
      expect.objectContaining({
        cache: "no-store",
        path: { secretRef: "secret_main" },
      }),
    );
  });

  it.each([
    { organizationRef: "org_foreign" },
    { projectRef: "project_foreign" },
    { scopeKind: "ORGANIZATION", projectRef: "" },
    { ref: "secret_foreign" },
  ])(
    "не раскрывает секрет при несовпадении authoritative metadata: %j",
    async (mismatch) => {
      sdk.getRuntimeSecret.mockReturnValueOnce(
        response({ ...secret, ...mismatch }),
      );
      await expect(
        revealRuntimeSecret("secret_main", "project_sales", "org_synthetic"),
      ).rejects.toThrow();
      expect(sdk.revealRuntimeSecret).not.toHaveBeenCalled();
    },
  );
});

beforeEach(() => initializeRuntimeOwnerFixture("org_synthetic"));
