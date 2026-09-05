import { beforeEach, describe, expect, it, vi } from "vitest";
import type {
  RuntimeEnvironmentConsumer,
  RuntimeEnvironmentImpact,
  RuntimeSecretImpact,
} from "@/shared/api/generated/openapi/types.gen";
const client = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn() }));
vi.mock("@/shared/api/generated/openapi/client.gen", () => ({ client }));
vi.mock("@/shared/api/client", () => ({
  requestSignal: (signal?: AbortSignal) =>
    signal ?? new AbortController().signal,
}));
vi.mock("@/shared/api/mutation", () => ({
  mutate: (
    request: (headers: Record<string, string>) => Promise<unknown>,
    version: number,
  ) =>
    request({
      "If-Match": `"${String(version)}"`,
      "Idempotency-Key": "synthetic-key",
      "X-CSRF-Token": "synthetic-csrf",
    }),
}));
import {
  applyEnvironmentRebind,
  applySecretRebind,
  readEnvironmentImpact,
  readSecretImpact,
} from "./revision-impact";
const consumer: RuntimeEnvironmentConsumer = {
  agentRef: "agent",
  agentVersion: 3,
  bindingRef: "binding",
  bindingVersion: 4,
  projectRef: "project",
  versionRef: "old-version",
};
const environment: RuntimeEnvironmentImpact = {
  environmentRef: "environment",
  environmentVersion: 19,
  targetVersionRef: "target-version",
  targetDigest: "a".repeat(64),
  consumers: [consumer],
  total: 1,
  nextPageToken: "",
};
const secret: RuntimeSecretImpact = {
  secretRef: "secret",
  secretVersion: 23,
  targetRevision: 7,
  consumers: [
    {
      environmentRef: "environment",
      environmentVersion: 19,
      environmentVersionRef: "old-version",
      projectRef: "project",
      secretRevisions: [6],
      consumer,
    },
  ],
  total: 1,
  nextPageToken: "",
};
const binding = {
  ref: "binding",
  version: 5,
  agentRef: "agent",
  environmentRef: "environment",
  versionRef: "target-version",
  digest: environment.targetDigest,
};
const response = (data: unknown) => ({
  data,
  response: new Response(null, { status: 200 }),
});
describe("revision impact adapters", () => {
  beforeEach(() => vi.resetAllMocks());
  it("передает исходную версию потребителя, целевую версию в path и OCC окружения", async () => {
    client.post.mockResolvedValue(response({ bindings: [binding] }));
    await applyEnvironmentRebind(environment, [consumer]);
    expect(client.post).toHaveBeenCalledWith(
      expect.objectContaining({
        path: { environmentRef: "environment", versionRef: "target-version" },
        headers: {
          "If-Match": '"19"',
          "Content-Type": "application/json",
          "Idempotency-Key": "synthetic-key",
          "X-CSRF-Token": "synthetic-csrf",
        },
        body: { consumers: [consumer] },
      }),
    );
  });
  it("отклоняет чужую и повторную выборку до мутации", async () => {
    await expect(
      applyEnvironmentRebind(environment, [{ ...consumer, bindingVersion: 5 }]),
    ).rejects.toThrow("selection");
    await expect(
      applyEnvironmentRebind(environment, [consumer, consumer]),
    ).rejects.toThrow("selection");
    expect(client.post).not.toHaveBeenCalled();
  });
  it("не принимает частичную квитанцию и не повторяет мутацию", async () => {
    client.post.mockResolvedValue(response({ bindings: [] }));
    await expect(
      applyEnvironmentRebind(environment, [consumer]),
    ).rejects.toThrow("receipt");
    expect(client.post).toHaveBeenCalledTimes(1);
  });
  it("проверяет scope и повтор курсора при чтении", async () => {
    client.get.mockResolvedValue(
      response({ ...environment, environmentRef: "other" }),
    );
    await expect(
      readEnvironmentImpact(
        "environment",
        "target-version",
        undefined,
        new AbortController().signal,
      ),
    ).rejects.toThrow("impact");
    client.get.mockResolvedValue(
      response({ ...secret, nextPageToken: "same" }),
    );
    await expect(
      readSecretImpact("secret", 7, "same", new AbortController().signal),
    ).rejects.toThrow("impact");
  });
  it("публикует окружение без агентов с пустым обязательным consumers", async () => {
    const selection = {
      environmentRef: "environment",
      expectedEnvironmentVersion: 19,
      sourceVersionRef: "old-version",
      consumers: [],
    };
    client.post.mockResolvedValue(
      response({
        environments: [
          {
            environmentRef: "environment",
            environmentVersion: 20,
            projectRef: "project",
            versionRef: "new-version",
            digest: "b".repeat(64),
          },
        ],
        bindings: [],
      }),
    );
    await applySecretRebind(secret, [selection]);
    expect(client.post).toHaveBeenCalledWith(
      expect.objectContaining({
        path: { secretRef: "secret", revision: 7 },
        headers: {
          "If-Match": '"23"',
          "Content-Type": "application/json",
          "Idempotency-Key": "synthetic-key",
          "X-CSRF-Token": "synthetic-csrf",
        },
        body: { selections: [selection] },
      }),
    );
  });
  it("отклоняет подмену sourceVersionRef и потерянные опубликованные окружения", async () => {
    const selection = {
      environmentRef: "environment",
      expectedEnvironmentVersion: 19,
      sourceVersionRef: "target-version",
      consumers: [],
    };
    await expect(applySecretRebind(secret, [selection])).rejects.toThrow(
      "snapshot",
    );
    expect(client.post).not.toHaveBeenCalled();
    client.post.mockResolvedValue(response({ environments: [], bindings: [] }));
    await expect(
      applySecretRebind(secret, [
        { ...selection, sourceVersionRef: "old-version" },
      ]),
    ).rejects.toThrow("receipt");
  });
});
