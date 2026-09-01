import type {
  AttachmentSet,
  AttachmentSetPurpose,
} from "@/shared/api/generated/openapi/types.gen";
import {
  addAttachmentItems,
  attachmentMutationBatches,
  createAttachmentDraft,
  finalizeAttachmentDraft,
  removeAttachmentItems,
} from "@/shared/api/attachment-sets";

export interface AttachmentSetDraftStoreState {
  attachmentSet?: AttachmentSet;
  references: string[];
  busy: boolean;
  error?: unknown;
}

export interface AttachmentSetDraftStore {
  state: () => AttachmentSetDraftStoreState;
  reconcile: (references: readonly string[]) => Promise<void>;
  retry: () => Promise<void>;
  finalize: () => Promise<string | undefined>;
  reset: () => void;
}

function uniqueReferences(references: readonly string[]): string[] {
  return [...new Set(references.filter(Boolean))];
}

function operationError(cause: unknown): Error {
  return cause instanceof Error
    ? cause
    : new Error("AttachmentSet operation failed");
}

export function createAttachmentSetDraftStore(options: {
  projectRef: () => string | undefined;
  purpose: () => AttachmentSetPurpose;
  changed: () => void;
}): AttachmentSetDraftStore {
  let attachmentSet: AttachmentSet | undefined;
  let references: string[] = [];
  let desiredReferences: string[] = [];
  let busy = false;
  let pending = 0;
  let error: unknown;
  let generation = 0;
  let controller = new AbortController();
  let queue = Promise.resolve();

  function changed(): void {
    options.changed();
  }

  function state(): AttachmentSetDraftStoreState {
    return {
      attachmentSet,
      references: [...references],
      busy,
      error,
    };
  }

  async function apply(generationAtStart: number): Promise<void> {
    if (generationAtStart !== generation) return;
    const projectRef = options.projectRef();
    const purpose = options.purpose();
    if (!projectRef && purpose !== "ASSISTANT_MESSAGE") {
      if (desiredReferences.length)
        throw new Error("AttachmentSet purpose requires a Project context");
      return;
    }
    if (!attachmentSet && desiredReferences.length)
      attachmentSet = await createAttachmentDraft(
        projectRef,
        purpose,
        controller.signal,
      );
    if (generationAtStart !== generation || !attachmentSet) return;
    if (
      attachmentSet.state === "FINALIZED" &&
      (references.length !== desiredReferences.length ||
        references.some(
          (reference, index) => reference !== desiredReferences[index],
        ))
    ) {
      attachmentSet = await createAttachmentDraft(
        projectRef,
        purpose,
        controller.signal,
      );
      references = [];
    }

    const desired = new Set(desiredReferences);
    const removals = references.filter((reference) => !desired.has(reference));
    for (const batch of attachmentMutationBatches(removals)) {
      attachmentSet = await removeAttachmentItems(
        attachmentSet,
        batch,
        controller.signal,
      );
      references = references.filter((reference) => !batch.includes(reference));
      if (generationAtStart !== generation) return;
    }

    const current = new Set(references);
    const additions = desiredReferences.filter(
      (reference) => !current.has(reference),
    );
    for (const batch of attachmentMutationBatches(additions)) {
      attachmentSet = await addAttachmentItems(
        attachmentSet,
        batch,
        references.length,
        controller.signal,
      );
      references.push(...batch);
      if (generationAtStart !== generation) return;
    }
  }

  function schedule(): Promise<void> {
    const generationAtStart = generation;
    pending += 1;
    busy = true;
    error = undefined;
    changed();
    const operation = queue.then(() => apply(generationAtStart));
    queue = operation
      .catch((cause: unknown) => {
        if (generationAtStart === generation) error = cause;
      })
      .finally(() => {
        if (generationAtStart === generation) {
          pending = Math.max(0, pending - 1);
          busy = pending > 0;
          changed();
        }
      });
    return queue;
  }

  async function reconcile(nextReferences: readonly string[]): Promise<void> {
    desiredReferences = uniqueReferences(nextReferences);
    await schedule();
  }

  async function retry(): Promise<void> {
    await schedule();
  }

  async function finalize(): Promise<string | undefined> {
    await schedule();
    if (error) throw operationError(error);
    if (!desiredReferences.length) return undefined;
    if (
      !attachmentSet ||
      references.length !== desiredReferences.length ||
      references.some(
        (reference, index) => reference !== desiredReferences[index],
      )
    )
      throw new Error("AttachmentSet draft is not synchronized");
    if (attachmentSet.state === "FINALIZED") return attachmentSet.ref;
    pending += 1;
    busy = true;
    changed();
    try {
      attachmentSet = await finalizeAttachmentDraft(
        attachmentSet,
        controller.signal,
      );
      return attachmentSet.ref;
    } catch (cause) {
      error = cause;
      throw cause;
    } finally {
      pending = Math.max(0, pending - 1);
      busy = pending > 0;
      changed();
    }
  }

  function reset(): void {
    generation += 1;
    controller.abort();
    controller = new AbortController();
    queue = Promise.resolve();
    attachmentSet = undefined;
    references = [];
    desiredReferences = [];
    busy = false;
    pending = 0;
    error = undefined;
    changed();
  }

  return { finalize, reconcile, reset, retry, state };
}
