import type {
  Artifact,
  ArtifactImpact,
} from "@/shared/api/generated/openapi/types.gen";

export type FileKind = "ALL" | "TEXT" | "DOCUMENT" | "IMAGE";
export type FileSource = "ALL" | Artifact["source"];
export type FileCollectionMode = "ACTIVE" | "TRASH";
export type FileTab = "FILES" | "KNOWLEDGE" | "RESULTS" | "TRASH";

export function artifactBindingControlEnabled(
  artifact: Pick<Artifact, "nextActions" | "agentBindings">,
  agentRef: string,
  hasAssignedFilesCapability: boolean,
): boolean {
  return (
    artifact.nextActions.includes("BIND") &&
    (artifact.agentBindings.includes(agentRef) || hasAssignedFilesCapability)
  );
}

export type ArtifactLifecycleAction = "DELETE" | "RESTORE" | "PURGE";
export type ArtifactTrashBulkAction = "DELETE" | "RESTORE" | "PURGE" | "EMPTY";
export type ArtifactLifecycleBlockReason =
  | "ACTION_NOT_ALLOWED"
  | "IMPACT_BLOCKED"
  | "IMPACT_UNAVAILABLE";

export type ArtifactLifecycleState =
  | {
      action: ArtifactLifecycleAction;
      available: true;
      impact?: ArtifactImpact;
    }
  | {
      action: ArtifactLifecycleAction;
      available: false;
      impact?: ArtifactImpact;
      reason: ArtifactLifecycleBlockReason;
    };

export type UploadQueueState = "QUEUED" | "UPLOADING" | "SUCCEEDED" | "FAILED";

export interface UploadQueueItem {
  file: File;
  id: string;
  problem?: string;
  progress?: {
    loadedBytes: number;
    totalBytes: number;
  };
  state: UploadQueueState;
}

export const uploadQueueConcurrency = 3;

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
const fileSources = [
  "CONTROL_CENTER",
  "INTERACTION_ATTACHMENT",
] as const satisfies readonly Artifact["source"][];
const knowledgeSources = [
  "KNOWLEDGE_SOURCE",
] as const satisfies readonly Artifact["source"][];
const resultSources = [
  "AGENT_RESULT",
  "INTEGRATION_RESULT",
] as const satisfies readonly Artifact["source"][];
const allSources = [
  ...fileSources,
  ...knowledgeSources,
  ...resultSources,
] as const satisfies readonly Artifact["source"][];

export function artifactSourcesForTab(
  tab: FileTab,
): readonly Artifact["source"][] {
  if (tab === "FILES") return fileSources;
  if (tab === "KNOWLEDGE") return knowledgeSources;
  if (tab === "RESULTS") return resultSources;
  return allSources;
}

export function artifactSourceKinds(
  tab: FileTab,
  source: FileSource,
): Artifact["source"][] {
  const available = artifactSourcesForTab(tab);
  if (source === "ALL") return [...available];
  return available.includes(source) ? [source] : [];
}

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
  if (!artifactSourcesForTab(options.tab).includes(artifact.source))
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
  impact?: ArtifactImpact,
): ArtifactLifecycleState {
  const announcedByApi = (artifact.nextActions as readonly string[]).includes(
    action,
  );
  if (!announcedByApi)
    return {
      action,
      available: false,
      reason: "ACTION_NOT_ALLOWED",
    };
  if (action === "RESTORE") return { action, available: true };
  if (
    !impact ||
    impact.artifactRef !== artifact.ref ||
    impact.artifactVersion !== artifact.version ||
    impact.action !== action
  )
    return {
      action,
      available: false,
      reason: "IMPACT_UNAVAILABLE",
    };
  if (!impact.permitted)
    return {
      action,
      available: false,
      impact,
      reason: "IMPACT_BLOCKED",
    };
  return { action, available: true, impact };
}

export function artifactLifecycleAnnounced(
  artifact: Artifact,
  action: ArtifactLifecycleAction,
): boolean {
  return (artifact.nextActions as readonly string[]).includes(action);
}

export function trashBulkConfirmed(
  action: ArtifactTrashBulkAction,
  input: string,
  phrase: string,
): boolean {
  return action === "RESTORE" || action === "DELETE" || input.trim() === phrase;
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

export function nextUploadQueueItems(
  items: readonly UploadQueueItem[],
  concurrency = uploadQueueConcurrency,
): UploadQueueItem[] {
  const limit = Math.max(1, Math.floor(concurrency));
  const active = items.filter((item) => item.state === "UPLOADING").length;
  return items
    .filter((item) => item.state === "QUEUED")
    .slice(0, Math.max(0, limit - active));
}

export function uploadProgressPercent(
  progress: UploadQueueItem["progress"],
): number | undefined {
  if (
    !progress ||
    !Number.isSafeInteger(progress.loadedBytes) ||
    !Number.isSafeInteger(progress.totalBytes) ||
    progress.loadedBytes < 0 ||
    progress.totalBytes < 1 ||
    progress.loadedBytes > progress.totalBytes
  )
    return undefined;
  return Math.round((progress.loadedBytes / progress.totalBytes) * 100);
}

export function supportsInlinePreview(artifact: Artifact): boolean {
  return (
    artifact.previewAvailable &&
    (artifact.mediaType.startsWith("text/") ||
      artifact.mediaType === "application/json" ||
      artifact.mediaType.startsWith("image/"))
  );
}
