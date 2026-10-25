import type {
  RepositoryBundleFile,
  RepositoryBundleManifestPage,
  RepositoryBundlePublication,
} from "@/shared/api/generated/openapi/types.gen";

const digest = /^[0-9a-f]{64}$/;
const encoder = new TextEncoder();

export interface CompleteRepositoryBundleManifest {
  proposal: RepositoryBundlePublication;
  files: RepositoryBundleFile[];
}

export interface BundlePageRequest {
  offset: number;
  expectedVersion?: number;
  expectedManifestSha256?: string;
}

function invalid(): never {
  throw new Error("Repository bundle manifest is invalid");
}

function validFile(file: Partial<RepositoryBundleFile>): file is RepositoryBundleFile {
  if (typeof file.path !== "string" || typeof file.mode !== "string" || typeof file.bytes !== "number" || typeof file.sha256 !== "string") return false;
  const segments = file.path.split("/");
  return (
    segments.length > 0 &&
    encoder.encode(file.path).length <= 512 &&
    !/[\\:\p{Cc}\p{Cf}]/u.test(file.path) &&
    segments.every(
      (part) => part !== "" && part !== "." && part !== ".." &&
        part.trim() === part && !/[. ]$/.test(part) && part.toLowerCase() !== ".git",
    ) &&
    ["100644", "100755"].includes(file.mode) &&
    Number.isSafeInteger(file.bytes) && file.bytes >= 0 && file.bytes <= 1048576 &&
    digest.test(file.sha256)
  );
}

function identity(proposal: RepositoryBundlePublication): string {
  return JSON.stringify(proposal);
}

function validContinuation(proposal: RepositoryBundlePublication): boolean {
  const state = proposal.continuationState ?? "";
  const code = proposal.continuationCode ?? "";
  const node = proposal.continuationNodeRef ?? "";
  if (state === "") return code === "" && node === "";
  if (!["SUCCEEDED", "FAILED", "REJECTED", "CANCELLED", "EXPIRED"].includes(proposal.state)) return false;
  if (!/^nod_[A-Za-z0-9_-]{8,89}$/.test(node)) return false;
  return (state === "BLOCKED" && ["CALLBACK_INPUT_TOO_LARGE", "CONTINUATION_AUTHORITY_CHANGED"].includes(code)) || (state === "CANCELLED" && code === "");
}

function compareUTF8(left: string, right: string): number {
  const a = encoder.encode(left), b = encoder.encode(right);
  for (let index = 0; index < Math.min(a.length, b.length); index += 1) {
    const leftByte = a[index], rightByte = b[index];
    if (leftByte === undefined || rightByte === undefined) return invalid();
    const difference = leftByte - rightByte;
    if (difference !== 0) return difference;
  }
  return a.length - b.length;
}

// Полнота подтверждается только всеми последовательными страницами и SHA256
// canonical manifest. Страница с complete=true сама по себе этого не доказывает.
export async function readCompleteRepositoryBundleManifest(
  gateRef: string,
  page: (request: BundlePageRequest) => Promise<RepositoryBundleManifestPage>,
  signal: AbortSignal,
): Promise<CompleteRepositoryBundleManifest> {
  let proposal: RepositoryBundlePublication | undefined;
  const files: RepositoryBundleFile[] = [];
  let offset = 0;
  for (let request = 0; request < 256; request += 1) {
    signal.throwIfAborted();
    const current = await page({
      offset,
      ...(proposal
        ? { expectedVersion: proposal.version, expectedManifestSha256: proposal.manifest.sha256 }
        : {}),
    });
    signal.throwIfAborted();
    // JSON приходит из transport; required generated fields не устраняют runtime validation.
    const value = (current as Partial<RepositoryBundleManifestPage>).proposal;
    const manifestPin = value?.manifest as Partial<RepositoryBundlePublication["manifest"]> | undefined;
    if (
      !value || value.gateRef !== gateRef || !value.ref.startsWith("rbp_") ||
      !validContinuation(value) ||
      value.proposalBranch !== `kodex/bundle/${value.ref}` ||
      !value.projectRef || !Number.isSafeInteger(value.version) || value.version < 1 ||
      !Number.isInteger(value.fileCount) || value.fileCount < 1 || value.fileCount > 256 ||
      !Number.isSafeInteger(value.totalBytes) || value.totalBytes < 0 || value.totalBytes > 1048576 ||
      !manifestPin || typeof manifestPin.sha256 !== "string" || !digest.test(manifestPin.sha256) ||
      typeof manifestPin.sizeBytes !== "number" || !Number.isSafeInteger(manifestPin.sizeBytes) || manifestPin.sizeBytes < 1 || manifestPin.sizeBytes > 131072 ||
      current.offset !== offset || !Array.isArray(current.files) || current.files.length < 1 || current.files.length > 32 ||
      current.nextOffset !== offset + current.files.length || current.nextOffset > value.fileCount ||
      current.complete !== (current.nextOffset === value.fileCount) ||
      (proposal !== undefined && identity(proposal) !== identity(value))
    ) invalid();
    proposal ??= value;
    for (const file of current.files) {
      const previous = files.at(-1);
      if (!validFile(file) || (previous !== undefined && compareUTF8(previous.path, file.path) >= 0)) invalid();
      files.push(file);
    }
    offset = current.nextOffset;
    if (!current.complete) continue;
    const folded = new Set<string>();
    const paths = new Set(files.map((file) => file.path));
    for (const file of files) {
      const lower = file.path.toLowerCase();
      if (folded.has(lower)) invalid();
      folded.add(lower);
      const segments = file.path.split("/");
      for (let count = 1; count < segments.length; count += 1) {
        if (paths.has(segments.slice(0, count).join("/"))) invalid();
      }
    }
    if (files.length !== proposal.fileCount || files.reduce((sum, file) => sum + file.bytes, 0) !== proposal.totalBytes) invalid();
    const canonical = encoder.encode(JSON.stringify({
      version: 1,
      total_bytes: proposal.totalBytes,
      files: files.map((file) => ({ path: file.path, mode: file.mode, bytes: file.bytes, sha256: file.sha256 })),
    }).replace(/[<>&\u2028\u2029]/g, (character) => `\\u${character.charCodeAt(0).toString(16).padStart(4, "0")}`));
    if (canonical.length !== proposal.manifest.sizeBytes) invalid();
    const calculated = Array.from(new Uint8Array(await crypto.subtle.digest("SHA-256", canonical)))
      .map((byte) => byte.toString(16).padStart(2, "0")).join("");
    signal.throwIfAborted();
    if (calculated !== proposal.manifest.sha256) invalid();
    return { proposal, files };
  }
  return invalid();
}
