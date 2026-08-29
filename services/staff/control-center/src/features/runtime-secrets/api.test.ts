import { beforeEach, describe, expect, it, vi } from "vitest";

const client = vi.hoisted(() => ({
  delete: vi.fn(),
  get: vi.fn(),
  post: vi.fn(),
}));
const mutation = vi.hoisted(() => ({
  csrfToken: vi.fn(() => "c".repeat(43)),
  etag: vi.fn((version: number) => `"${String(version)}"`),
  idempotencyKey: vi.fn(() => "idem_1"),
  mutate: vi.fn(),
}));

vi.mock("@/shared/api/generated/openapi/client.gen", () => ({ client }));
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
} from "./api";
import type { RuntimeSecret } from "./model";

const secret: RuntimeSecret = {
  ref: "secret_main",
  version: 3,
  projectRef: "project_sales",
  name: "CRM_TOKEN",
  description: "Токен CRM",
  valueType: "STRING",
  state: "ACTIVE",
  currentRevision: 2,
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
    mutation.mutate.mockImplementation(
      async (request: (headers: Record<string, string>) => Promise<unknown>) =>
        request({
          "Idempotency-Key": "idem_1",
          "If-Match": '"3"',
          "X-CSRF-Token": "c".repeat(43),
        }),
    );
  });

  it("передаёт серверу project, поиск и cursor", async () => {
    client.get.mockReturnValueOnce(
      response({ items: [secret], nextPageToken: "page_2" }),
    );
    await expect(
      loadRuntimeSecretPage("project_sales", " crm ", "page_1"),
    ).resolves.toEqual({ items: [secret], nextPageToken: "page_2" });
    expect(client.get).toHaveBeenCalledWith(
      expect.objectContaining({
        url: "/api/v1/projects/{projectRef}/runtime-secrets",
        path: { projectRef: "project_sales" },
        query: { pageSize: 40, query: "crm", pageToken: "page_1" },
      }),
    );
  });

  it("использует idempotency и OCC для полного lifecycle", async () => {
    client.post
      .mockReturnValueOnce(response(secret, 201))
      .mockReturnValueOnce(response({ ...secret, version: 4 }));
    client.delete.mockReturnValueOnce(
      response({ ...secret, version: 5, state: "REVOKED" }),
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
    const rotateRequest: unknown = client.post.mock.calls[1]?.[0];
    const revokeRequest: unknown = client.delete.mock.calls[0]?.[0];
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
    client.post.mockReturnValueOnce(
      response(
        { value: "ephemeral-value", valueType: "STRING" as const },
        200,
        { "Cache-Control": "no-store" },
      ),
    );
    await expect(revealRuntimeSecret("secret_main")).resolves.toEqual({
      value: "ephemeral-value",
      valueType: "STRING",
    });
    expect(client.post).toHaveBeenCalledWith(
      expect.objectContaining({
        cache: "no-store",
        headers: {
          "Idempotency-Key": "idem_1",
          "X-CSRF-Token": "c".repeat(43),
        },
      }),
    );
  });

  it("закрыто отклоняет reveal без server no-store", async () => {
    const payload = { value: "must-be-cleared", valueType: "STRING" as const };
    client.post.mockReturnValueOnce(response(payload));
    await expect(revealRuntimeSecret("secret_main")).rejects.toMatchObject({
      code: "SECRET_REVEAL_CACHE_POLICY_INVALID",
    });
    expect(payload.value).toBe("");
  });
});
