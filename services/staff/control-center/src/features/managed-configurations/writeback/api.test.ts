import { beforeEach, expect, it, vi } from "vitest";
const client = vi.hoisted(() => ({ post: vi.fn(), get: vi.fn() }));
vi.mock("@/shared/api/generated/openapi/client.gen", () => ({ client }));
vi.mock("@/shared/api/client", () => ({
  requestSignal: (signal: AbortSignal) => signal,
}));
vi.mock("@/shared/api/mutation", async () => {
  const { unwrap } = await import("@/shared/api/problem");
  const mutation = (
    request: (headers: Record<string, string>) => Parameters<typeof unwrap>[0],
    version: number,
    key: string,
  ) =>
    unwrap(
      request({
        "If-Match": `"${String(version)}"`,
        "Idempotency-Key": key,
        "X-CSRF-Token": "synthetic-csrf",
      }),
    );
  return {
    etag: (version: number) => `"${String(version)}"`,
    mutate: mutation,
    mutateWithRetry: mutation,
  };
});
import {
  executeIntent,
  listProposals,
  readProposal,
  PreparationRejected,
} from "./api";
import { writeBackFixture } from "./fixtures";
import type { Intent } from "./model";
beforeEach(() => vi.clearAllMocks());
it.each(["ROLE_IMAGE", "INTEGRATION_DEFINITION"] as const)(
  "Prepare %s сохраняет исходный source/OCC/digest и не отправляет approval",
  async (kind) => {
    const { proposal, view } = await writeBackFixture();
    const intent: Intent = {
      action: "PREPARE",
      kind,
      configurationRef: proposal.configurationRef,
      sourceRef: proposal.sourceRef,
      sourceVersion: 4,
      version: 8,
      key: "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
      contentDigest: proposal.proposedContentSha256,
    };
    client.post.mockResolvedValue({
      data: { ...proposal, kind },
      response: new Response(null),
    });
    const signal = new AbortController().signal;
    await executeIntent(intent, signal, view.proposedContent);
    await executeIntent(intent, signal, view.proposedContent);
    expect(client.post.mock.calls[0]?.[0]).toEqual(
      client.post.mock.calls[1]?.[0],
    );
    expect(client.post.mock.calls[0]?.[0]).toMatchObject({
      url: `/api/v1/${kind === "ROLE_IMAGE" ? "role-image" : "integration-definition"}-configurations/{configurationRef}/git-write-backs`,
      path: { configurationRef: proposal.configurationRef },
      headers: { "If-Match": '"8"', "Idempotency-Key": intent.key },
      body: { content: view.proposedContent, expectedSourceVersion: 4 },
      signal,
    });
    await expect(
      executeIntent(intent, signal, view.proposedContent + " "),
    ).rejects.toThrow("differs");
    expect(client.post).toHaveBeenCalledTimes(2);
  },
);
it.each(["APPROVE", "REJECT", "CANCEL"] as const)(
  "%s использует dedicated endpoint и exact proposal OCC",
  async (action) => {
    const { proposal } = await writeBackFixture();
    const intent: Intent = {
      action,
      kind: proposal.kind,
      configurationRef: proposal.configurationRef,
      proposalRef: proposal.ref,
      version: 1,
      approvalDigest: proposal.approvalDigest,
      key: "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
    };
    client.post.mockResolvedValue({
      data: { ...proposal, version: 2 },
      response: new Response(null),
    });
    await executeIntent(intent, new AbortController().signal);
    const options = client.post.mock.lastCall?.[0] as { body?: unknown };
    expect(options).toMatchObject({
      url: `/api/v1/managed-configuration-git-write-backs/{proposalRef}/${action.toLowerCase()}`,
      path: { proposalRef: proposal.ref },
      headers: { "If-Match": '"1"', "Idempotency-Key": intent.key },
    });
    expect(options.body).toEqual(
      action === "CANCEL"
        ? undefined
        : { approvalDigest: proposal.approvalDigest },
    );
  },
);
it("List/Get сохраняют protected scope, pageSize и exact content checks", async () => {
  const { proposal, view } = await writeBackFixture();
  const signal = new AbortController().signal;
  client.get
    .mockResolvedValueOnce({
      data: { items: [proposal], total: 70, nextPageToken: "next" },
      response: new Response(null),
    })
    .mockResolvedValueOnce({ data: view, response: new Response(null) });
  expect((await listProposals(proposal.configurationRef, signal)).total).toBe(
    70,
  );
  expect(client.get.mock.calls[0]?.[0]).toMatchObject({
    path: { configurationRef: proposal.configurationRef },
    query: { pageSize: 30 },
    cache: "no-store",
    signal,
  });
  expect(
    await readProposal(proposal.configurationRef, proposal.ref, signal),
  ).toEqual(view);
  expect(client.get.mock.calls[1]?.[0]).toMatchObject({
    path: { proposalRef: proposal.ref },
    cache: "no-store",
    signal,
  });
  client.get.mockResolvedValueOnce({
    data: { ...view, baseContent: "tampered" },
    response: new Response(null),
  });
  await expect(
    readProposal(proposal.configurationRef, proposal.ref, signal),
  ).rejects.toThrow("digest");
});
it("отклоняет чужой receipt и abort до network и после позднего ответа", async () => {
  const { proposal } = await writeBackFixture();
  const intent: Intent = {
    action: "APPROVE",
    kind: proposal.kind,
    configurationRef: proposal.configurationRef,
    proposalRef: proposal.ref,
    version: 1,
    approvalDigest: proposal.approvalDigest,
    key: "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
  };
  client.post.mockResolvedValue({
    data: { ...proposal, version: 2, configurationRef: "foreign" },
    response: new Response(null),
  });
  await expect(
    executeIntent(intent, new AbortController().signal),
  ).rejects.toThrow();
  const abort = new AbortController();
  abort.abort();
  await expect(executeIntent(intent, abort.signal)).rejects.toThrow();
  expect(client.post).toHaveBeenCalledTimes(1);
  const late = new AbortController();
  client.post.mockImplementation(() => {
    late.abort();
    return Promise.resolve({
      data: { ...proposal, version: 2 },
      response: new Response(null),
    });
  });
  await expect(executeIntent(intent, late.signal)).rejects.toThrow();
});
it.each(["ROLE_IMAGE", "INTEGRATION_DEFINITION"] as const)(
  "окончательный typed Prepare %s rejection отличается от gateway/proxy ошибки",
  async (kind) => {
    const { proposal, view } = await writeBackFixture();
    const intent: Intent = {
      action: "PREPARE",
      configurationRef: proposal.configurationRef,
      kind,
      version: 8,
      sourceRef: proposal.sourceRef,
      sourceVersion: 4,
      key: "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
      contentDigest: proposal.proposedContentSha256,
    };
    for (const status of [400, 412, 422]) {
      client.post.mockResolvedValueOnce({
        error: {
          status,
          code:
            status === 412 ? "VERSION_OR_STATE_CONFLICT" : "INVALID_REQUEST",
          retryable: status === 412,
        },
        response: new Response(null, {
          status,
          headers: { "Content-Type": "application/problem+json" },
        }),
      });
      await expect(
        executeIntent(
          intent,
          new AbortController().signal,
          view.proposedContent,
        ),
      ).rejects.toBeInstanceOf(PreparationRejected);
    }
    expect(client.post).toHaveBeenCalledTimes(3);
    client.post.mockResolvedValueOnce({
      error: "proxy error",
      response: new Response(null, { status: 400 }),
    });
    try {
      await executeIntent(
        intent,
        new AbortController().signal,
        view.proposedContent,
      );
      throw new Error("expected denial");
    } catch (error) {
      expect(error).not.toBeInstanceOf(PreparationRejected);
    }
  },
);
