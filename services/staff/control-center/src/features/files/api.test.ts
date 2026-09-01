import { describe, expect, it, vi } from "vitest";

import type { Artifact } from "@/shared/api/generated/openapi/types.gen";
import type { ArtifactImpact } from "@/shared/api/generated/openapi/types.gen";

const listArtifactsMock = vi.hoisted(() => vi.fn());
const deleteArtifactMock = vi.hoisted(() => vi.fn());
const getArtifactImpactMock = vi.hoisted(() => vi.fn());
const purgeArtifactMock = vi.hoisted(() => vi.fn());
const restoreArtifactMock = vi.hoisted(() => vi.fn());
const mutateMock = vi.hoisted(() =>
  vi.fn(
    async (
      request: (headers: Record<string, string>) => Promise<{ data: unknown }>,
      version?: number,
    ) => ({
      data: (
        await request({
          "Idempotency-Key": "idempotency-key",
          "If-Match": `"${String(version ?? 1)}"`,
          "X-CSRF-Token": "csrf-token",
        })
      ).data,
    }),
  ),
);
vi.mock("@/shared/api/generated/openapi/sdk.gen", () => ({
  deleteArtifact: deleteArtifactMock,
  getArtifactImpact: getArtifactImpactMock,
  listArtifacts: listArtifactsMock,
  purgeArtifact: purgeArtifactMock,
  restoreArtifact: restoreArtifactMock,
}));
vi.mock("@/shared/api/client", () => ({ requestSignal: () => undefined }));
vi.mock("@/shared/api/mutation", () => ({ mutate: mutateMock }));
vi.mock("@/shared/config/runtime", () => ({
  runtimeConfig: () => ({
    apiBaseUrl: "https://kodex.test",
    requestTimeoutMs: 30_000,
  }),
}));
vi.mock("@/shared/locale", () => ({ currentLocale: () => "ru" }));

import {
  deleteArtifactItem,
  loadArtifactImpact,
  loadArtifactPage,
  mutateArtifactsSequentially,
  purgeArtifactItem,
  restoreArtifactItem,
  uploadArtifactItem,
} from "@/features/files/api";

function artifact(ref: string, options: Partial<Artifact> = {}): Artifact {
  return {
    ref,
    version: 1,
    projectRef: "project_sales",
    fileName: `${ref}.pdf`,
    mediaType: "application/pdf",
    sizeBytes: 1024,
    digest: "sha256:test",
    scanState: "CLEAN",
    source: "CONTROL_CENTER",
    revision: 1,
    lifecycleState: "ACTIVE",
    agentBindings: [],
    previewAvailable: true,
    createdAt: "2026-08-28T09:00:00Z",
    nextActions: ["DOWNLOAD"],
    ...options,
  };
}

function impact(
  artifact: Artifact,
  action: ArtifactImpact["action"],
): ArtifactImpact {
  return {
    action,
    activeRuns: [],
    activeRunsTruncated: false,
    activeRuntimeCount: 0,
    artifactRef: artifact.ref,
    artifactVersion: artifact.version,
    attachmentCount: 0,
    bindingCount: 0,
    blockers: [],
    impactDigest: action === "DELETE" ? "d".repeat(64) : "p".repeat(64),
    permitted: true,
  };
}

class FakeXMLHttpRequest {
  static latest: FakeXMLHttpRequest | undefined;

  readonly headers = new Map<string, string>();
  readonly upload = {
    addEventListener: (
      _event: string,
      listener: (event: ProgressEvent) => void,
    ) => {
      this.progressListener = listener;
    },
  };
  method = "";
  url = "";
  withCredentials = false;
  timeout = 0;
  status = 0;
  statusText = "";
  responseText = "";
  body?: Document | XMLHttpRequestBodyInit | null;
  onload: (() => void) | null = null;
  onerror: (() => void) | null = null;
  ontimeout: (() => void) | null = null;
  onabort: (() => void) | null = null;
  private progressListener?: (event: ProgressEvent) => void;

  constructor() {
    FakeXMLHttpRequest.latest = this;
  }

  open(method: string, url: string): void {
    this.method = method;
    this.url = url;
  }

  setRequestHeader(name: string, value: string): void {
    this.headers.set(name, value);
  }

  getAllResponseHeaders(): string {
    return "content-type: application/json\r\n";
  }

  send(body?: Document | XMLHttpRequestBodyInit | null): void {
    this.body = body;
  }

  abort(): void {
    this.onabort?.();
  }

  progress(loaded: number, total: number): void {
    this.progressListener?.({ loaded, total } as ProgressEvent);
  }

  respond(status: number, body: unknown): void {
    this.status = status;
    this.statusText = "Created";
    this.responseText = JSON.stringify(body);
    this.onload?.();
  }
}

