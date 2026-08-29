export interface StagedAssistantAttachment {
  key: string;
  file: File;
  name: string;
  mediaType: string;
  size: number;
}

export const assistantAttachmentTransportAvailable = false;

function attachmentKey(file: File): string {
  return [file.name, file.size, file.type, file.lastModified].join(":");
}

export function stageAssistantAttachments(
  current: readonly StagedAssistantAttachment[],
  files: Iterable<File>,
): StagedAssistantAttachment[] {
  const staged = new Map(current.map((item) => [item.key, item]));
  for (const file of files) {
    const key = attachmentKey(file);
    if (staged.has(key)) continue;
    staged.set(key, {
      key,
      file,
      name: file.name,
      mediaType: file.type || "application/octet-stream",
      size: file.size,
    });
  }
  return [...staged.values()];
}

export function removeAssistantAttachment(
  current: readonly StagedAssistantAttachment[],
  key: string,
): StagedAssistantAttachment[] {
  return current.filter((item) => item.key !== key);
}

export function formatAttachmentSize(value: number): string {
  if (value < 1_024) return `${String(value)} B`;
  if (value < 1_048_576) return `${(value / 1_024).toFixed(1)} KB`;
  return `${(value / 1_048_576).toFixed(1)} MB`;
}
