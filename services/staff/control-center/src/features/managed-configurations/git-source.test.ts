import { beforeEach, describe, expect, it, vi } from "vitest";
import type {
  ManagedConfiguration,
  RoleImageGitSourceInput,
} from "@/shared/api/generated/openapi/types.gen";
const client = vi.hoisted(() => ({ post: vi.fn(), get: vi.fn() }));
vi.mock("@/shared/api/generated/openapi/client.gen", () => ({ client }));
vi.mock("@/shared/api/client", () => ({
  requestSignal: (signal: AbortSignal) => signal,
}));
vi.mock("@/shared/api/mutation", async () => {
  const { unwrap } = await import("@/shared/api/problem");
  return {
    etag: (version: number) => `"${String(version)}"`,
    idempotencyKey: () => "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
    mutateWithRetry: (
      request: (
        headers: Record<string, string>,
      ) => Parameters<typeof unwrap>[0],
      version: number,
      key: string,
    ) =>
      unwrap(
        request({
          "If-Match": `"${String(version)}"`,
          "Idempotency-Key": key,
          "X-CSRF-Token": "synthetic-csrf",
        }),
      ),
  };
});
import {
  executeGitSource,
  forgetGitSource,
  gitSourceConnection,
  gitSourceRecoveryKey,
  pendingGitSource,
  prepareGitSource,
  rememberGitSource,
} from "./git-source";
const configuration: ManagedConfiguration = {
  ref: "configuration_synthetic",
  version: 8,
  kind: "ROLE_IMAGE",
  name: "Synthetic",
  managedBy: "UI",
  source: "UI",
  sourceRevision: "",
  updatedAt: "2026-09-05T00:00:00Z",
};
const input: RoleImageGitSourceInput = {
  connectionRef: "connection_synthetic",
  expectedConnectionVersion: 12,
  repositoryRef: "owner/repository",
  refName: "main",
  path: "role-image.yaml",
  contentFormat: "YAML",
};
function storage() {
  const values = new Map<string, string>();
  return {
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => {
      values.set(key, value);
    },
    removeItem: (key: string) => {
      values.delete(key);
    },
  };
}
function receipt() {
  return {
    ...configuration,
    version: 9,
    managedBy: "GIT",
    gitSource: { state: "QUEUED" },
  };
}
describe("Git source mutation recovery", () => {
  beforeEach(() => vi.clearAllMocks());
  it("сохраняет исходный запрос при закрытии формы и не допускает подмену intent", () => {
    const store = storage();
    const original = { ...input };
    const attempt = prepareGitSource(configuration, original);
    original.path = "changed.yaml";
    rememberGitSource(attempt, store);
    const restored = pendingGitSource(configuration.ref, store);
    expect(restored).toEqual(attempt);
    expect(restored?.input?.path).toBe("role-image.yaml");
    expect(() => rememberGitSource({ ...attempt, version: 9 }, store)).toThrow(
      "intent changed",
    );
    forgetGitSource(
      { ...attempt, key: "ffffffff-bbbb-cccc-dddd-eeeeeeeeeeee" },
      store,
    );
    expect(pendingGitSource(configuration.ref, store)).toEqual(attempt);
    forgetGitSource(attempt, store);
    expect(store.getItem(gitSourceRecoveryKey)).toBeNull();
  });
  it.each([
    { privateCredential: "forbidden" },
    { input: { ...input, contentFormat: "TOML" } },
    { version: 0 },
    { input: { ...input, repositoryRef: "я".repeat(129) } },
  ])("закрыто отклоняет повреждённые recovery metadata %j", (extra) => {
    const store = storage();
    store.setItem(
      gitSourceRecoveryKey,
      JSON.stringify([{ ...prepareGitSource(configuration, input), ...extra }]),
    );
    expect(() => pendingGitSource(configuration.ref, store)).toThrow();
    expect(client.post).not.toHaveBeenCalled();
  });
  it.each([
    ["ROLE_IMAGE", "role-image-configurations"],
    ["INTEGRATION_DEFINITION", "integration-definition-configurations"],
  ] as const)(
    "отправляет специализированные Configure/Refresh %s с исходными OCC и ключом",
    async (kind, resource) => {
      for (const configure of [true, false]) {
        const attempt = prepareGitSource(
          { ...configuration, kind },
          configure ? input : undefined,
        );
        client.post.mockResolvedValue({
          data: { ...receipt(), kind },
          response: new Response(null),
        });
        const signal = new AbortController().signal;
        await executeGitSource(attempt, signal);
        await executeGitSource(attempt, signal);
        const options = client.post.mock.lastCall?.[0] as { body?: unknown };
        expect(options).toMatchObject({
          url: `/api/v1/${resource}/{configurationRef}/git-source${configure ? "" : "/refresh"}`,
          path: { configurationRef: configuration.ref },
          headers: { "If-Match": '"8"', "Idempotency-Key": attempt.key },
          signal,
        });
        expect(options.body).toEqual(configure ? input : undefined);
      }
    },
  );
  it.each([
    { ref: "foreign" },
    { kind: "PROMPT_TEMPLATE" },
    { version: 8 },
    { managedBy: "UI" },
    { gitSource: { state: "READY" } },
  ])("не выдаёт несовпавшую квитанцию за успех %j", async (extra) => {
    client.post.mockResolvedValue({
      data: { ...receipt(), ...extra },
      response: new Response(null),
    });
    await expect(
      executeGitSource(
        prepareGitSource(configuration, input),
        new AbortController().signal,
      ),
    ).rejects.toThrow("receipt mismatch");
  });
  it("проверяет идентичность и версию выбранного соединения", async () => {
    client.get.mockResolvedValue({
      data: { ref: "foreign", version: 1 },
      response: new Response(null),
    });
    await expect(
      gitSourceConnection(input.connectionRef, new AbortController().signal),
    ).rejects.toThrow("readback mismatch");
  });
});