describe("loadArtifactPage", () => {
  it("загружает нефильтрованную корзину одним серверным cursor-запросом", async () => {
    const deleted = artifact("artifact_deleted", {
      lifecycleState: "DELETED",
      version: 2,
    });
    listArtifactsMock.mockResolvedValue({
      data: { items: [deleted], nextPageToken: "trash-next" },
      response: new Response(null, { status: 200 }),
    });
    const controller = new AbortController();

    const page = await loadArtifactPage(
      "project_sales",
      {
        cursor: "trash-before",
        query: "  результат  ",
        signal: controller.signal,
      },
      { allSources: true, lifecycleState: "DELETED" },
    );

    expect(listArtifactsMock).toHaveBeenCalledTimes(1);
    expect(listArtifactsMock).toHaveBeenCalledWith({
      path: { projectRef: "project_sales" },
      query: {
        lifecycleState: "DELETED",
        pageSize: 40,
        pageToken: "trash-before",
        query: "результат",
      },
      signal: controller.signal,
    });
    expect(page.items[0]?.artifact).toEqual(deleted);
    expect(page.nextCursor).toBe("trash-next");
  });

  it("передаёт серверный поиск и cursor, затем сохраняет следующий cursor", async () => {
    listArtifactsMock.mockImplementation(
      ({ query }: { query: { sourceKind: Artifact["source"] } }) =>
        Promise.resolve({
          data: {
            items: [
              artifact(`artifact_${query.sourceKind.toLocaleLowerCase()}`, {
                source: query.sourceKind,
              }),
            ],
            nextPageToken: `cursor-${query.sourceKind}`,
          },
          response: new Response(null, { status: 200 }),
        }),
    );
    const controller = new AbortController();

    const page = await loadArtifactPage(
      "project_sales",
      {
        query: "  договор  ",
        signal: controller.signal,
      },
      {
        lifecycleState: "ACTIVE",
        scanState: "CLEAN",
        sourceKinds: ["AGENT_RESULT", "INTEGRATION_RESULT"],
        type: "DOCUMENT",
      },
    );

    expect(listArtifactsMock).toHaveBeenCalledTimes(2);
    expect(listArtifactsMock).toHaveBeenCalledWith({
      path: { projectRef: "project_sales" },
      query: {
        lifecycleState: "ACTIVE",
        pageSize: 20,
        query: "договор",
        scanState: "CLEAN",
        sourceKind: "AGENT_RESULT",
        type: "DOCUMENT",
      },
      signal: controller.signal,
    });
    expect(page.items.map((item) => item.artifact.source)).toEqual([
      "AGENT_RESULT",
      "INTEGRATION_RESULT",
    ]);
    expect(JSON.parse(page.nextCursor ?? "{}")).toEqual({
      version: 1,
      sources: {
        AGENT_RESULT: "cursor-AGENT_RESULT",
        INTEGRATION_RESULT: "cursor-INTEGRATION_RESULT",
      },
    });
  });

  it("передаёт upload progress и mutation headers в потоковый HTTP request", async () => {
    vi.stubGlobal("XMLHttpRequest", FakeXMLHttpRequest);
    const file = new File(["test"], "отчёт.txt", { type: "text/plain" });
    const progress: Array<{ loadedBytes: number; totalBytes: number }> = [];
    const controller = new AbortController();

    const pending = uploadArtifactItem("project_sales", file, {
      signal: controller.signal,
      onProgress: (value) => progress.push(value),
    });
    const xhr = FakeXMLHttpRequest.latest;
    expect(xhr).toBeDefined();
    xhr?.progress(2, 4);
    xhr?.respond(201, artifact("artifact_uploaded"));

    await expect(pending).resolves.toMatchObject({ ref: "artifact_uploaded" });
    expect(progress).toEqual([
      { loadedBytes: 0, totalBytes: 4 },
      { loadedBytes: 2, totalBytes: 4 },
    ]);
    expect(xhr).toMatchObject({
      body: file,
      method: "POST",
      timeout: 30_000,
      url: "https://kodex.test/api/v1/projects/project_sales/artifacts",
      withCredentials: true,
    });
    expect(xhr?.headers).toEqual(
      new Map([
        ["Accept", "application/json"],
        ["Accept-Language", "ru"],
        ["Content-Type", "text/plain"],
        ["Idempotency-Key", "idempotency-key"],
        ["X-CSRF-Token", "csrf-token"],
        [
          "X-File-Name",
          Array.from(new TextEncoder().encode("отчёт.txt"), (byte) =>
            String.fromCharCode(byte),
          ).join(""),
        ],
        ["X-Kodex-Project-ID", "project_sales"],
      ]),
    );
  });

  it("выполняет lifecycle-команды с OCC и mutation headers", async () => {
    const active = artifact("artifact_one");
    const deleted = {
      ...active,
      lifecycleState: "DELETED" as const,
      nextActions: ["RESTORE"] as Artifact["nextActions"],
      version: 2,
    };
    deleteArtifactMock.mockResolvedValue({ data: deleted });
    restoreArtifactMock.mockResolvedValue({ data: active });
    purgeArtifactMock.mockResolvedValue({
      data: { artifactRef: active.ref, lifecycleState: "PURGED" },
    });

    const deleteImpact = impact(active, "DELETE");
    const purgeImpact = impact(deleted, "PURGE");

    await expect(deleteArtifactItem(active, deleteImpact)).resolves.toEqual(
      deleted,
    );
    await expect(restoreArtifactItem(deleted)).resolves.toEqual(active);
    await expect(purgeArtifactItem(deleted, purgeImpact)).resolves.toEqual({
      artifactRef: active.ref,
      lifecycleState: "PURGED",
    });

    expect(deleteArtifactMock).toHaveBeenCalledWith(
      expect.objectContaining({
        path: { artifactRef: active.ref },
        headers: {
          "Idempotency-Key": "idempotency-key",
          "If-Match": '"1"',
          "X-CSRF-Token": "csrf-token",
          "X-Impact-Digest": deleteImpact.impactDigest,
        },
      }),
    );
    expect(restoreArtifactMock).toHaveBeenCalledWith(
      expect.objectContaining({
        path: { artifactRef: active.ref },
        headers: {
          "Idempotency-Key": "idempotency-key",
          "If-Match": '"2"',
          "X-CSRF-Token": "csrf-token",
        },
      }),
    );
    expect(purgeArtifactMock).toHaveBeenCalledWith(
      expect.objectContaining({
        path: { artifactRef: active.ref },
        headers: {
          "Idempotency-Key": "idempotency-key",
          "If-Match": '"2"',
          "X-CSRF-Token": "csrf-token",
          "X-Impact-Digest": purgeImpact.impactDigest,
        },
      }),
    );
  });

  it("закрыто отклоняет destructive-команду с impact другой ревизии", async () => {
    const current = artifact("artifact_one");
    const staleImpact = {
      ...impact(current, "DELETE"),
      artifactVersion: current.version + 1,
    };

    await expect(deleteArtifactItem(current, staleImpact)).rejects.toThrow(
      "Artifact impact does not authorize this mutation",
    );
    expect(deleteArtifactMock).not.toHaveBeenCalled();
  });

  it("последовательно обрабатывает каждый файл и возвращает отдельный receipt", async () => {
    const first = artifact("artifact_one");
    const second = artifact("artifact_two");
    const third = artifact("artifact_three");
    const command = vi.fn((current: Artifact) =>
      current.ref === second.ref
        ? Promise.reject(new Error("second failed"))
        : Promise.resolve(),
    );

    const receipts = await mutateArtifactsSequentially(
      [first, second, third],
      command,
    );

    expect(command.mock.calls.map(([current]) => current.ref)).toEqual([
      first.ref,
      second.ref,
      third.ref,
    ]);
    expect(receipts).toMatchObject([
      { artifact: first, status: "SUCCEEDED" },
      {
        artifact: second,
        problem: { code: "UNKNOWN", status: 0 },
        status: "FAILED",
      },
      { artifact: third, status: "SUCCEEDED" },
    ]);
  });
});

