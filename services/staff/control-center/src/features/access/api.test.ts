import { beforeEach, describe, expect, it, vi } from "vitest";

const sdk = vi.hoisted(() => ({
  explainAccess: vi.fn(),
  listPlatformMemberships: vi.fn(),
  queryEffectiveAccess: vi.fn(),
  simulateAccess: vi.fn(),
}));

vi.mock("@/shared/api/generated/openapi/sdk.gen", () => sdk);
vi.mock("@/shared/api/client", () => ({
  requestSignal: () => new AbortController().signal,
}));
vi.mock("@/shared/api/mutation", () => ({
  csrfToken: () => "csrf-test-value-with-sufficient-length-000000000000",
  mutate: vi.fn(),
}));

import {
  fetchAccessExplanation,
  fetchAccessSimulation,
  fetchEffectiveAccess,
  fetchPlatformMemberships,
} from "@/features/access/api";

const csrfHeaders = {
  "X-CSRF-Token": "csrf-test-value-with-sufficient-length-000000000000",
};
const target = { kind: "ORGANIZATION" as const };

describe("access decision API", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    for (const request of [
      sdk.queryEffectiveAccess,
      sdk.explainAccess,
      sdk.simulateAccess,
    ]) {
      request.mockResolvedValue({
        data: {},
        response: new Response(null, { status: 200 }),
      });
    }
  });

  it("передаёт CSRF во все POST-запросы чтения эффективного доступа", async () => {
    await fetchEffectiveAccess({ target, permissionKeys: ["agent.launch"] });
    await fetchAccessExplanation({
      target,
      permissionKey: "agent.launch",
    });
    await fetchAccessSimulation({
      subjectRef: "subject_test",
      target,
      permissionKey: "agent.launch",
      role: {
        permissionKeys: ["agent.launch"],
        allowedScopes: ["ORGANIZATION"],
      },
      binding: {
        subjectKind: "OIDC_GROUP",
        subjectRef: "subject_test",
        scope: target,
        conditions: { requireOwner: false },
      },
    });

    for (const request of [
      sdk.queryEffectiveAccess,
      sdk.explainAccess,
      sdk.simulateAccess,
    ]) {
      expect(request).toHaveBeenCalledWith(
        expect.objectContaining({ headers: csrfHeaders }),
      );
    }
  });

  it("не выдаёт первую страницу platform memberships за полный список", async () => {
    const membership = (ref: string) => ({
      ref,
      version: 1,
      user: { ref: `user_${ref}`, displayName: ref },
      platformRole: "MEMBER" as const,
      permissions: [],
      active: true,
      nextActions: [],
    });
    sdk.listPlatformMemberships
      .mockResolvedValueOnce({
        data: { items: [membership("first")], nextPageToken: "next-page" },
        response: new Response(null, { status: 200 }),
      })
      .mockResolvedValueOnce({
        data: { items: [membership("second")] },
        response: new Response(null, { status: 200 }),
      });

    await expect(fetchPlatformMemberships()).resolves.toEqual([
      membership("first"),
      membership("second"),
    ]);
    expect(sdk.listPlatformMemberships).toHaveBeenNthCalledWith(
      2,
      expect.objectContaining({
        query: { pageSize: 100, pageToken: "next-page" },
      }),
    );
  });
});
