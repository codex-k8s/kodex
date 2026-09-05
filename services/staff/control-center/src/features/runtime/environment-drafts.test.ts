import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type {
  RuntimeEnvironmentDraft,
  RuntimeEnvironmentDraftSpecification,
} from "@/shared/api/generated/openapi/types.gen";
const sdk = vi.hoisted(() => ({
  create: vi.fn(),
  read: vi.fn(),
  save: vi.fn(),
  validate: vi.fn(),
  publish: vi.fn(),
  discard: vi.fn(),
}));
vi.mock("@/shared/api/generated/openapi/sdk.gen", () => ({
  createRuntimeEnvironmentDraft: sdk.create,
  getRuntimeEnvironmentDraft: sdk.read,
  saveRuntimeEnvironmentDraft: sdk.save,
  validateRuntimeEnvironmentDraft: sdk.validate,
  publishRuntimeEnvironmentDraft: sdk.publish,
  discardRuntimeEnvironmentDraft: sdk.discard,
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
    sdk.publish.mockResolvedValue(result(draft("PUBLISHED", 4)));
    const published = await transitionEnvironmentDraft(
      "publish",
      valid,
      signal,
    );
    expect(published.publishedEnvironmentRef).toBe("environment_synthetic");
    sdk.discard.mockResolvedValue(result(draft("DISCARDED", 2)));
    expect(
      (await transitionEnvironmentDraft("discard", draft(), signal)).state,
    ).toBe("DISCARDED");
    const publishRequest = sdk.publish.mock.calls[0]?.[0] as {
      headers: Record<string, string>;
    };
    expect(publishRequest.headers["If-Match"]).toBe('"3"');
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
