import type { Artifact, Run } from "@/shared/api/generated/openapi/types.gen";
import type { AsyncEntityPickerItem } from "@/shared/ui/async-entity-picker";

export type NewRunTargetType = "AGENT" | "WORKFLOW";
export type FileVisualKind =
  | "archive"
  | "code"
  | "document"
  | "generic"
  | "image"
  | "pdf"
  | "spreadsheet";

export interface ArtifactPickerItem extends AsyncEntityPickerItem {
  artifact: Artifact;
}

export interface SessionPickerItem extends AsyncEntityPickerItem {
  run: Run;
}

const extensionKinds: Record<string, FileVisualKind> = {
  csv: "spreadsheet",
  doc: "document",
  docx: "document",
  gif: "image",
  gz: "archive",
  jpeg: "image",
  jpg: "image",
  json: "code",
  md: "document",
  pdf: "pdf",
  png: "image",
  tar: "archive",
  txt: "document",
  webp: "image",
  xls: "spreadsheet",
  xlsx: "spreadsheet",
  xml: "code",
  yaml: "code",
  yml: "code",
  zip: "archive",
};

export function fileExtension(fileName: string, mediaType: string): string {
  const suffix = fileName.split(".").at(-1)?.trim().toLowerCase();
  if (suffix && suffix !== fileName.toLowerCase() && suffix.length <= 8)
    return suffix;
  const subtype = mediaType.split("/").at(-1)?.split(/[;+]/)[0]?.trim();
  return subtype && subtype.length <= 8 ? subtype : "file";
}

export function fileVisualKind(
  fileName: string,
  mediaType: string,
): FileVisualKind {
  const extension = fileExtension(fileName, mediaType);
  const mapped = extensionKinds[extension];
  if (mapped) return mapped;
  if (mediaType.startsWith("image/")) return "image";
  if (mediaType.includes("spreadsheet") || mediaType.includes("excel"))
    return "spreadsheet";
  if (mediaType.includes("json") || mediaType.includes("xml")) return "code";
  if (mediaType.startsWith("text/") || mediaType.includes("word"))
    return "document";
  return "generic";
}

export function formatFileSize(sizeBytes: number, locale: string): string {
  const units: Array<[Intl.NumberFormatOptions["unit"], number]> = [
    ["gigabyte", 1024 ** 3],
    ["megabyte", 1024 ** 2],
    ["kilobyte", 1024],
  ];
  const [unit, divisor] = units.find(
    ([, threshold]) => sizeBytes >= threshold,
  ) ?? ["byte", 1];
  return new Intl.NumberFormat(locale, {
    maximumFractionDigits: divisor === 1 ? 0 : 1,
    style: "unit",
    unit,
    unitDisplay: "short",
  }).format(sizeBytes / divisor);
}

export function formatTimestamp(value: string, locale: string): string {
  return new Intl.DateTimeFormat(locale, {
    dateStyle: "medium",
    timeStyle: "short",
  }).format(new Date(value));
}

export function toArtifactPickerItem(artifact: Artifact): ArtifactPickerItem {
  return {
    artifact,
    disabled: artifact.scanState !== "CLEAN",
    id: artifact.ref,
    label: artifact.fileName,
  };
}

export function toSessionPickerItem(run: Run): SessionPickerItem {
  return {
    description: run.resultSummary,
    id: run.sessionRef,
    label: run.title,
    run,
  };
}
