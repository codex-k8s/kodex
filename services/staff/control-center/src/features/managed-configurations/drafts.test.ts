import { beforeEach, describe, expect, it, vi } from "vitest";
import type {
  ManagedConfiguration,
  ManagedConfigurationResult,
  ManagedConfigurationRevision,
} from "@/shared/api/generated/openapi/types.gen";
const client = vi.hoisted(() => ({
  post: vi.fn<
    (options: {
      url: string;
      body?: unknown;
      headers: Record<string, string>;
    }) => Promise<{ data: ManagedConfigurationResult; response: Response }>
  >(),
}));
vi.mock("@/shared/api/generated/openapi/client.gen", () => ({ client }));
vi.mock("@/shared/api/client", () => ({
  requestSignal: () => new AbortController().signal,
}));
vi.mock("@/shared/api/mutation", async () => {
  const { unwrap } = await import("@/shared/api/problem");
  return {
    etag: (version: number) => `"${String(version)}"`,
    mutate: (
      request: (
        headers: Record<string, string>,
      ) => Parameters<typeof unwrap>[0],
      version: number,
    ) =>
      unwrap(
        request({
          "If-Match": `"${String(version)}"`,
          "Idempotency-Key": "synthetic-key",
          "X-CSRF-Token": "synthetic-csrf",
        }),
      ),
  };
});
import { changeDraft } from "./api";
import { canChangeDraft } from "./model";
const configuration: ManagedConfiguration = {
  ref: "configuration_synthetic",
  version: 8,
  kind: "PROMPT_TEMPLATE",
  name: "Synthetic",
  managedBy: "UI",
  source: "UI",
  sourceRevision: "",
  updatedAt: "2026-09-05T00:00:00Z",
};
const revision: ManagedConfigurationRevision = {
  ref: "revision_old",
  revision: 3,
  state: "VALID",
  contentFormat: "TEXT",
  content: "Synthetic",
  digest: "a".repeat(64),
  validationDiagnostics: [],
  createdAt: "2026-09-05T00:00:00Z",
};
const kinds = [
  ["PROMPT_TEMPLATE", "prompt-template-configurations"],
  ["ROLE_IMAGE", "role-image-configurations"],
  ["INTEGRATION_DEFINITION", "integration-definition-configurations"],
  ["SYSTEM_STT", "system-stt-configurations"],
] as const;
function response(data: ManagedConfigurationResult) {
  return { data, response: new Response(null, { headers: { ETag: '"9"' } }) };
}
describe("Immutable managed drafts", () => {
  beforeEach(() => vi.clearAllMocks());
  it.each(kinds)(
    "сохраняет пустой %s через /saves и выбирает новый parent-linked ref",
    async (kind, path) => {
      const current = { ...configuration, kind };
      const contentFormat = kind === "PROMPT_TEMPLATE" ? "TEXT" : "JSON";
      const result: ManagedConfigurationResult = {
        configuration: { ...current, version: 9 },
        revision: {
          ...revision,
          ref: "revision_new",
          revision: 4,
          parentRevisionRef: revision.ref,
          state: "DRAFT",
          contentFormat,
          content: "",
        },
      };
      client.post.mockResolvedValue(response(result));
      expect(
        await changeDraft(current, revision, { contentFormat, content: "" }),
      ).toEqual(result);
      expect(client.post.mock.calls[0]?.[0]).toMatchObject({
        url: `/api/v1/${path}/{configurationRef}/revisions/{revisionRef}/saves`,
        headers: { "If-Match": '"8"' },
        body: { contentFormat, content: "" },
      });
    },
  );
  it.each(kinds)(
    "отбрасывает exact old %s без создания ревизии",
    async (kind, path) => {
      const current = { ...configuration, kind };
      const result: ManagedConfigurationResult = {
        configuration: { ...current, version: 9 },
        revision: { ...revision, state: "DISCARDED" },
      };
      client.post.mockResolvedValue(response(result));
      expect(await changeDraft(current, revision)).toEqual(result);
      expect(client.post.mock.calls[0]?.[0].url).toBe(
        `/api/v1/${path}/{configurationRef}/revisions/{revisionRef}/discard`,
      );
      expect(client.post.mock.calls[0]?.[0].body).toBeUndefined();
    },
  );
  it("отклоняет неправильный parent и повторное использование old ref", async () => {
    client.post.mockResolvedValue(
      response({
        configuration: { ...configuration, version: 9 },
        revision: { ...revision, state: "DRAFT" },
      }),
    );
    await expect(
      changeDraft(configuration, revision, {
        contentFormat: "TEXT",
        content: "",
      }),
    ).rejects.toThrow("Managed draft receipt mismatch");
  });
  it("ограничивает UTF8 bytes, не считает Unicode code units байтами", async () => {
    await expect(
      changeDraft(configuration, revision, {
        contentFormat: "TEXT",
        content: "я".repeat(131073),
      }),
    ).rejects.toThrow();
    expect(client.post).not.toHaveBeenCalled();
  });
  it("не меняет Git-owned и terminal revisions", async () => {
    expect(
      canChangeDraft({ ...configuration, managedBy: "GIT" }, revision),
    ).toBe(false);
    for (const state of ["PUBLISHED", "SUPERSEDED", "DISCARDED"] as const) {
      await expect(
        changeDraft(configuration, { ...revision, state }),
      ).rejects.toThrow();
    }
    expect(client.post).not.toHaveBeenCalled();
  });
});
