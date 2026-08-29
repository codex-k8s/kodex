import type { Artifact } from "@/shared/api/generated/openapi/types.gen";

export type FileKind = "ALL" | "TEXT" | "DOCUMENT" | "IMAGE";
export type FileSource = "ALL" | Artifact["source"];
export type FileTab = "FILES" | "KNOWLEDGE" | "RESULTS" | "TRASH";

export type ArtifactLifecycleAction = "DELETE" | "RESTORE" | "PURGE";
export type ArtifactTrashBulkAction = "RESTORE" | "PURGE" | "EMPTY";
export type ArtifactLifecycleBlockReason =
  | "ACTION_NOT_ALLOWED"
  | "CONTRACT_UNAVAILABLE";

export type ArtifactLifecycleState =
  | {
      action: ArtifactLifecycleAction;
      available: true;
    }
  | {
      action: ArtifactLifecycleAction;
      available: false;
      reason: ArtifactLifecycleBlockReason;
    };

export type UploadQueueState = "QUEUED" | "UPLOADING" | "SUCCEEDED" | "FAILED";

export interface UploadQueueItem {
  file: File;
  id: string;
  problem?: string;
  state: UploadQueueState;
}

export type FileIconKind =
  | "archive"
  | "code"
  | "document"
  | "file"
  | "image"
  | "pdf"
  | "spreadsheet";

export interface FileVisual {
  extension: string;
  icon: FileIconKind;
}

export interface FilePreviewLabels {
  added: string;
  close: string;
  download: string;
  find: string;
  loading: string;
  protectedPreview: string;
  size: string;
  source: string;
  unavailable: string;
  version: string;
  zoom: string;
}

const documentExtensions = new Set(["doc", "docx", "odt", "ppt", "pptx"]);
const spreadsheetExtensions = new Set(["csv", "ods", "xls", "xlsx"]);
const archiveExtensions = new Set(["7z", "gz", "rar", "tar", "zip"]);
const codeExtensions = new Set([
  "json",
  "md",
  "markdown",
  "txt",
  "xml",
  "yaml",
  "yml",
]);

export function fileExtension(fileName: string): string {
  const extension = fileName.split(".").pop()?.trim().toLocaleLowerCase();
  return extension && extension !== fileName.toLocaleLowerCase()
    ? extension.slice(0, 5)
    : "file";
}

export function fileVisual(artifact: Artifact): FileVisual {
  const extension = fileExtension(artifact.fileName);
  if (artifact.mediaType === "application/pdf" || extension === "pdf")
    return { extension, icon: "pdf" };
  if (artifact.mediaType.startsWith("image/"))
    return { extension, icon: "image" };
  if (
    artifact.mediaType.includes("spreadsheet") ||
    spreadsheetExtensions.has(extension)
  )
    return { extension, icon: "spreadsheet" };
  if (
    artifact.mediaType.includes("officedocument") ||
    documentExtensions.has(extension)
  )
    return { extension, icon: "document" };
  if (
    artifact.mediaType.includes("zip") ||
    artifact.mediaType.includes("compressed") ||
    archiveExtensions.has(extension)
  )
    return { extension, icon: "archive" };
  if (artifact.mediaType.startsWith("text/") || codeExtensions.has(extension))
    return { extension, icon: "code" };
  return { extension, icon: "file" };
}

export function artifactKind(artifact: Artifact): Exclude<FileKind, "ALL"> {
  if (artifact.mediaType.startsWith("image/")) return "IMAGE";
  if (
    artifact.mediaType === "application/pdf" ||
    artifact.mediaType.includes("officedocument") ||
    documentExtensions.has(fileExtension(artifact.fileName)) ||
    spreadsheetExtensions.has(fileExtension(artifact.fileName))
  )
    return "DOCUMENT";
  return "TEXT";
}

export function matchesArtifactFilters(
  artifact: Artifact,
  options: {
    kind: FileKind;
    scanState: "ALL" | Artifact["scanState"];
    source: FileSource;
    tab: FileTab;
  },
): boolean {
  const inTrash = artifact.lifecycleState !== "ACTIVE";
  if (options.tab === "TRASH" && !inTrash) return false;
  if (options.tab !== "TRASH" && inTrash) return false;
  if (options.tab === "KNOWLEDGE" && artifact.agentBindings.length === 0)
    return false;
  if (
    options.tab === "RESULTS" &&
    artifact.source !== "AGENT_RESULT" &&
    artifact.source !== "INTEGRATION_RESULT"
  )
    return false;
  if (options.scanState !== "ALL" && artifact.scanState !== options.scanState)
    return false;
  if (options.source !== "ALL" && artifact.source !== options.source)
    return false;
  return options.kind === "ALL" || artifactKind(artifact) === options.kind;
}

export function artifactLifecycleState(
  artifact: Artifact,
  action: ArtifactLifecycleAction,
): ArtifactLifecycleState {
  const announcedByApi = (artifact.nextActions as readonly string[]).includes(
    action,
  );
  if (announcedByApi) return { action, available: true };
  return {
    action,
    available: false,
    reason: "ACTION_NOT_ALLOWED",
  };
}

export function trashBulkConfirmed(
  action: ArtifactTrashBulkAction,
  input: string,
  phrase: string,
): boolean {
  return action === "RESTORE" || input.trim() === phrase;
}

export function createUploadQueueItems(
  files: readonly File[],
  createId: () => string,
): UploadQueueItem[] {
  return files.map((file) => ({
    file,
    id: createId(),
    state: "QUEUED",
  }));
}

export function supportsInlinePreview(artifact: Artifact): boolean {
  return (
    artifact.previewAvailable &&
    (artifact.mediaType.startsWith("text/") ||
      artifact.mediaType === "application/json" ||
      artifact.mediaType.startsWith("image/"))
  );
}
