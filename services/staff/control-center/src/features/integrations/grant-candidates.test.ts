import { beforeEach, expect, it, vi } from "vitest";
import type {
  IntegrationGrantConnectionCandidatePage,
  IntegrationGrantProjectCandidatePage,
} from "@/shared/api/generated/openapi/types.gen";
const sdk = vi.hoisted(() => ({
  connections: vi.fn(),
  projects: vi.fn(),
  recipients: vi.fn(),
  capabilities: vi.fn(),
}));
vi.mock("@/shared/api/generated/openapi/sdk.gen", () => ({
  listIntegrationGrantConnectionCandidates: sdk.connections,
  listIntegrationGrantProjectCandidates: sdk.projects,
  listIntegrationGrantRecipientCandidates: sdk.recipients,
  listIntegrationGrantCapabilityCandidates: sdk.capabilities,
}));
vi.mock("@/shared/api/client", () => ({
  requestSignal: (signal: AbortSignal) => signal,
}));
import {
  checkedCandidatePage,
  connectionCandidates,
  projectCandidates,
  recipientCandidates,
  capabilityCandidates,
} from "./grant-candidates";
const digest = "a".repeat(64);
const page: IntegrationGrantConnectionCandidatePage = {
  context: {},
  contextDigest: digest,
  pins: { contextDigest: digest },
  total: 12,
  nextPageToken: "cursor-one",
  items: [
    {
      connectionRef: "connection",
      name: "Подключение",
      definitionKey: "github",
      providerName: "GitHub",
      credentialKind: "TOKEN",
      resourceScope: {},
      grantable: false,
      usable: false,
      reason: "CONNECTION_UNAVAILABLE",
      pins: {
        contextDigest: digest,
        connectionVersion: 7,
        definitionVersion: "2.3",
        definitionDigest: "b".repeat(64),
      },
    },
  ],
};
const firstCandidate = page.items[0];
if (!firstCandidate) throw new Error("Candidate fixture is missing");
beforeEach(() => vi.resetAllMocks());
it("сохраняет недоступную строку, server total и один owner cursor запрос", async () => {
  sdk.connections.mockResolvedValue({
    data: page,
    response: new Response(null),
  });
  const load = connectionCandidates({ purpose: "GRANT" });
  const signal = new AbortController().signal;
  const result = await load("repo", undefined, signal);
  expect(result.items[0]?.grantable).toBe(false);
  expect(result.total).toBe(12);
  expect(sdk.connections).toHaveBeenCalledExactlyOnceWith({
    query: {
      purpose: "GRANT",
      query: "repo",
      pageToken: undefined,
      pageSize: 40,
    },
    signal,
    cache: "no-store",
  });
  expect(sdk.projects).not.toHaveBeenCalled();
});
it("отклоняет чужой context, ложную readiness и duplicate rows", () => {
  expect(() =>
    checkedCandidatePage(
      { ...page, context: { connectionRef: "foreign" } },
      {},
      "GRANT",
    ),
  ).toThrow();
  expect(() =>
    checkedCandidatePage(
      { ...page, items: [{ ...firstCandidate, grantable: true }] },
      {},
      "GRANT",
    ),
  ).toThrow();
  expect(() =>
    checkedCandidatePage(
      { ...page, items: [...page.items, ...page.items] },
      {},
      "GRANT",
    ),
  ).toThrow();
});
it("USE не получает grant authority и сохраняет точного получателя", () => {
  const context = {
    projectRef: "project",
    recipientKind: "AGENT" as const,
    recipientRef: "agent",
    capabilityKey: "github.read",
  };
  const use = {
    ...page,
    context,
    items: [
      {
        ...firstCandidate,
        grantable: false,
        usable: true,
        reason: "READY" as const,
      },
    ],
  };
  expect(checkedCandidatePage(use, context, "USE").items[0]?.usable).toBe(true);
  expect(() => checkedCandidatePage(use, context, "GRANT")).toThrow();
  expect(() =>
    checkedCandidatePage(use, { ...context, recipientRef: "other" }, "USE"),
  ).toThrow();
});
it("закрывает старый cursor при смене digest и позволяет новое первое чтение", async () => {
  const load = connectionCandidates({ purpose: "GRANT" });
  const signal = new AbortController().signal;
  sdk.connections.mockResolvedValueOnce({
    data: page,
    response: new Response(null),
  });
  await load("", undefined, signal);
  const changed = {
    ...page,
    contextDigest: "c".repeat(64),
    pins: { contextDigest: "c".repeat(64) },
    nextPageToken: undefined,
  };
  sdk.connections.mockResolvedValue({
    data: changed,
    response: new Response(null),
  });
  await expect(load("", "cursor-one", signal)).rejects.toThrow(
    "snapshot changed",
  );
  await expect(load("", undefined, signal)).resolves.toMatchObject({
    contextDigest: changed.contextDigest,
  });
});
it("зависимый Project не меняет выбранную connection version", async () => {
  const pins = firstCandidate.pins;
  const projects: IntegrationGrantProjectCandidatePage = {
    context: { connectionRef: "connection" },
    contextDigest: digest,
    pins,
    total: 1,
    items: [
      {
        projectRef: "project",
        name: "Проект",
        grantable: true,
        reason: "READY",
        pins: { ...pins, projectVersion: 3 },
      },
    ],
  };
  sdk.projects.mockResolvedValue({
    data: projects,
    response: new Response(null),
  });
  const load = projectCandidates({ connectionRef: "connection" }, pins);
  await expect(
    load("", undefined, new AbortController().signal),
  ).resolves.toEqual(projects);
  sdk.projects.mockResolvedValue({
    data: { ...projects, pins: { ...pins, connectionVersion: 8 } },
    response: new Response(null),
  });
  await expect(
    load("", undefined, new AbortController().signal),
  ).rejects.toThrow("prefix changed");
});

