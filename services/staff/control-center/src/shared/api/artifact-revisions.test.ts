import { beforeEach, describe, expect, it, vi } from "vitest";
import type { ArtifactRevision } from "./generated/openapi/types.gen";

const sdk = vi.hoisted(() => ({
  downloadArtifactRevision: vi.fn(),
  getArtifact: vi.fn(),
  getArtifactRevision: vi.fn(),
  listArtifactRevisions: vi.fn(),
}));
vi.mock("./generated/openapi/sdk.gen", () => sdk);
vi.mock("./client", () => ({ requestSignal: (signal: AbortSignal) => signal }));
vi.mock("./read-retry", async (importOriginal) => {
  const actual = await importOriginal<typeof import("./read-retry")>();
  return { ...actual, readWithRetry: vi.fn(actual.readWithRetry) };
});

import { readWithRetry } from "./read-retry";

import {
  assertArtifactRevision,
  downloadRevisionContent,
  loadArtifactRevisionPage,
  readArtifactRevision,
  readProjectArtifact,
} from "./artifact-revisions";

function revision(options: Partial<ArtifactRevision> = {}): ArtifactRevision {
  return {
    ref: "arv_fixture",
    artifactRef: "art_fixture",
    revision: 2,
    fileName: "document.md",
    mediaType: "text/markdown",
    sizeBytes: 3,
    digest: `sha256:${"a".repeat(64)}`,
    scanState: "CLEAN",
    source: "CONTROL_CENTER",
    previewAvailable: true,
    createdAt: "2026-10-10T00:00:00Z",
    ...options,
  };
}

beforeEach(() => vi.clearAllMocks());

