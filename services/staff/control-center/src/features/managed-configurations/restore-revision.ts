import type {
  ManagedConfiguration,
  ManagedConfigurationRevision,
} from "@/shared/api/generated/openapi/types.gen";
import { createDraft, history } from "./api";
import { normalizedConfigurationDocument } from "./document";

export function canRestoreRevision(
  configuration: ManagedConfiguration,
  revision: ManagedConfigurationRevision,
): boolean {
  return (
    !configuration.archived &&
    configuration.managedBy === "UI" &&
    !!configuration.currentRevision &&
    ["PUBLISHED", "SUPERSEDED"].includes(revision.state) &&
    (configuration.kind === "INTEGRATION_DEFINITION" ||
      (configuration.kind === "ROLE_IMAGE" &&
        configuration.sourceEditable === true &&
        revision.sourceAvailable === true))
  );
}

function document(revision: ManagedConfigurationRevision): string {
  return revision.contentFormat === "JSON" || revision.contentFormat === "YAML"
    ? normalizedConfigurationDocument(revision.content, revision.contentFormat)
    : revision.content.trim();
}

// История даёт содержимое новой ревизии. Опубликованный указатель и consumers
// меняются только последующими специализированными командами владельца.
export async function restoreRevision(
  configuration: ManagedConfiguration,
  revision: ManagedConfigurationRevision,
  signal: AbortSignal,
) {
  signal.throwIfAborted();
  if (!canRestoreRevision(configuration, revision))
    throw new Error("Configuration revision cannot be restored");
  const current = {
    ...configuration,
    currentRevision: configuration.currentRevision
      ? { ...configuration.currentRevision }
      : undefined,
  };
  const source = { ...revision };
  const publishedRef = current.currentRevision?.ref;
  const originalDocument = document(source);
  const result = await createDraft(
    current.kind,
    {
      configurationRef: current.ref,
      projectRef: current.projectRef,
      name: current.name,
      contentFormat: source.contentFormat,
      content: source.content,
    },
    current.version,
  );
  signal.throwIfAborted();
  if (
    result.configuration.ref !== current.ref ||
    result.configuration.kind !== current.kind ||
    result.configuration.projectRef !== current.projectRef ||
    result.configuration.managedBy !== "UI" ||
    result.configuration.archived ||
    result.configuration.version <= current.version ||
    result.revision.ref === source.ref ||
    result.revision.state !== "DRAFT" ||
    result.revision.parentRevisionRef !== publishedRef ||
    result.revision.revision <=
      Math.max(source.revision, current.currentRevision?.revision ?? 0) ||
    document(result.revision) !== originalDocument
  )
    throw new Error("Configuration restoration receipt mismatch");
  const readback = await history(current.ref, signal);
  signal.throwIfAborted();
  const draft = readback.items.find((item) => item.ref === result.revision.ref);
  if (
    readback.configuration.ref !== current.ref ||
    readback.configuration.kind !== current.kind ||
    readback.configuration.projectRef !== current.projectRef ||
    readback.configuration.managedBy !== "UI" ||
    readback.configuration.archived ||
    readback.configuration.version < result.configuration.version ||
    readback.configuration.currentRevision?.ref !== publishedRef ||
    !draft ||
    draft.state !== "DRAFT" ||
    draft.revision !== result.revision.revision ||
    draft.digest !== result.revision.digest ||
    document(draft) !== originalDocument
  )
    throw new Error("Configuration restoration readback mismatch");
  return { configuration: readback.configuration, revision: draft };
}
