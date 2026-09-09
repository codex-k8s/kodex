// Время жизни документа отдельно от серверной сессии и owner generation.
export const documentRequestsSuspended = "kodex:document-requests-suspended";
export const documentRequestsResumed = "kodex:document-requests-resumed";
let controller = new AbortController();
let dispose: (() => void) | undefined;

export function documentRequestSignal(): AbortSignal {
  return controller.signal;
}

export function installDocumentRequestLifetime(
  target: Window = window,
): () => void {
  if (dispose) return dispose;
  if (controller.signal.aborted) controller = new AbortController();
  const suspend = () => {
    if (controller.signal.aborted) return;
    controller.abort();
    target.dispatchEvent(new Event(documentRequestsSuspended));
  };
  const resume = () => {
    if (!controller.signal.aborted) return;
    controller = new AbortController();
    target.dispatchEvent(new Event(documentRequestsResumed));
  };
  const interact = (event: Event) => {
    if (event.isTrusted && target.document.visibilityState !== "hidden")
      resume();
  };
  target.addEventListener("beforeunload", suspend);
  target.addEventListener("pagehide", suspend);
  target.addEventListener("pageshow", resume);
  target.addEventListener("pointerdown", interact, true);
  target.addEventListener("keydown", interact, true);
  dispose = () => {
    target.removeEventListener("beforeunload", suspend);
    target.removeEventListener("pagehide", suspend);
    target.removeEventListener("pageshow", resume);
    target.removeEventListener("pointerdown", interact, true);
    target.removeEventListener("keydown", interact, true);
    suspend();
    dispose = undefined;
  };
  return dispose;
}

// Между interceptor и native fetch есть await в generated client. Повторно
// проверяем scope именно здесь: уже закрытый документ не вызывает native fetch.
export const documentFetch: typeof fetch = (input, init) => {
  const request = new Request(input, init);
  const signal = AbortSignal.any([request.signal, documentRequestSignal()]);
  if (signal.aborted) {
    const reason: unknown = signal.reason;
    return Promise.reject(
      reason instanceof Error
        ? reason
        : new DOMException("Document request aborted", "AbortError"),
    );
  }
  return globalThis.fetch(new Request(request, { signal }));
};
