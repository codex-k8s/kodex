import { describe, expect, it, vi } from "vitest";
import { AppProblem } from "@/shared/api/problem";

const sdk = vi.hoisted(() => ({
  createRuntimeSecretDraft: vi.fn(),
  saveRuntimeSecretDraft: vi.fn(),
  getRuntimeSecretDraft: vi.fn(),
  validateRuntimeSecretDraft: vi.fn(),
  discardRuntimeSecretDraft: vi.fn(),
}));
vi.mock("@/shared/api/generated/openapi/sdk.gen", () => sdk);
vi.mock("@/shared/api/client", () => ({
  requestSignal: (signal?: AbortSignal) => signal,
}));
vi.mock("@/shared/api/mutation", () => ({
  mutate: async (
    request: (headers: Record<string, string>) => Promise<unknown>,
    version: number | undefined,
    key: string,
  ) =>
    request({
      "Idempotency-Key": key,
      "X-CSRF-Token": "csrf",
      ...(version ? { "If-Match": `"${String(version)}"` } : {}),
    }),
}));

import {
  checkedDraft,
  safeDraftProblem,
  createSecretDraft,
  saveSecretDraft,
  readSecretDraft,
  changeSecretDraft,
  type RuntimeSecretDraft,
} from "./draft-api";

const draft: RuntimeSecretDraft = {
  ref: "draft_1",
  version: 4,
  generation: 2,
  projectRef: "project_1",
  secretRef: "secret_1",
  secretVersion: 3,
  name: "TOKEN",
  description: "",
  valueType: "STRING",
  state: "DRAFT",
  publishedRevision: 0,
  createdAt: "2026-09-05T01:00:00Z",
  updatedAt: "2026-09-05T01:00:00Z",
  expiresAt: "2026-09-06T01:00:00Z",
};

describe("безопасный adapter черновиков Secret", () => {
  it("не принимает чужую область, нулевую owner version и неизвестное состояние", () => {
    expect(() => checkedDraft(draft, "other")).toThrow();
    expect(() =>
      checkedDraft({ ...draft, secretVersion: 0 }, "project_1"),
    ).toThrow();
    expect(() =>
      checkedDraft(
        { ...draft, state: "NEW" as RuntimeSecretDraft["state"] },
        "project_1",
      ),
    ).toThrow();
    expect(() => checkedDraft(draft, "project_1", { ref: "other" })).toThrow();
  });

  it("оставляет только безопасные metadata и не отражает текст ошибки", () => {
    const incoming = {
      ...draft,
      value: "private",
      digest: "private",
      grant: "private",
    };
    expect(checkedDraft(incoming, "project_1")).toEqual(draft);
    const problem = safeDraftProblem(
      new AppProblem({
        status: 502,
        code: "UNAVAILABLE",
        kind: "unavailable",
        retryable: true,
        title: "private",
        detail: "private",
        correlationId: "private",
      }),
    );
    expect(problem).toMatchObject({ status: 502, retryable: true });
    expect(JSON.stringify(problem)).not.toContain("private");
  });

  it("повторяет save с исходным key и OCC Secret, lifecycle с OCC Draft", async () => {
    sdk.createRuntimeSecretDraft.mockResolvedValue({ data: draft });
    sdk.saveRuntimeSecretDraft.mockResolvedValue({ data: draft });
    sdk.validateRuntimeSecretDraft.mockResolvedValue({
      data: { ...draft, state: "VALID" },
    });
    sdk.discardRuntimeSecretDraft.mockResolvedValue({
      data: { ...draft, state: "DISCARDED" },
    });
    const input = { valueType: "STRING" as const, value: "ephemeral" };
    await createSecretDraft(
      "project_1",
      { ...input, name: "TOKEN", description: "" },
      "create-key",
    );
    await saveSecretDraft(
      "project_1",
      { ref: "secret_1", version: 3 },
      input,
      "save-key",
    );
    await saveSecretDraft(
      "project_1",
      { ref: "secret_1", version: 3 },
      input,
      "save-key",
    );
    expect(sdk.saveRuntimeSecretDraft.mock.calls[0]).toEqual(
      sdk.saveRuntimeSecretDraft.mock.calls[1],
    );
    expect(sdk.saveRuntimeSecretDraft.mock.calls[0]?.[0]).toMatchObject({
      headers: { "If-Match": '"3"', "Idempotency-Key": "save-key" },
    });
    await changeSecretDraft(draft, "validate", "validate-key");
    await changeSecretDraft(draft, "discard", "discard-key");
    expect(sdk.validateRuntimeSecretDraft.mock.calls[0]?.[0]).toMatchObject({
      headers: { "If-Match": '"4"', "Idempotency-Key": "validate-key" },
    });
  });

  it("восстанавливает только точный draft и передаёт отмену чтения", async () => {
    sdk.getRuntimeSecretDraft.mockResolvedValue({
      data: draft,
      response: new Response(null, { status: 200 }),
    });
    const controller = new AbortController();
    await expect(
      readSecretDraft("project_1", "draft_1", controller.signal),
    ).resolves.toEqual(draft);
    expect(sdk.getRuntimeSecretDraft.mock.calls[0]?.[0]).toMatchObject({
      signal: controller.signal,
      cache: "no-store",
    });
    await expect(
      readSecretDraft("project_1", "draft_2", controller.signal),
    ).rejects.toThrow();
  });
});
