import { beforeEach, describe, expect, it, vi } from "vitest";
import type {
  IntegrationConnection,
  ManagedConfiguration,
  ManagedConfigurationRevision,
} from "@/shared/api/generated/openapi/types.gen";
import { getIntegrationConnection } from "@/shared/api/generated/openapi/sdk.gen";
import * as api from "./api";
import {
  bindIntegrationConnection,
  prepareIntegrationConnectionBinding,
} from "./integration-binding";

vi.mock("./api", () => ({
  history: vi.fn(),
  impact: vi.fn(),
  rebind: vi.fn(),
}));
vi.mock("@/shared/api/generated/openapi/sdk.gen", () => ({
  getIntegrationConnection: vi.fn(),
}));
vi.mock("@/shared/api/client", () => ({
  requestSignal: (signal: AbortSignal) => signal,
}));

const configuration = {
  ref: "configuration_target",
  kind: "INTEGRATION_DEFINITION",
  managedBy: "UI",
  version: 7,
  archived: false,
} as ManagedConfiguration;
const revision = {
  ref: "revision_target",
  state: "PUBLISHED",
  digest: "c".repeat(64),
  contentFormat: "JSON",
} as ManagedConfigurationRevision;
const binding = {
  state: "MATCH",
  configurationRef: "configuration_source",
  revisionRef: "revision_source",
  bindingVersion: 19,
} as const;
const connection = {
  ref: "connection_selected",
  definitionKey: "synthetic",
  version: 52,
  state: "CONNECTED",
  definitionConfigurationBinding: binding,
} as IntegrationConnection;
const projection = {
  configurationRef: configuration.ref,
  targetRevisionRef: revision.ref,
  digest: "a".repeat(64),
  total: 0,
  consumers: [],
};
const ok = (data: unknown) => ({
  data,
  response: new Response(null, { status: 200 }),
});
const prepare = (signal = new AbortController().signal) =>
  prepareIntegrationConnectionBinding(
    configuration,
    revision,
    connection.ref,
    connection.definitionKey,
    signal,
  );

beforeEach(() => {
  vi.resetAllMocks();
  vi.mocked(api.history).mockResolvedValue({
    configuration,
    items: [revision],
  } as never);
  vi.mocked(getIntegrationConnection).mockResolvedValue(
    ok(connection) as never,
  );
  vi.mocked(api.impact).mockResolvedValue(projection);
  vi.mocked(api.rebind).mockResolvedValue({ configuration, revision } as never);
});