// Один и тот же транспортный контракт проверяется для четырёх разных SDK operations.
const candidateRequests = [
  {
    name: "connections",
    sdk: sdk.connections,
    load: () => connectionCandidates({ purpose: "GRANT" }),
    query: { purpose: "GRANT" },
    context: {},
  },
  {
    name: "projects",
    sdk: sdk.projects,
    load: () => projectCandidates({ connectionRef: "connection" }),
    query: { connectionRef: "connection" },
    context: { connectionRef: "connection" },
  },
  {
    name: "recipients",
    sdk: sdk.recipients,
    load: () =>
      recipientCandidates({
        connectionRef: "connection",
        projectRef: "project",
        recipientKind: "AGENT",
      }),
    query: {
      connectionRef: "connection",
      projectRef: "project",
      recipientKind: "AGENT",
    },
    context: {
      connectionRef: "connection",
      projectRef: "project",
      recipientKind: "AGENT",
    },
  },
  {
    name: "capabilities",
    sdk: sdk.capabilities,
    load: () =>
      capabilityCandidates({
        connectionRef: "connection",
        projectRef: "project",
        recipientKind: "AGENT",
        recipientRef: "agent",
      }),
    query: {
      connectionRef: "connection",
      projectRef: "project",
      recipientKind: "AGENT",
      recipientRef: "agent",
    },
    context: {
      connectionRef: "connection",
      projectRef: "project",
      recipientKind: "AGENT",
      recipientRef: "agent",
    },
  },
];
it.each(candidateRequests)(
  "$name: лениво вызывает только выбранный SDK с точными query/signal/no-store",
  async (scenario) => {
    scenario.sdk.mockResolvedValue({
      data: {
        context: scenario.context,
        contextDigest: digest,
        pins: { contextDigest: digest },
        total: 0,
        items: [],
      },
      response: new Response(null),
    });
    const load = scenario.load();
    for (const request of Object.values(sdk))
      expect(request).not.toHaveBeenCalled();
    const signal = new AbortController().signal;
    await expect(load("find", undefined, signal, 23)).resolves.toMatchObject({
      total: 0,
      items: [],
    });
    expect(scenario.sdk).toHaveBeenCalledExactlyOnceWith({
      query: {
        ...scenario.query,
        query: "find",
        pageToken: undefined,
        pageSize: 23,
      },
      signal,
      cache: "no-store",
    });
    for (const request of Object.values(sdk)) {
      if (request !== scenario.sdk) expect(request).not.toHaveBeenCalled();
    }
  },
);
it.each(candidateRequests)(
  "$name: возвращает исходную ошибку без повторного запроса",
  async (scenario) => {
    const error = new Error("synthetic candidate read failure");
    scenario.sdk.mockRejectedValue(error);
    await expect(
      scenario.load()("", undefined, new AbortController().signal),
    ).rejects.toBe(error);
    expect(scenario.sdk).toHaveBeenCalledTimes(1);
  },
);
it.each(candidateRequests)(
  "$name: отклоняет ответ при отмене во время запроса",
  async (scenario) => {
    scenario.sdk.mockResolvedValue({
      data: {
        context: scenario.context,
        contextDigest: digest,
        pins: { contextDigest: digest },
        total: 0,
        items: [],
      },
      response: new Response(null),
    });
    const controller = new AbortController();
    const pending = scenario.load()("", undefined, controller.signal);
    const reason = new Error("synthetic candidate request cancelled");
    controller.abort(reason);
    await expect(pending).rejects.toBe(reason);
    expect(scenario.sdk).toHaveBeenCalledTimes(1);
  },
);
