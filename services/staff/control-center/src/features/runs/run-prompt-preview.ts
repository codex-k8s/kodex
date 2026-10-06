import { previewPromptTemplate } from "@/shared/api/generated/openapi/sdk.gen";
import type { PromptTemplatePreview } from "@/shared/api/generated/openapi/types.gen";
import { requestSignal } from "@/shared/api/client";
import { csrfToken } from "@/shared/api/mutation";
import { unwrap } from "@/shared/api/problem";

const digestPattern = /^[a-f0-9]{64}$/;
const referencePattern = /^[A-Za-z0-9_-]{8,96}$/;
const slots = [
  "WORKFLOW",
  "STAGE",
  "PURPOSE",
  "EXPECTED_RESULT",
  "INPUT",
  "CONSTRAINTS",
  "EFFECTIVE_CAPABILITIES",
  "FILES",
  "TOOLS",
  "INTEGRATIONS",
  "RUNTIME_CHANGES",
];

export async function loadRunPromptPreview(
  runRef: string,
  signal: AbortSignal,
): Promise<PromptTemplatePreview> {
  if (typeof runRef !== "string" || !referencePattern.test(runRef))
    throw new Error("Invalid run prompt preview target");
  const active = requestSignal(signal);
  active.throwIfAborted();
  const result = (
    await unwrap(
      previewPromptTemplate({
        body: {
          template: "",
          targetKind: "RUN",
          targetRef: runRef,
          includeFullMaterialization: false,
        },
        headers: { "X-CSRF-Token": csrfToken() },
        signal: active,
        cache: "no-store",
      }),
    )
  ).data;
  active.throwIfAborted();
  const contextPin = result.contextPin;
  if (
    !contextPin ||
    result.fullMaterializedPrompt !== undefined ||
    typeof result.templateRef !== "string" ||
    !referencePattern.test(result.templateRef) ||
    typeof result.complete !== "boolean" ||
    typeof result.safePreview !== "string" ||
    typeof result.serviceTemplateRevision !== "string" ||
    !result.serviceTemplateRevision ||
    result.serviceTemplateRevision.length > 128 ||
    typeof result.locale !== "string" ||
    !result.locale ||
    result.locale.length > 16 ||
    ![
      result.templateDigest,
      result.materializationDigest,
      result.serviceTemplateDigest,
      result.variableSnapshotDigest,
      contextPin.digest,
    ].every(
      (digest) => typeof digest === "string" && digestPattern.test(digest),
    ) ||
    !Array.isArray(result.sections) ||
    result.sections.length < 1 ||
    result.sections.length > 32 ||
    result.sections.some(
      (section) =>
        !["PLATFORM", "USER_TEMPLATE"].includes(section.source) ||
        typeof section.content !== "string" ||
        section.content.length > 256 << 10 ||
        (section.slot !== undefined && !slots.includes(section.slot)),
    ) ||
    !Array.isArray(result.slots) ||
    result.slots.length < 1 ||
    result.slots.length > 11 ||
    result.slots.some(
      (slot) =>
        !slots.includes(slot.slot) ||
        !["PLATFORM", "USER_TEMPLATE"].includes(slot.source) ||
        !Number.isSafeInteger(slot.position) ||
        slot.position < 1,
    ) ||
    !Array.isArray(result.diagnostics) ||
    !Array.isArray(result.effectiveCapabilities)
  )
    throw new Error("Invalid safe run prompt preview boundary");
  // Только поля безопасной проекции; полный текст и runtime payload не копируются.
  return {
    safePreview: result.safePreview,
    complete: result.complete,
    diagnostics: result.diagnostics.map(
      ({ code, message, severity, variableName, line, column }) => ({
        code,
        message,
        severity,
        variableName,
        line,
        column,
      }),
    ),
    templateRef: result.templateRef,
    templateDigest: result.templateDigest,
    materializationDigest: result.materializationDigest,
    serviceTemplateRevision: result.serviceTemplateRevision,
    serviceTemplateDigest: result.serviceTemplateDigest,
    variableSnapshotDigest: result.variableSnapshotDigest,
    locale: result.locale,
    effectiveCapabilities: [...result.effectiveCapabilities],
    slots: result.slots.map(({ slot, source, position }) => ({
      slot,
      source,
      position,
    })),
    sections: result.sections.map(({ source, slot, content }) => ({
      source,
      slot,
      content,
    })),
    contextPin: { digest: contextPin.digest },
  };
}