describe("immutable artifact revision adapter", () => {
  it.each(["PENDING", "SCANNING", "CLEAN", "QUARANTINED", "FAILED"] as const)(
    "принимает каноническое состояние metadata %s",
    (scanState) => {
      const value = revision({ scanState });
      expect(assertArtifactRevision(value, "art_fixture")).toBe(value);
    },
  );

  it("закрыто отклоняет неизвестное состояние metadata", () => {
    const value = {
      ...revision(),
      scanState: "UNKNOWN",
    } as unknown as ArtifactRevision;
    expect(() => assertArtifactRevision(value, "art_fixture")).toThrow(
      "Invalid artifact revision readback",
    );
  });

  it.each(["PENDING", "SCANNING"] as const)(
    "читает metadata %s через exact revision и history без body",
    async (scanState) => {
      const value = revision({ scanState });
      const page = { items: [value], total: 1 };
      const signal = new AbortController().signal;
      sdk.getArtifactRevision.mockResolvedValue({
        data: value,
        response: new Response(),
      });
      sdk.listArtifactRevisions.mockResolvedValue({
        data: page,
        response: new Response(),
      });
      expect(
        await readArtifactRevision("art_fixture", "arv_fixture", signal),
      ).toEqual(value);
      expect(
        await loadArtifactRevisionPage("art_fixture", undefined, signal),
      ).toEqual(page);
      expect(sdk.downloadArtifactRevision).not.toHaveBeenCalled();
    },
  );

  it.each([
    { artifactRef: "art_foreign" },
    { ref: "" },
    { revision: 0 },
    { revision: 1.5 },
    { sizeBytes: -1 },
    { sizeBytes: (512 << 20) + 1 },
    { digest: "sha256:broken" },
    { createdAt: "not-a-date" },
  ])("закрыто отклоняет invalid metadata %j", (options) => {
    expect(() =>
      assertArtifactRevision(revision(options), "art_fixture"),
    ).toThrow();
  });

  it("перечитывает только exact revision, не latest", async () => {
    sdk.getArtifactRevision.mockResolvedValue({
      data: revision(),
      response: new Response(),
    });
    const signal = new AbortController().signal;
    expect(
      await readArtifactRevision("art_fixture", "arv_fixture", signal),
    ).toEqual(revision());
    expect(sdk.getArtifactRevision).toHaveBeenCalledWith({
      path: { artifactRef: "art_fixture", revisionRef: "arv_fixture" },
      signal,
    });
    sdk.getArtifactRevision.mockResolvedValue({
      data: revision({ ref: "arv_latest" }),
      response: new Response(),
    });
    await expect(
      readArtifactRevision("art_fixture", "arv_fixture", signal),
    ).rejects.toThrow();
    expect(sdk.getArtifact).not.toHaveBeenCalled();
  });

  it("проверяет project после owner Artifact read", async () => {
    sdk.getArtifact.mockResolvedValue({
      data: { ref: "art_fixture", projectRef: "prj_foreign" },
      response: new Response(),
    });
    await expect(
      readProjectArtifact(
        "prj_fixture",
        "art_fixture",
        new AbortController().signal,
      ),
    ).rejects.toThrow();
  });

  it("запрашивает bounded page и передаёт owner cursor", async () => {
    const data = {
      items: [revision()],
      total: 6,
      nextPageToken: "opaque-cursor",
    };
    sdk.listArtifactRevisions.mockResolvedValue({
      data,
      response: new Response(),
    });
    const signal = new AbortController().signal;
    expect(
      await loadArtifactRevisionPage("art_fixture", "previous-cursor", signal),
    ).toEqual(data);
    expect(sdk.listArtifactRevisions).toHaveBeenCalledWith({
      path: { artifactRef: "art_fixture" },
      query: { pageSize: 5, pageToken: "previous-cursor" },
      signal,
    });
  });

  it.each([
    { items: [revision(), revision()], total: 2 },
    {
      items: [
        revision({ revision: 1 }),
        revision({ ref: "arv_other", revision: 2 }),
      ],
      total: 2,
    },
    { items: [revision({ artifactRef: "art_foreign" })], total: 1 },
    { items: [revision()], total: 0 },
    { items: [], total: 0, nextPageToken: "" },
  ])("не принимает malformed history %j", async (data) => {
    sdk.listArtifactRevisions.mockResolvedValue({
      data,
      response: new Response(),
    });
    await expect(
      loadArtifactRevisionPage(
        "art_fixture",
        undefined,
        new AbortController().signal,
      ),
    ).rejects.toThrow();
  });

  it.each(
    (["PENDING", "SCANNING", "QUARANTINED", "FAILED"] as const).flatMap(
      (scanState) =>
        (["DOWNLOAD", "PREVIEW"] as const).map(
          (purpose) => [scanState, purpose] as const,
        ),
    ),
  )("закрывает %s content %s до retry и SDK", async (scanState, purpose) => {
    await expect(
      downloadRevisionContent(
        revision({ scanState }),
        purpose,
        new AbortController().signal,
      ),
    ).rejects.toThrow("Artifact revision content requires clean scan state");
    expect(readWithRetry).not.toHaveBeenCalled();
    expect(sdk.downloadArtifactRevision).not.toHaveBeenCalled();
  });

  it.each(["DOWNLOAD", "PREVIEW"] as const)(
    "%s pin-ит CLEAN historical revision и проверяет размер",
    async (purpose) => {
      const signal = new AbortController().signal;
      const body = new Blob(["old"]);
      sdk.downloadArtifactRevision.mockResolvedValue({
        data: body,
        response: new Response(),
      });
      expect(await downloadRevisionContent(revision(), purpose, signal)).toBe(
        body,
      );
      expect(sdk.downloadArtifactRevision).toHaveBeenCalledWith({
        path: { artifactRef: "art_fixture", revisionRef: "arv_fixture" },
        query: { purpose },
        parseAs: "blob",
        signal,
      });
      sdk.downloadArtifactRevision.mockResolvedValue({
        data: new Blob(["unexpected"]),
        response: new Response(),
      });
      await expect(
        downloadRevisionContent(revision(), purpose, signal),
      ).rejects.toThrow();
    },
  );

  it("отзыв local scope не принимает поздний page ACK", async () => {
    let resolve: (value: unknown) => void = () => {};
    sdk.listArtifactRevisions.mockImplementation(
      () =>
        new Promise((accept) => {
          resolve = accept;
        }),
    );
    const controller = new AbortController();
    const request = loadArtifactRevisionPage(
      "art_fixture",
      undefined,
      controller.signal,
    );
    controller.abort();
    resolve({
      data: { items: [revision()], total: 1 },
      response: new Response(),
    });
    await expect(request).rejects.toThrow();
  });
});
