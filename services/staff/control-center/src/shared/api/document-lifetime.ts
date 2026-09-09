// Время жизни документа отдельно от серверной сессии и owner generation.
export const documentRequestsSuspended = "kodex:document-requests-suspended";
export const documentRequestsResumed = "kodex:document-requests-resumed";
let controller = new AbortController();
let dispose: (() => void) | undefined;
const retainedRequestSignals = new WeakMap<Request, readonly AbortSignal[]>();

export function documentRequestSignal(): AbortSignal {
  return controller.signal;
}

export function retainRequestSignalParents(
  request: Request,
  source: Request,
): Request {
  retainedRequestSignals.set(request, [
    source.signal,
    ...(retainedRequestSignals.get(source) ?? []),
  ]);
  return request;
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

function linkedRequestSignal(signals: readonly AbortSignal[]): {
  signal: AbortSignal;
  dispose: () => void;
} {
  const linked = new AbortController();
  const abort = (event: Event) => {
    const source = event.target;
    if (source instanceof AbortSignal && !linked.signal.aborted)
      linked.abort(source.reason);
  };
  for (const signal of signals) {
    if (signal.aborted) {
      linked.abort(signal.reason);
      break;
    }
    signal.addEventListener("abort", abort, { once: true });
  }
  return {
    signal: linked.signal,
    dispose: () => {
      for (const signal of signals) signal.removeEventListener("abort", abort);
    },
  };
}

// Между interceptor и native fetch есть await в generated client. Повторно
// проверяем scope именно здесь: уже закрытый документ не вызывает native fetch.
export const documentFetch: typeof fetch = (input, init) => {
  const request = new Request(input, init);
  const linked = linkedRequestSignal([
    request.signal,
    ...(input instanceof Request
      ? (retainedRequestSignals.get(input) ?? [])
      : []),
    documentRequestSignal(),
  ]);
  const { signal } = linked;
  if (signal.aborted) {
    linked.dispose();
    const reason: unknown = signal.reason;
    return Promise.reject(
      reason instanceof Error
        ? reason
        : new DOMException("Document request aborted", "AbortError"),
    );
  }
  try {
    return globalThis.fetch(request, { signal }).finally(linked.dispose);
  } catch (error) {
    linked.dispose();
    throw error;
  }
};