describe("loadArtifactImpact", () => {
  it("принимает только preflight точной версии artifact", async () => {
    getArtifactImpactMock.mockResolvedValue({
      data: {
        action: "DELETE",
        activeRuns: [],
        activeRunsTruncated: false,
        activeRuntimeCount: 0,
        artifactRef: "artifact_file",
        artifactVersion: 1,
        attachmentCount: 0,
        bindingCount: 0,
        blockers: [],
        impactDigest: "a".repeat(64),
        permitted: true,
      },
      response: new Response(null, { status: 200 }),
    });

    await expect(
      loadArtifactImpact(artifact("artifact_file"), "DELETE"),
    ).resolves.toMatchObject({ permitted: true });
    expect(getArtifactImpactMock).toHaveBeenCalledWith({
      path: { artifactRef: "artifact_file" },
      query: { action: "DELETE" },
      signal: undefined,
    });

    getArtifactImpactMock.mockResolvedValueOnce({
      data: {
        action: "DELETE",
        activeRuns: [],
        activeRunsTruncated: false,
        activeRuntimeCount: 0,
        artifactRef: "artifact_file",
        artifactVersion: 2,
        attachmentCount: 0,
        bindingCount: 0,
        blockers: [],
        impactDigest: "b".repeat(64),
        permitted: true,
      },
      response: new Response(null, { status: 200 }),
    });
    await expect(
      loadArtifactImpact(artifact("artifact_file"), "DELETE"),
    ).rejects.toThrow("Artifact impact does not match the requested revision");
  });
});
