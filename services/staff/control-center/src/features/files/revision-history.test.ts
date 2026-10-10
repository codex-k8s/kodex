import { afterEach, describe, expect, it, vi } from "vitest";
import type {
  Artifact,
  ArtifactRevision,
} from "@/shared/api/generated/openapi/types.gen";
import { resetOwnerRequests } from "@/shared/api/owner-lifetime";
const api = vi.hoisted(() => ({
  loadArtifactRevisionPage: vi.fn(),
  downloadRevisionContent: vi.fn(),
}));
vi.mock("@/shared/api/artifact-revisions", () => api);
import { createRevisionHistory } from "./revision-history";
const revision: ArtifactRevision = {
  ref: "arv_old",
  artifactRef: "art_file",
  revision: 1,
  fileName: "document.md",
  mediaType: "text/markdown",
  sizeBytes: 3,
  digest: `sha256:${"a".repeat(64)}`,
  scanState: "CLEAN",
  source: "CONTROL_CENTER",
  previewAvailable: true,
  createdAt: "2026-10-10T00:00:00Z",
};
const artifact = {
  ref: "art_file",
  projectRef: "prj_file",
  version: 2,
  nextActions: ["DOWNLOAD"],
} as Artifact;
afterEach(() => {
  resetOwnerRequests();
  vi.clearAllMocks();
});

describe("revision history request ownership", () => {
  it("явно догружает bounded history, не читает body автоматически", async () => {
    const latest = { ...revision, ref: "arv_latest", revision: 2 };
    api.loadArtifactRevisionPage
      .mockResolvedValueOnce({
        items: [latest],
        total: 2,
        nextPageToken: "cursor",
      })
      .mockResolvedValueOnce({ items: [revision], total: 2 });
    const history = createRevisionHistory();
    history.reset(artifact);
    await history.load();
    expect(history.items.value).toEqual([latest]);
    expect(api.downloadRevisionContent).not.toHaveBeenCalled();
    expect(api.loadArtifactRevisionPage).toHaveBeenCalledTimes(1);
    await history.load(true);
    expect(api.loadArtifactRevisionPage.mock.calls[1]?.[1]).toBe("cursor");
    history.dispose();
  });
  it("смена Artifact игнорирует old page ACK", async () => {
    let resolve: (value: unknown) => void = () => {};
    api.loadArtifactRevisionPage.mockImplementationOnce(
      () =>
        new Promise((accept) => {
          resolve = accept;
        }),
    );
    const history = createRevisionHistory();
    history.reset(artifact);
    const load = history.load();
    history.reset({ ...artifact, ref: "art_new" });
    resolve({ items: [revision], total: 1 });
    await load;
    expect(history.items.value).toEqual([]);
    expect(history.loading.value).toBe(false);
    history.dispose();
  });
  it("logout очищает уже видимую историю и закрывает поздний download", async () => {
    api.loadArtifactRevisionPage.mockResolvedValue({
      items: [revision],
      total: 1,
    });
    let resolve: (value: Blob) => void = () => {};
    api.downloadRevisionContent.mockImplementation(
      () =>
        new Promise((accept) => {
          resolve = accept;
        }),
    );
    const history = createRevisionHistory();
    history.reset(artifact);
    await history.load();
    const download = history.download(revision);
    resetOwnerRequests();
    resolve(new Blob(["old"]));
    expect(await download).toBeUndefined();
    expect(history.items.value).toEqual([]);
    history.dispose();
  });
  it("повторяющийся cursor и duplicate revisions закрывают догрузку", async () => {
    api.loadArtifactRevisionPage
      .mockResolvedValueOnce({
        items: [revision],
        total: 2,
        nextPageToken: "cursor",
      })
      .mockResolvedValueOnce({
        items: [revision],
        total: 2,
        nextPageToken: "cursor",
      });
    const history = createRevisionHistory();
    history.reset(artifact);
    await history.load();
    await history.load(true);
    expect(history.error.value).toBeDefined();
    expect(history.items.value).toEqual([revision]);
    history.dispose();
  });
  it.each(["PENDING", "SCANNING", "QUARANTINED", "FAILED"] as const)(
    "показывает metadata %s и закрывает download без вызова adapter",
    async (scanState) => {
      const value = { ...revision, scanState };
      api.loadArtifactRevisionPage.mockResolvedValue({
        items: [value],
        total: 1,
      });
      const history = createRevisionHistory();
      history.reset(artifact);
      await history.load();
      expect(history.items.value).toEqual([value]);
      expect(history.loaded.value).toBe(true);
      expect(await history.download(value)).toBeUndefined();
      expect(history.downloading.value).toBe("");
      expect(api.downloadRevisionContent).not.toHaveBeenCalled();
      history.dispose();
    },
  );

  it("скачивает CLEAN historical revision после drift текущего Artifact", async () => {
    const body = new Blob(["old"]);
    api.loadArtifactRevisionPage.mockResolvedValue({
      items: [revision],
      total: 1,
    });
    api.downloadRevisionContent.mockResolvedValueOnce(body);
    const history = createRevisionHistory();
    history.reset({
      ...artifact,
      version: 17,
      revision: 12,
      currentRevisionRef: "arv_latest",
    });
    await history.load();
    expect(await history.download(revision)).toBe(body);
    expect(api.downloadRevisionContent).toHaveBeenCalledOnce();
    expect(api.downloadRevisionContent).toHaveBeenCalledWith(
      revision,
      "DOWNLOAD",
      expect.any(AbortSignal),
    );
    expect(history.error.value).toBeUndefined();
    history.dispose();
  });

  it("не скачивает чужую revision или без DOWNLOAD", async () => {
    api.loadArtifactRevisionPage.mockResolvedValue({
      items: [revision],
      total: 1,
    });
    const history = createRevisionHistory();
    history.reset(artifact);
    await history.load();
    expect(
      await history.download({ ...revision, ref: "arv_foreign" }),
    ).toBeUndefined();
    history.reset({ ...artifact, nextActions: [] });
    await history.load();
    expect(await history.download(revision)).toBeUndefined();
    expect(api.downloadRevisionContent).not.toHaveBeenCalled();
    history.dispose();
  });
});
