import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  prepareSttActivation,
  activateStt,
  readEffectiveStt,
} from "./stt-activation";
import * as api from "./api";
import * as sdk from "@/shared/api/generated/openapi/sdk.gen";
import { AppProblem } from "@/shared/api/problem";
import type {
  ManagedConfiguration,
  ManagedConfigurationRevision,
} from "@/shared/api/generated/openapi/types.gen";
vi.mock("./api", () => ({ history: vi.fn(), impact: vi.fn() }));
vi.mock("@/shared/api/generated/openapi/sdk.gen", () => ({
  getSystemSttConfiguration: vi.fn(),
  getManagedConfigurationImpact: vi.fn(),
  rebindSystemSttConsumers: vi.fn(),
}));
vi.mock("@/shared/api/client", () => ({
  requestSignal: (s: AbortSignal) => s,
}));
vi.mock("@/shared/api/mutation", () => ({
  etag: (v: number) => `"${String(v)}"`,
  mutate: async (fn: (h: object) => Promise<unknown>) =>
    fn({ "X-CSRF-Token": "synthetic", "Idempotency-Key": "synthetic" }),
}));
const configuration = {
  ref: "configuration_target",
  kind: "SYSTEM_STT",
  managedBy: "UI",
  version: 3,
  name: "Целевая",
} as ManagedConfiguration;
const revision = {
  ref: "revision_target",
  state: "PUBLISHED",
  revision: 2,
} as ManagedConfigurationRevision;
const signal = new AbortController().signal;
const ok = <T>(data: T) => ({
  data,
  response: new Response(null, { status: 200 }),
});
const missing = {
  error: { code: "NOT_FOUND", status: 404 },
  response: new Response(null, { status: 404 }),
};
const target = {
  configurationRef: configuration.ref,
  targetRevisionRef: revision.ref,
  digest: "a".repeat(64),
  total: 0,
  consumers: [],
};
beforeEach(() => {
  vi.resetAllMocks();
  vi.mocked(api.history).mockResolvedValue({ configuration } as never);
  vi.mocked(api.impact).mockResolvedValue(target);
  vi.mocked(sdk.getSystemSttConfiguration).mockResolvedValue(missing as never);
});
describe("активация STT", () => {
  it("доказывает view/manage до global absence и передаёт ABSENT без pins", async () => {
    const plan = await prepareSttActivation(configuration, revision, signal);
    expect(plan.consumer).toEqual({
      kind: "STT_SERVICE",
      ref: "stt-tts-service",
      expectedAbsent: true,
    });
    expect(vi.mocked(api.impact).mock.invocationCallOrder[0]).toBeLessThan(
      vi.mocked(sdk.getSystemSttConfiguration).mock.invocationCallOrder[0] ?? 0,
    );
    await activateStt(plan, "key", signal);
    expect(sdk.rebindSystemSttConsumers).toHaveBeenCalledOnce();
    expect(sdk.rebindSystemSttConsumers).toHaveBeenCalledWith(
      expect.objectContaining({
        body: { impactDigest: target.digest, consumers: [plan.consumer] },
      }),
    );
    expect(
      vi.mocked(sdk.rebindSystemSttConsumers).mock.calls[0]?.[0].headers[
        "If-Match"
      ],
    ).toBe('"3"');
  });
  it("пустой target impact не скрывает чужую текущую конфигурацию, MATCH берёт binding version", async () => {
    vi.mocked(sdk.getSystemSttConfiguration).mockResolvedValue(
      ok({
        configurationRef: "configuration_old",
        revisionRef: "revision_old",
        revision: 2,
        ready: false,
      }) as never,
    );
    vi.mocked(api.history)
      .mockResolvedValueOnce({ configuration } as never)
      .mockResolvedValueOnce({
        configuration: {
          ...configuration,
          ref: "configuration_old",
          name: "Прежняя",
        },
      } as never);
    const binding = {
      kind: "STT_SERVICE",
      ref: "stt-tts-service",
      revisionRef: "revision_old",
      version: 19,
    };
    vi.mocked(sdk.getManagedConfigurationImpact).mockResolvedValue(
      ok({
        ...target,
        configurationRef: "configuration_old",
        targetRevisionRef: "revision_old",
        total: 1,
        consumers: [binding],
      }) as never,
    );
    const plan = await prepareSttActivation(configuration, revision, signal);
    expect(plan.consumer).toEqual({ ...binding, expectedAbsent: false });
    expect(plan.current?.ready).toBe(false);
  });
  it.each([403, 500, 503])("не считает HTTP%s отсутствием", async (status) => {
    vi.mocked(sdk.getSystemSttConfiguration).mockResolvedValue({
      error: { code: "UNAVAILABLE", status },
      response: new Response(null, { status }),
    } as never);
    await expect(
      prepareSttActivation(configuration, revision, signal),
    ).rejects.toBeInstanceOf(AppProblem);
    expect(sdk.rebindSystemSttConsumers).not.toHaveBeenCalled();
  });
  it.each(["GIT", "changed-version", "project"])(
    "отклоняет %s до mutation",
    async (mode) => {
      vi.mocked(api.history).mockResolvedValue({
        configuration: {
          ...configuration,
          ...(mode === "GIT"
            ? { managedBy: "GIT" }
            : mode === "project"
              ? { projectRef: "project_other" }
              : { version: 4 }),
        },
      } as never);
      await expect(
        prepareSttActivation(configuration, revision, signal),
      ).rejects.toThrow();
      expect(sdk.getSystemSttConfiguration).not.toHaveBeenCalled();
    },
  );
  it("не превращает unknown404 в отсутствие", async () => {
    vi.mocked(sdk.getSystemSttConfiguration).mockResolvedValue({
      error: { code: "UNKNOWN", status: 404 },
      response: new Response(null, { status: 404 }),
    } as never);
    await expect(readEffectiveStt(signal)).rejects.toThrow();
  });
  it("отклоняет гонку current revision и actual binding", async () => {
    vi.mocked(sdk.getSystemSttConfiguration).mockResolvedValue(
      ok({
        configurationRef: configuration.ref,
        revisionRef: "revision_old",
      }) as never,
    );
    vi.mocked(sdk.getManagedConfigurationImpact).mockResolvedValue(
      ok({
        ...target,
        targetRevisionRef: "revision_old",
        total: 1,
        consumers: [
          {
            kind: "STT_SERVICE",
            ref: "stt-tts-service",
            revisionRef: "revision_raced",
            version: 5,
          },
        ],
      }) as never,
    );
    await expect(
      prepareSttActivation(configuration, revision, signal),
    ).rejects.toThrow("changed during preparation");
  });
});
