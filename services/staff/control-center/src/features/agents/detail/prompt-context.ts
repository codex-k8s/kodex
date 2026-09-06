import {
  queryPromptTemplateVariables,
  previewPromptTemplate,
  validatePromptTemplate,
} from "@/shared/api/generated/openapi/sdk.gen";
import type {
  PromptVariableCatalogInput,
  PromptContextPin,
  PromptTemplatePreview,
  PromptTemplateValidation,
} from "@/shared/api/generated/openapi/types.gen";
import { csrfToken } from "@/shared/api/mutation";
import { requestSignal } from "@/shared/api/client";
import { unwrap } from "@/shared/api/problem";
import type { AsyncEntityLoader } from "@/shared/ui/async-entity-picker";
import {
  toTemplateVariablePickerItem,
  type TemplateVariablePickerItem,
} from "./model";

export type PromptTarget = Pick<
  PromptVariableCatalogInput,
  "projectRef" | "targetKind" | "targetRef" | "context"
>;
export function checkedPromptPin(
  pin: PromptContextPin | undefined,
  target: PromptTarget,
): PromptContextPin {
  if (!pin || !/^[a-f0-9]{64}$/.test(pin.digest) || !target.targetRef)
    throw new Error("Prompt context pin is unavailable");
  if (target.targetKind === "AGENT" && pin.agentRef !== target.targetRef)
    throw new Error("Prompt agent context mismatch");
  if (
    target.targetKind === "WORKFLOW_STAGE" &&
    (pin.workflowRef !== target.targetRef ||
      !pin.workflowRevisionRef ||
      pin.workflowStageKey !== target.context?.workflowStageKey)
  )
    throw new Error("Prompt workflow context mismatch");
  if (
    target.targetKind === "SESSION_CONTINUATION" &&
    !pin.previousRuntimeRevisionRef
  )
    throw new Error("Prompt continuation context mismatch");
  const context = target.context;
  if (
    (context?.agentRef && pin.agentRef !== context.agentRef) ||
    (context?.expectedAgentVersion !== undefined &&
      pin.agentVersion !== context.expectedAgentVersion) ||
    (context?.expectedWorkflowVersion !== undefined &&
      pin.workflowVersion !== context.expectedWorkflowVersion) ||
    (context?.workflowRevisionRef &&
      pin.workflowRevisionRef !== context.workflowRevisionRef) ||
    (context?.attachmentSetRef &&
      pin.attachmentSetRef !== context.attachmentSetRef)
  )
    throw new Error("Prompt selected context changed");
  return pin;
}
export async function readPromptVariables(
  target: PromptTarget,
  query: string,
  pageToken: string | undefined,
  expectedContextDigest: string | undefined,
  signal: AbortSignal,
) {
  const page = (
    await unwrap(
      queryPromptTemplateVariables({
        body: {
          ...target,
          query,
          pageSize: 50,
          pageToken,
          expectedContextDigest,
        },
        headers: { "X-CSRF-Token": csrfToken() },
        signal: requestSignal(signal),
        cache: "no-store",
      }),
    )
  ).data;
  const pin = checkedPromptPin(page.contextPin, target);
  if (
    (expectedContextDigest && pin.digest !== expectedContextDigest) ||
    !Number.isSafeInteger(page.total) ||
    page.total < page.items.length ||
    (page.nextPageToken && page.nextPageToken === pageToken)
  )
    throw new Error("Invalid prompt variable snapshot");
  return { ...page, contextPin: pin };
}
export function createPromptVariableLoader(
  target: PromptTarget,
  onPin?: (pin: PromptContextPin) => void,
): AsyncEntityLoader<TemplateVariablePickerItem> {
  let snapshot: { digest: string; query: string; cursor?: string } | undefined;
  return async ({ query, cursor, signal }) => {
    if (
      cursor &&
      (!snapshot || snapshot.query !== query || snapshot.cursor !== cursor)
    )
      throw new Error(
        "Prompt variable cursor does not match the selected snapshot",
      );
    const page = await readPromptVariables(
      target,
      query,
      cursor,
      cursor ? snapshot?.digest : undefined,
      signal,
    );
    signal.throwIfAborted();
    snapshot = {
      digest: page.contextPin.digest,
      query,
      cursor: page.nextPageToken,
    };
    onPin?.(page.contextPin);
    return {
      items: page.items.map(toTemplateVariablePickerItem),
      total: page.total,
      nextCursor: page.nextPageToken,
    };
  };
}
export async function previewContextPrompt(
  target: PromptTarget,
  template: string,
  signal: AbortSignal,
  full = false,
): Promise<PromptTemplatePreview> {
  const catalog = await readPromptVariables(
    target,
    "",
    undefined,
    undefined,
    signal,
  );
  const preview = (
    await unwrap(
      previewPromptTemplate({
        body: {
          template,
          targetKind: target.targetKind,
          targetRef: target.targetRef,
          context: target.context,
          expectedContextDigest: catalog.contextPin.digest,
          includeFullMaterialization: full,
        },
        headers: { "X-CSRF-Token": csrfToken() },
        signal: requestSignal(signal),
        cache: "no-store",
      }),
    )
  ).data;
  if (
    checkedPromptPin(preview.contextPin, target).digest !==
    catalog.contextPin.digest
  )
    throw new Error("Prompt preview context changed");
  if (!full && preview.fullMaterializedPrompt !== undefined)
    throw new Error("Unexpected full prompt disclosure");
  if (
    target.targetKind === "SESSION_CONTINUATION" &&
    (preview.runtimeDiff?.sessionRef !== target.targetRef ||
      preview.runtimeDiff.previousRevisionRef !==
        catalog.contextPin.previousRuntimeRevisionRef)
  )
    throw new Error("Prompt continuation runtime identity mismatch");
  return preview;
}
export async function validateContextPrompt(
  target: PromptTarget,
  template: string,
  signal: AbortSignal,
): Promise<PromptTemplateValidation> {
  const catalog = await readPromptVariables(
    target,
    "",
    undefined,
    undefined,
    signal,
  );
  const validation = (
    await unwrap(
      validatePromptTemplate({
        body: {
          template,
          targetKind: target.targetKind,
          targetRef: target.targetRef,
          context: target.context,
          expectedContextDigest: catalog.contextPin.digest,
        },
        headers: { "X-CSRF-Token": csrfToken() },
        signal: requestSignal(signal),
      }),
    )
  ).data;
  if (
    checkedPromptPin(validation.contextPin, target).digest !==
    catalog.contextPin.digest
  )
    throw new Error("Prompt validation context changed");
  return validation;
}