describe("перепривязка подключения между конфигурациями", () => {
  it("пустой target impact использует точные pins прежней связи, не версию подключения", async () => {
    const plan = await prepare();
    expect(plan.input).toEqual({
      impactDigest: projection.digest,
      consumers: [
        {
          kind: "INTEGRATION_CONNECTION",
          ref: connection.ref,
          expectedAbsent: false,
          revisionRef: binding.revisionRef,
          version: binding.bindingVersion,
        },
      ],
    });
    expect(api.rebind).not.toHaveBeenCalled();
    await bindIntegrationConnection(plan, new AbortController().signal);
    expect(api.history).toHaveBeenCalledTimes(2);
    expect(getIntegrationConnection).toHaveBeenCalledTimes(2);
    expect(api.impact).toHaveBeenCalledTimes(2);
    expect(api.rebind).toHaveBeenCalledExactlyOnceWith(
      configuration,
      revision,
      plan.input,
    );
    const lastRead =
      vi.mocked(getIntegrationConnection).mock.invocationCallOrder[1] ?? 0;
    const lastImpact = vi.mocked(api.impact).mock.invocationCallOrder[1] ?? 0;
    expect(lastRead).toBeLessThan(lastImpact);
    expect(lastImpact).toBeLessThan(
      vi.mocked(api.rebind).mock.invocationCallOrder[0] ?? 0,
    );
    expect(getIntegrationConnection).toHaveBeenCalledWith(
      expect.objectContaining({
        path: { connectionRef: connection.ref },
        cache: "no-store",
      }),
    );
  });

  it("передаёт expectedAbsent только после явного authoritative ABSENT", async () => {
    vi.mocked(getIntegrationConnection).mockResolvedValue(
      ok({
        ...connection,
        definitionConfigurationBinding: { state: "ABSENT" },
      }) as never,
    );
    const plan = await prepare();
    expect(plan.input.consumers).toEqual([
      {
        kind: "INTEGRATION_CONNECTION",
        ref: connection.ref,
        expectedAbsent: true,
      },
    ]);
    await bindIntegrationConnection(plan, new AbortController().signal);
    expect(api.rebind).toHaveBeenCalledOnce();
  });

  it.each([
    undefined,
    { state: "UNKNOWN" },
    { state: "ABSENT", revisionRef: binding.revisionRef },
    { ...binding, bindingVersion: 0 },
    { ...binding, bindingVersion: Number.MAX_SAFE_INTEGER + 1 },
    { ...binding, revisionRef: "" },
    { ...binding, configurationRef: configuration.ref },
  ])(
    "не превращает отсутствующее/повреждённое чтение %j в absence",
    async (value) => {
      vi.mocked(getIntegrationConnection).mockResolvedValue(
        ok({ ...connection, definitionConfigurationBinding: value }) as never,
      );
      await expect(prepare()).rejects.toThrow();
      expect(api.impact).not.toHaveBeenCalled();
      expect(api.rebind).not.toHaveBeenCalled();
    },
  );

  it.each([403, 404, 500, 503])(
    "не подменяет ошибку GET%s отсутствием",
    async (status) => {
      vi.mocked(getIntegrationConnection).mockResolvedValue({
        error: { code: status === 404 ? "NOT_FOUND" : "UNAVAILABLE", status },
        response: new Response(null, { status }),
      } as never);
      await expect(prepare()).rejects.toThrow();
      expect(api.rebind).not.toHaveBeenCalled();
    },
  );

  it.each([
    { ref: "configuration_other" },
    { kind: "SYSTEM_STT" },
    { version: 8 },
    { archived: true },
    { managedBy: "GIT" },
    { projectRef: "project_other" },
  ])("отклоняет drift target %j до чтения подключения", async (change) => {
    vi.mocked(api.history).mockResolvedValue({
      configuration: { ...configuration, ...change },
    } as never);
    await expect(prepare()).rejects.toThrow();
    expect(getIntegrationConnection).not.toHaveBeenCalled();
    expect(api.rebind).not.toHaveBeenCalled();
  });

  it.each([
    { ref: "connection_other" },
    { definitionKey: "other" },
    { state: "DELETED" },
    { version: 0 },
  ])("отклоняет другой/непригодный connection %j", async (change) => {
    vi.mocked(getIntegrationConnection).mockResolvedValue(
      ok({ ...connection, ...change }) as never,
    );
    await expect(prepare()).rejects.toThrow();
    expect(api.rebind).not.toHaveBeenCalled();
  });

  it.each([
    { configurationRef: "configuration_other" },
    { targetRevisionRef: "revision_other" },
    { digest: "invalid" },
    { total: -1 },
    {
      consumers: [
        {
          kind: "INTEGRATION_CONNECTION",
          ref: connection.ref,
          revisionRef: revision.ref,
          version: 2,
        },
      ],
      total: 1,
    },
  ])("отклоняет несовпавший target impact %j", async (change) => {
    vi.mocked(api.impact).mockResolvedValue({
      ...projection,
      ...change,
    } as never);
    await expect(prepare()).rejects.toThrow();
    expect(api.rebind).not.toHaveBeenCalled();
  });

  it.each(["binding-version", "binding-revision", "absence", "digest"])(
    "не применяет изменившийся %s между выбором и подтверждением",
    async (change) => {
      const plan = await prepare();
      if (change === "digest")
        vi.mocked(api.impact).mockResolvedValue({
          ...projection,
          digest: "b".repeat(64),
        });
      else
        vi.mocked(getIntegrationConnection).mockResolvedValue(
          ok({
            ...connection,
            definitionConfigurationBinding:
              change === "absence"
                ? { state: "ABSENT" }
                : {
                    ...binding,
                    ...(change === "binding-version"
                      ? { bindingVersion: 20 }
                      : { revisionRef: "revision_new" }),
                  },
          }) as never,
        );
      await expect(
        bindIntegrationConnection(plan, new AbortController().signal),
      ).rejects.toThrow("changed before confirmation");
      expect(api.rebind).not.toHaveBeenCalled();
    },
  );

  it("закрытие picker/owner lifetime между чтениями не запускает mutation", async () => {
    const controller = new AbortController();
    const plan = await prepare(controller.signal);
    controller.abort();
    await expect(
      bindIntegrationConnection(plan, new AbortController().signal),
    ).rejects.toThrow();
    expect(api.history).toHaveBeenCalledTimes(1);
    expect(api.rebind).not.toHaveBeenCalled();
  });

  it("не продолжает подготовку после закрытия lifetime во время чтения", async () => {
    const controller = new AbortController();
    vi.mocked(getIntegrationConnection).mockImplementation(() => {
      controller.abort();
      return Promise.resolve(ok(connection) as never);
    });
    await expect(prepare(controller.signal)).rejects.toThrow();
    expect(api.impact).not.toHaveBeenCalled();
    expect(api.rebind).not.toHaveBeenCalled();
  });

  it("конфликт или неизвестный mutation outcome не получает автоматический повтор", async () => {
    const plan = await prepare();
    vi.mocked(api.rebind).mockRejectedValue(new Error("Unknown outcome"));
    await expect(
      bindIntegrationConnection(plan, new AbortController().signal),
    ).rejects.toThrow("Unknown outcome");
    expect(api.rebind).toHaveBeenCalledOnce();
  });

  it.each([
    { items: [] },
    { items: [{ ...revision, state: "SUPERSEDED" }] },
    { items: [{ ...revision, digest: "d".repeat(64) }] },
    { items: [{ ...revision, ref: "revision_other" }] },
  ])(
    "не подтверждает отсутствующую или изменённую target revision %j",
    async ({ items }) => {
      vi.mocked(api.history).mockResolvedValue({
        configuration,
        items,
      } as never);
      await expect(prepare()).rejects.toThrow("revision changed");
      expect(getIntegrationConnection).not.toHaveBeenCalled();
      expect(api.rebind).not.toHaveBeenCalled();
    },
  );
});
