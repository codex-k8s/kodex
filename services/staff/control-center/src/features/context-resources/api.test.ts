import { beforeEach, describe, expect, it, vi } from "vitest";
import type {
  SkillBundle,
  KodexMemoryRecord,
} from "@/shared/api/generated/openapi/types.gen";
const client = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
  put: vi.fn(),
}));
vi.mock("@/shared/api/generated/openapi/client.gen", () => ({ client }));
vi.mock("@/shared/api/client", () => ({
  requestSignal: (signal?: AbortSignal) =>
    signal ?? new AbortController().signal,
}));
vi.mock("@/shared/api/mutation", () => ({
  mutate: (
    request: (headers: Record<string, string>) => Promise<unknown>,
    version?: number,
  ) =>
    request({
      "Idempotency-Key": "synthetic-key",
      "X-CSRF-Token": "synthetic-csrf",
      ...(version ? { "If-Match": `"${String(version)}"` } : {}),
    }),
}));
import {
  listContext,
  readMemory,
  saveMemory,
  saveSkill,
  transitionSkill,
  reviewSkill,
  lifecycle,
} from "./api";
const timestamp = "2026-09-05T00:00:00Z";
const provenance = {
  actorRef: "user",
  sourceKind: "USER",
  digest: "a".repeat(64),
  createdAt: timestamp,
};
const skill: SkillBundle = {
  ref: "skill",
  version: 8,
  projectRef: "project",
  state: "ACTIVE",
  createdAt: timestamp,
  updatedAt: timestamp,
  draftRevision: {
    ref: "revision",
    revision: 2,
    state: "DRAFT",
    name: "Skill",
    description: "",
    files: [],
    digest: "b".repeat(64),
    provenance,
    scanState: "PENDING",
    diagnostics: [],
  },
};
const memory: KodexMemoryRecord = {
  ref: "memory",
  version: 3,
  projectRef: "project",
  state: "ACTIVE",
  createdAt: timestamp,
  updatedAt: timestamp,
  currentRevision: {
    ref: "memory-revision",
    revision: 1,
    title: "Memory",
    summary: "Synthetic summary",
    digest: "c".repeat(64),
    provenance,
    retentionUntil: "2026-10-01T00:00:00Z",
    redacted: false,
  },
};
const response = (data: unknown) => ({
  data,
  response: new Response(null, { status: 200 }),
});
describe("context resource adapters", () => {
  beforeEach(() => vi.resetAllMocks());
  it("передает серверный scope/query/state и cursor без client fan-out", async () => {
    client.get.mockResolvedValue(
      response({ items: [skill], total: 1, nextPageToken: "" }),
    );
    await listContext("skills", {
      projectRef: "project",
      agentRef: "agent",
      query: "Skill",
      state: "ACTIVE",
      pageToken: "next",
      signal: new AbortController().signal,
    });
    expect(client.get).toHaveBeenCalledWith(
      expect.objectContaining({
        query: {
          projectRef: "project",
          agentRef: "agent",
          query: "Skill",
          state: "ACTIVE",
          pageToken: "next",
          pageSize: 40,
        },
      }),
    );
    expect(client.get).toHaveBeenCalledTimes(1);
  });
  it("отклоняет чужой project и незакрытый expired content", async () => {
    client.get.mockResolvedValue(
      response({ items: [skill], total: 1, nextPageToken: "" }),
    );
    await expect(
      listContext("skills", {
        projectRef: "other",
        query: "",
        state: "ACTIVE",
        signal: new AbortController().signal,
      }),
    ).rejects.toThrow("scope");
    client.get.mockResolvedValue(response({ ...memory, state: "EXPIRED" }));
    await expect(
      readMemory("memory", new AbortController().signal),
    ).rejects.toThrow("redaction");
    client.get.mockResolvedValue(
      response({
        ...memory,
        currentRevision: { ...memory.currentRevision, redacted: true },
      }),
    );
    await expect(
      readMemory("memory", new AbortController().signal),
    ).rejects.toThrow("redaction");
  });
  it("сохраняет Skill draft с OCC aggregate, без выданного клиентом scan", async () => {
    client.put.mockResolvedValue(response({ ...skill, version: 9 }));
    const specification = {
      name: "Skill",
      description: "",
      files: [
        { path: "SKILL.md", artifactRef: "artifact", artifactRevision: 3 },
      ],
    };
    await saveSkill("project", specification, skill);
    const request = client.put.mock.calls[0]?.[0] as {
      headers: Record<string, string>;
      path: unknown;
      body: unknown;
    };
    expect(request.headers["If-Match"]).toBe('"8"');
    expect(request.path).toEqual({
      bundleRef: "skill",
      revisionRef: "revision",
    });
    expect(request.body).toEqual(specification);
  });
  it("validate/review закрепляют exact digest и не принимают actor/review result из формы", async () => {
    const revision = skill.draftRevision;
    if (!revision) throw new Error("Missing synthetic revision");
    client.post.mockResolvedValue(response({ ...skill, version: 9 }));
    await transitionSkill(skill, "validate", revision);
    expect(client.post).toHaveBeenCalledWith(
      expect.objectContaining({ body: { expectedDigest: revision.digest } }),
    );
    await reviewSkill(skill, revision, "APPROVE", "Reviewed");
    expect(client.post).toHaveBeenLastCalledWith(
      expect.objectContaining({
        body: {
          expectedDigest: revision.digest,
          decision: "APPROVE",
          comment: "Reviewed",
        },
      }),
    );
  });
  it("Memory revise не подменяет immutable revision update-ом", async () => {
    client.post.mockResolvedValue(response({ ...memory, version: 4 }));
    await saveMemory(
      "project",
      {
        title: "Updated",
        summary: "New summary",
        retentionUntil: "2026-10-02T00:00:00Z",
      },
      memory,
    );
    expect(client.post).toHaveBeenCalledWith(
      expect.objectContaining({
        url: "/api/v1/memory-records/{recordRef}/revisions",
        path: { recordRef: "memory" },
      }),
    );
    const request = client.post.mock.calls[0]?.[0] as {
      headers: Record<string, string>;
    };
    expect(request.headers["If-Match"]).toBe('"3"');
  });
  it("purge сверяет полученный ресурс и не повторяет мутацию при mismatch", async () => {
    client.post.mockResolvedValue(response({ ...memory, ref: "other" }));
    await expect(lifecycle("memory", memory, "purge")).rejects.toThrow("scope");
    expect(client.post).toHaveBeenCalledTimes(1);
  });
});
