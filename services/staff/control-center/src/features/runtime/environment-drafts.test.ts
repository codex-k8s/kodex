import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type {
  RuntimeEnvironmentDraft,
  RuntimeEnvironmentDraftSpecification,
  RevisionImpactPlan,
} from "@/shared/api/generated/openapi/types.gen";
const sdk = vi.hoisted(() => ({
  create: vi.fn(),
  read: vi.fn(),
  save: vi.fn(),
  validate: vi.fn(),
  publish: vi.fn(),
  discard: vi.fn(),
  prepare: vi.fn(),
}));
vi.mock("@/shared/api/generated/openapi/sdk.gen", () => ({
  createRuntimeEnvironmentDraft: sdk.create,
  getRuntimeEnvironmentDraft: sdk.read,
  saveRuntimeEnvironmentDraft: sdk.save,
  validateRuntimeEnvironmentDraft: sdk.validate,
  publishRuntimeEnvironmentDraft: sdk.publish,
  discardRuntimeEnvironmentDraft: sdk.discard,
  prepareEnvironmentDraftImpact: sdk.prepare,
}));
vi.mock("@/shared/api/client", () => ({
  requestSignal: (signal: AbortSignal) => signal,
}));
import {
  createEnvironmentDraft,
  readEnvironmentDraft,
  saveEnvironmentDraft,
  transitionEnvironmentDraft,
  environmentDraftFingerprint,
  publishEnvironmentDraft,
  prepareEnvironmentPublication,
} from "./environment-drafts";
const specification: RuntimeEnvironmentDraftSpecification = {
  name: "",
  description: "",
  imageArtifactRef: "",
  tools: [],
  values: [],
  secretBindings: [],
};
function draft(
  state: RuntimeEnvironmentDraft["state"] = "DRAFT",
  version = 1,
): RuntimeEnvironmentDraft {
  return {
    ref: "draft_synthetic",
    projectRef: "project_synthetic",
    version,
    expectedEnvironmentVersion: 0,
    state,
    specification,
    diagnostics: state === "INVALID" ? ["ENVIRONMENT_VALIDATION_FAILED"] : [],
    ...(["VALID", "PUBLISHED"].includes(state)
      ? { validationDigest: "a".repeat(64) }
      : {}),
    ...(state === "PUBLISHED"
      ? { publishedEnvironmentRef: "environment_synthetic" }
      : {}),
  };
}
function result(value: RuntimeEnvironmentDraft) {
  return {
    data: value,
    response: new Response(null, {
      headers: { ETag: `"${String(value.version)}"` },
    }),
  };
}
describe("серверные черновики окружений", () => {
  beforeEach(() =>
    vi.stubGlobal("document", {
      cookie: `__Host-kodex-csrf=${"s".repeat(43)}`,
    }),
  );
  afterEach(() => vi.unstubAllGlobals());
  it("сохраняет неполное окружение без policy и не публикует его", async () => {
    sdk.create.mockResolvedValue(result(draft()));
    const signal = new AbortController().signal;
    await expect(
      createEnvironmentDraft("project_synthetic", specification, signal),
    ).resolves.toEqual(draft());
    expect(sdk.create).toHaveBeenCalledWith(
      expect.objectContaining({
        path: { projectRef: "project_synthetic" },
        body: { specification },
        signal,
      }),
    );
    expect(sdk.publish).not.toHaveBeenCalled();
    expect(sdk.validate).not.toHaveBeenCalled();
  });
  it("передаёт полную пару ref/version существующего окружения и If-Match версии черновика", async () => {
    const existing = {
      ...draft(),
      environmentRef: "environment_synthetic",
      expectedEnvironmentVersion: 7,
    };
    sdk.create.mockResolvedValue(result(existing));
    sdk.save.mockResolvedValue(result({ ...existing, version: 2 }));
    const signal = new AbortController().signal;
    await createEnvironmentDraft("project_synthetic", specification, signal, {
      ref: "environment_synthetic",
      version: 7,
    });
    expect(sdk.create).toHaveBeenCalledWith(
      expect.objectContaining({
        body: {
          environmentRef: "environment_synthetic",
          expectedEnvironmentVersion: 7,
          specification,
        },
      }),
    );
    await saveEnvironmentDraft(existing, specification, signal);
    const saveRequest = sdk.save.mock.calls[0]?.[0] as {
      body: unknown;
      headers: Record<string, string>;
    };
    expect(saveRequest.body).toEqual(specification);
    expect(saveRequest.headers["If-Match"]).toBe('"1"');
    expect(saveRequest.headers["Idempotency-Key"]).toEqual(expect.any(String));
    expect(sdk.publish).not.toHaveBeenCalled();
  });
  it("разделяет INVALID, VALID, PUBLISHED и DISCARDED ответы", async () => {
    const signal = new AbortController().signal;
    sdk.validate
      .mockResolvedValueOnce(result(draft("INVALID", 2)))
      .mockResolvedValueOnce(result(draft("VALID", 3)));
    const invalid = await transitionEnvironmentDraft(
      "validate",
      draft(),
      signal,
    );
    expect(invalid.diagnostics).toEqual(["ENVIRONMENT_VALIDATION_FAILED"]);
    const valid = await transitionEnvironmentDraft("validate", invalid, signal);
    const plan: RevisionImpactPlan = {
      ref: "plan",
      version: 1,
      kind: "RUNTIME_ENVIRONMENT",
      sourceVersion: 0,
      draftRef: valid.ref,
      draftVersion: valid.version,
      targetDigest: "target",
      digest: "plan-digest",
      total: 0,
      state: "PREPARED",
      createdAt: "2026-09-06T00:00:00Z",
      expiresAt: "2099-09-06T00:00:00Z",
    };
    sdk.publish.mockResolvedValue({
      data: {
        draft: draft("PUBLISHED", 4),
        environment: {
          ref: "environment_synthetic",
          projectRef: valid.projectRef,
          currentVersion: { ref: "version", digest: "target" },
        },
        plan: {
          ...plan,
          version: 2,
          state: "APPLIED",
          publishedRevisionRef: "version",
        },
      },
      response: new Response(),
    });
    const published = await publishEnvironmentDraft(
      valid,
      plan,
      [],
      signal,
      "original-key",
    );
    expect(published.draft.publishedEnvironmentRef).toBe(
      "environment_synthetic",
    );
    sdk.discard.mockResolvedValue(result(draft("DISCARDED", 2)));
    expect(
      (await transitionEnvironmentDraft("discard", draft(), signal)).state,
    ).toBe("DISCARDED");
    const publishRequest = sdk.publish.mock.calls[0]?.[0] as {
      headers: Record<string, string>;
      body: unknown;
    };
    expect(publishRequest.headers["If-Match"]).toBe('"3"');
    expect(publishRequest.body).toEqual({
      planRef: "plan",
      selectedItemRefs: [],
    });
    expect(publishRequest.headers["Idempotency-Key"]).toBe("original-key");
  });
  it("отклоняет чужой scope и несовпадающий ETag без повторной команды", async () => {
    sdk.read.mockResolvedValue(
      result({ ...draft(), projectRef: "project_other" }),
    );
    await expect(
      readEnvironmentDraft(
        "project_synthetic",
        "draft_synthetic",
        new AbortController().signal,
      ),
    ).rejects.toThrow();
    sdk.save.mockResolvedValue({
      data: draft(),
      response: new Response(null, { headers: { ETag: '"99"' } }),
    });
    await expect(
      saveEnvironmentDraft(
        draft(),
        specification,
        new AbortController().signal,
      ),
    ).rejects.toThrow();
    expect(sdk.save).toHaveBeenCalledOnce();
  });
  it("готовит план только после fresh VALID draft и проверяет исходный immutable base", async () => {
    const current = {
      ...draft("VALID", 3),
      environmentRef: "environment",
      expectedEnvironmentVersion: 7,
      baseVersionRef: "base",
    };
    const plan: RevisionImpactPlan = {
      ref: "plan",
      version: 1,
      kind: "RUNTIME_ENVIRONMENT",
      sourceRef: "environment",
      sourceVersion: 7,
      sourceRevisionRef: "base",
      draftRef: current.ref,
      draftVersion: 3,
      targetDigest: "target",
      digest: "digest",
      total: 0,
      state: "PREPARED",
      createdAt: "2026-09-06T00:00:00Z",
      expiresAt: "2099-09-06T00:00:00Z",
    };
    sdk.read.mockResolvedValue(result(current));
    sdk.prepare.mockResolvedValue({ data: plan, response: new Response() });
    expect(
      await prepareEnvironmentPublication(
        current,
        new AbortController().signal,
      ),
    ).toEqual(plan);
    const count = sdk.prepare.mock.calls.length;
    sdk.read.mockResolvedValue(result({ ...current, version: 4 }));
    await expect(
      prepareEnvironmentPublication(current, new AbortController().signal),
    ).rejects.toThrow();
    expect(sdk.prepare.mock.calls).toHaveLength(count);
    sdk.read.mockResolvedValue(result(current));
    sdk.prepare.mockResolvedValue({
      data: { ...plan, sourceRevisionRef: "other" },
      response: new Response(),
    });
    await expect(
      prepareEnvironmentPublication(current, new AbortController().signal),
    ).rejects.toThrow();
  });
  it("сравнивает specification независимо от порядка ключей JSON", () => {
    expect(environmentDraftFingerprint(specification)).toBe(
      environmentDraftFingerprint({
        secretBindings: [],
        values: [],
        tools: [],
        imageArtifactRef: "",
        description: "",
        name: "",
      }),
    );
  });
});
