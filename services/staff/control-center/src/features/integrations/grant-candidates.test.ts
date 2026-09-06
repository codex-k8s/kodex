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
