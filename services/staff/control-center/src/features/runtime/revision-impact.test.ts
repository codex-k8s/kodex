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
  scopeKind: "PROJECT",
  organizationRef: "org_synthetic",
  agentRef: "agent",
  agentVersion: 3,
  bindingRef: "binding",
  bindingVersion: 4,
  projectRef: "project_synthetic",
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
      projectRef: "project_synthetic",
      scopeKind: "PROJECT",
      organizationRef: "org_synthetic",
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

it("закрывает чтение без authoritative org anchor до HTTP", async () => {
  await expect(
    readEnvironmentImpact(
      "environment",
      "target-version",
      undefined,
      new AbortController().signal,
    ),
  ).rejects.toThrow("anchor");
  expect(client.get).not.toHaveBeenCalled();
});
it("закрывает secret rebind с чужим nested owner до HTTP даже при совпадении selection", async () => {
  const foreign = { ...consumer, organizationRef: "org_foreign" };
  const row = secret.consumers[0];
  if (!row) throw new Error("Synthetic consumer is missing");
  await expect(
    applySecretRebind(
      { ...secret, consumers: [{ ...row, consumer: foreign }] },
      [
        {
          environmentRef: row.environmentRef,
          expectedEnvironmentVersion: row.environmentVersion,
          sourceVersionRef: row.environmentVersionRef,
          scopeKind: row.scopeKind,
          organizationRef: row.organizationRef,
          projectRef: row.projectRef,
          consumers: [foreign],
        },
      ],
      "org_synthetic",
    ),
  ).rejects.toThrow("selection");
  expect(client.post).not.toHaveBeenCalled();
});
it.each([
  { scopeKind: "UNSPECIFIED" },
  { scopeKind: undefined },
  { organizationRef: "org_foreign" },
  { scopeKind: "PROJECT", projectRef: "" },
  { scopeKind: "ORGANIZATION", projectRef: "project_synthetic" },
])("отклоняет некорректный consumer owner tuple %j", async (change) => {
  client.get.mockResolvedValue(
    response({ ...environment, consumers: [{ ...consumer, ...change }] }),
  );
  await expect(
    readEnvironmentImpact(
      "environment",
      "target-version",
      undefined,
      new AbortController().signal,
      "",
      40,
      "org_synthetic",
    ),
  ).rejects.toThrow("impact");
});
it("публикует bindingless ORG окружение с точным пустым project и проверяет receipt owner", async () => {
  const original = secret.consumers[0];
  if (!original) throw new Error("Synthetic consumer is missing");
  const row = {
    ...original,
    scopeKind: "ORGANIZATION" as const,
    projectRef: "",
    consumer: undefined,
  };
  const owned = { ...secret, consumers: [row] };
  client.get.mockResolvedValue(response(owned));
  await expect(
    readSecretImpact(
      "secret",
      7,
      undefined,
      new AbortController().signal,
      "",
      40,
      "org_synthetic",
    ),
  ).resolves.toEqual(owned);
  const selection = {
    scopeKind: row.scopeKind,
    organizationRef: row.organizationRef,
    projectRef: row.projectRef,
    environmentRef: row.environmentRef,
    expectedEnvironmentVersion: row.environmentVersion,
    sourceVersionRef: row.environmentVersionRef,
    consumers: [],
  };
  const receipt = {
    environmentRef: row.environmentRef,
    environmentVersion: 20,
    versionRef: "published-version",
    digest: "a".repeat(64),
    scopeKind: row.scopeKind,
    organizationRef: row.organizationRef,
    projectRef: "",
  };
  client.post.mockResolvedValue(
    response({ environments: [receipt], bindings: [] }),
  );
  await expect(
    applySecretRebind(owned, [selection], "org_synthetic"),
  ).resolves.toMatchObject({ environments: [receipt] });
  client.post.mockResolvedValue(
    response({
      environments: [{ ...receipt, organizationRef: "org_foreign" }],
      bindings: [],
    }),
  );
  await expect(
    applySecretRebind(owned, [selection], "org_synthetic"),
  ).rejects.toThrow("receipt");
});
it("не публикует selection чужой организации даже без binding", async () => {
  const row = secret.consumers[0];
  if (!row) throw new Error("Synthetic consumer is missing");
  const selection = {
    scopeKind: "PROJECT" as const,
    organizationRef: "org_foreign",
    projectRef: row.projectRef,
    environmentRef: row.environmentRef,
    expectedEnvironmentVersion: row.environmentVersion,
    sourceVersionRef: row.environmentVersionRef,
    consumers: [],
  };
  await expect(
    applySecretRebind(secret, [selection], "org_synthetic"),
  ).rejects.toThrow("selection");
  expect(client.post).not.toHaveBeenCalled();
});
describe("revision impact adapters", () => {
  beforeEach(() => vi.resetAllMocks());
  it("передаёт query вместе с cursor в оба impact endpoint", async () => {
    const signal = new AbortController().signal;
    client.get.mockResolvedValueOnce(response(environment));
    await readEnvironmentImpact(
      "environment",
      "target-version",
      "page",
      signal,
      "  agent  ",
      undefined,
      "org_synthetic",
    );
    expect(client.get).toHaveBeenLastCalledWith(
      expect.objectContaining({
        query: { pageSize: 40, pageToken: "page", query: "agent" },
        signal,
      }),
    );
    client.get.mockResolvedValueOnce(response(secret));
    await readSecretImpact(
      "secret",
      7,
      undefined,
      signal,
      " env ",
      undefined,
      "org_synthetic",
    );
    expect(client.get).toHaveBeenLastCalledWith(
      expect.objectContaining({
        query: { pageSize: 40, query: "env" },
        signal,
      }),
    );
  });
  it("передает исходную версию потребителя, целевую версию в path и OCC окружения", async () => {
    client.post.mockResolvedValue(response({ bindings: [binding] }));
    await applyEnvironmentRebind(environment, [consumer], "org_synthetic");
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
      applyEnvironmentRebind(
        environment,
        [{ ...consumer, bindingVersion: 5 }],
        "org_synthetic",
      ),
    ).rejects.toThrow("selection");
    await expect(
      applyEnvironmentRebind(
        environment,
        [consumer, consumer],
        "org_synthetic",
      ),
    ).rejects.toThrow("selection");
    expect(client.post).not.toHaveBeenCalled();
  });
  it("не принимает частичную квитанцию и не повторяет мутацию", async () => {
    client.post.mockResolvedValue(response({ bindings: [] }));
    await expect(
      applyEnvironmentRebind(environment, [consumer], "org_synthetic"),
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
        undefined,
        undefined,
        "org_synthetic",
      ),
    ).rejects.toThrow("impact");
    client.get.mockResolvedValue(
      response({ ...secret, nextPageToken: "same" }),
    );
    await expect(
      readSecretImpact(
        "secret",
        7,
        "same",
        new AbortController().signal,
        undefined,
        undefined,
        "org_synthetic",
      ),
    ).rejects.toThrow("impact");
  });
  it("публикует окружение без агентов с пустым обязательным consumers", async () => {
    const selection = {
      scopeKind: "PROJECT" as const,
      organizationRef: "org_synthetic",
      projectRef: "project_synthetic",
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
            projectRef: "project_synthetic",
            scopeKind: "PROJECT",
            organizationRef: "org_synthetic",
            versionRef: "new-version",
            digest: "b".repeat(64),
          },
        ],
        bindings: [],
      }),
    );
    await applySecretRebind(secret, [selection], "org_synthetic");
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
      scopeKind: "PROJECT" as const,
      organizationRef: "org_synthetic",
      projectRef: "project_synthetic",
      environmentRef: "environment",
      expectedEnvironmentVersion: 19,
      sourceVersionRef: "target-version",
      consumers: [],
    };
    await expect(
      applySecretRebind(secret, [selection], "org_synthetic"),
    ).rejects.toThrow("snapshot");
    expect(client.post).not.toHaveBeenCalled();
    client.post.mockResolvedValue(response({ environments: [], bindings: [] }));
    await expect(
      applySecretRebind(
        secret,
        [{ ...selection, sourceVersionRef: "old-version" }],
        "org_synthetic",
      ),
    ).rejects.toThrow("receipt");
  });
});
