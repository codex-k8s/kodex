import { describe, expect, it, vi } from "vitest";
import { fileRevisionFixture } from "./project-file-plan.fixtures";
const api = vi.hoisted(() => ({
  readProjectArtifact: vi.fn(),
  readArtifactRevision: vi.fn(),
}));
vi.mock("@/shared/api/artifact-revisions", () => api);
import { readAppliedProjectFileRevision } from "./project-file-readback";

describe("canonical file revision receipt readback", () => {
  it("old immutable receipt не требует equality текущей aggregate version", async () => {
    const receipt = fileRevisionFixture();
    const signal = new AbortController().signal;
    api.readProjectArtifact.mockResolvedValue({
      ref: receipt.artifactRef,
      projectRef: "prj_file",
      version: 100,
      revision: 90,
    });
    api.readArtifactRevision.mockResolvedValue(receipt);
    expect(
      await readAppliedProjectFileRevision("prj_file", receipt, signal),
    ).toEqual(receipt);
    expect(api.readProjectArtifact).toHaveBeenCalledWith(
      "prj_file",
      receipt.artifactRef,
      signal,
    );
    expect(api.readArtifactRevision).toHaveBeenCalledWith(
      receipt.artifactRef,
      receipt.ref,
      signal,
    );
  });
  it.each([
    "ref",
    "artifactRef",
    "digest",
    "fileName",
    "mediaType",
    "sizeBytes",
    "revision",
    "scanState",
    "source",
    "previewAvailable",
    "createdAt",
  ] as const)("не принимает readback mismatch %s", async (field) => {
    const receipt = fileRevisionFixture();
    api.readProjectArtifact.mockResolvedValue({});
    api.readArtifactRevision.mockResolvedValue({
      ...receipt,
      [field]: field === "sizeBytes" || field === "revision" ? 99 : "different",
    });
    await expect(
      readAppliedProjectFileRevision(
        "prj_file",
        receipt,
        new AbortController().signal,
      ),
    ).rejects.toThrow();
  });
});
