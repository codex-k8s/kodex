import { webcrypto } from "node:crypto";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type {
  RepositoryBundleFile,
  RepositoryBundleManifestPage,
  RepositoryBundlePublication,
} from "@/shared/api/generated/openapi/types.gen";
import { readCompleteRepositoryBundleManifest } from "./repository-bundle-manifest";

const encoder = new TextEncoder();
async function fixture(): Promise<{ proposal: RepositoryBundlePublication; files: RepositoryBundleFile[] }> {
  const files: RepositoryBundleFile[] = [
    { path: "a<&>.go", mode: "100644", bytes: 4, sha256: "a".repeat(64) },
    { path: "source/я.go", mode: "100644", bytes: 5, sha256: "b".repeat(64) },
    { path: "source/\uE000.go", mode: "100644", bytes: 0, sha256: "c".repeat(64) },
    { path: "source/😀.go", mode: "100755", bytes: 6, sha256: "d".repeat(64) },
  ];
  const raw = encoder.encode(JSON.stringify({ version: 1, total_bytes: 15, files })
    .replace(/[<>&\u2028\u2029]/g, (c) => `\\u${c.charCodeAt(0).toString(16).padStart(4, "0")}`));
  const sha = Array.from(new Uint8Array(await crypto.subtle.digest("SHA-256", raw)))
    .map((c) => c.toString(16).padStart(2, "0")).join("");
  const pin = { ref: "art_archive000", revision: 1, version: 2, sha256: "e".repeat(64), sizeBytes: 1000 };
  const proposal: RepositoryBundlePublication = {
    ref: "rbp_publication000", version: 3, projectRef: "prj_project000", originRunRef: "run_origin000", originRuntimeRevisionRef: "rrev_origin000",
    invocationRef: "inv_invocation000", gateRef: "gat_decision000", connectionRef: "int_connection000", connectionVersion: 5, grantRef: "grt_permission000", grantVersion: 2,
    repositoryRef: "fixture/repository", baseCommitSha: "f".repeat(40), proposalBranch: "kodex/bundle/rbp_publication000", archive: pin,
    manifest: { ...pin, ref: "art_manifest000", sha256: sha, sizeBytes: raw.length }, approvalDigest: "1".repeat(64), fileCount: files.length, totalBytes: 15,
    state: "WAITING_APPROVAL", createdAt: "2026-10-25T00:00:00Z", expiresAt: "2026-10-26T00:00:00Z",
  };
  return { proposal, files };
}

describe("полный version-pinned manifest публикации", () => {
  beforeEach(() => { vi.stubGlobal("crypto", webcrypto); });
  afterEach(() => { vi.unstubAllGlobals(); });

  it("читает все страницы и сверяет exact canonical UTF8 SHA, включая иной UTF16 order", async () => {
    const { proposal, files } = await fixture();
    const requests: unknown[] = [];
    const result = await readCompleteRepositoryBundleManifest(proposal.gateRef, (request) => {
      requests.push(request);
      const offset = request.offset;
      return Promise.resolve({ proposal: { ...proposal }, files: files.slice(offset, offset + 2), offset, nextOffset: offset + 2, complete: offset + 2 === files.length });
    }, new AbortController().signal);
    expect(result.files).toEqual(files);
    expect(requests).toEqual([{ offset: 0 }, { offset: 2, expectedVersion: 3, expectedManifestSha256: proposal.manifest.sha256 }]);
  });

  for (const name of ["foreign gate", "false complete", "missing file", "duplicate path", "wrong digest", "wrong total", "empty page", "offset gap", "mixed version", "mixed source pin"]) {
    it(`закрыто отклоняет ${name}`, async () => {
      const { proposal, files } = await fixture();
      let calls = 0;
      await expect(readCompleteRepositoryBundleManifest(proposal.gateRef, ({ offset }) => {
        calls += 1;
        const page: RepositoryBundleManifestPage = { proposal: structuredClone(proposal), files: structuredClone(files.slice(offset, offset + 2)), offset, nextOffset: offset + 2, complete: offset + 2 === files.length };
        switch (name) {
          case "foreign gate": page.proposal.gateRef = "gat_foreign000"; break;
          case "false complete": page.complete = true; break;
          case "missing file": page.files.pop(); break;
          case "duplicate path": { const [first, second] = page.files; if (!first || !second) throw new Error("fixture page is incomplete"); second.path = first.path; break; }
          case "wrong digest": { const first = page.files[0]; if (!first) throw new Error("fixture page is incomplete"); first.sha256 = "0".repeat(64); break; }
          case "wrong total": page.proposal.totalBytes += 1; break;
          case "empty page": page.files = []; page.nextOffset = offset; break;
          case "offset gap": page.nextOffset += 1; break;
          case "mixed version": if (offset > 0) page.proposal.version += 1; break;
          case "mixed source pin": if (offset > 0) page.proposal.archive.revision += 1; break;
        }
        return Promise.resolve(page);
      }, new AbortController().signal)).rejects.toThrow("manifest is invalid");
      expect(calls).toBeLessThanOrEqual(2);
    });
  }

  it("не выдаёт старые metadata после owner cancellation между страницами", async () => {
    const { proposal, files } = await fixture();
    const controller = new AbortController();
    let calls = 0;
    await expect(readCompleteRepositoryBundleManifest(proposal.gateRef, ({ offset }) => {
      calls += 1;
      controller.abort();
      return Promise.resolve({ proposal, files: files.slice(offset, offset + 2), offset, nextOffset: offset + 2, complete: false });
    }, controller.signal)).rejects.toThrow();
    expect(calls).toBe(1);
  });

  it("сохраняет подтверждённый Git outcome отдельно от blocked continuation и отклоняет partial status", async () => {
    const { proposal, files } = await fixture();
    proposal.state = "SUCCEEDED";
    proposal.candidateCommitSha = "a".repeat(40);
    proposal.candidateTreeSha = "b".repeat(40);
    proposal.confirmedAt = "2026-10-25T01:00:00Z";
    proposal.continuationState = "BLOCKED";
    proposal.continuationCode = "CALLBACK_INPUT_TOO_LARGE";
    proposal.continuationNodeRef = "nod_exact12345";
    const page = (): Promise<RepositoryBundleManifestPage> => Promise.resolve({ proposal, files, offset: 0, nextOffset: files.length, complete: true });
    const result = await readCompleteRepositoryBundleManifest(proposal.gateRef, page, new AbortController().signal);
    expect(result.proposal.state).toBe("SUCCEEDED");
    expect(result.proposal.continuationState).toBe("BLOCKED");
    delete proposal.continuationCode;
    await expect(readCompleteRepositoryBundleManifest(proposal.gateRef, page, new AbortController().signal)).rejects.toThrow("manifest is invalid");
  });
});
