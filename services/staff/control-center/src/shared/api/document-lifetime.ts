// Время жизни документа отдельно от серверной сессии и owner generation.
export const documentRequestsSuspended = "kodex:document-requests-suspended";
export const documentRequestsResumed = "kodex:document-requests-resumed";
let controller = new AbortController();
let dispose: (() => void) | undefined;
const retainedRequestSignals = new WeakMap<Request, readonly AbortSignal[]>();
const responseFinalizer = new FinalizationRegistry<() => void>((dispose) =>
  dispose(),
);

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
  cancel: (reason?: unknown) => void;
  dispose: () => void;
} {
  const linked = new AbortController();
  let disposed = false;
  const dispose = () => {
    if (disposed) return;
    disposed = true;
    for (const signal of signals) signal.removeEventListener("abort", abort);
  };
  const abort = (event: Event) => {
    const source = event.target;
    if (source instanceof AbortSignal && !linked.signal.aborted) {
      linked.abort(source.reason);
      dispose();
    }
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
    cancel: (reason?: unknown) => {
      if (!linked.signal.aborted) linked.abort(reason);
      dispose();
    },
    dispose,
  };
}

function responseWithLifetime(
  response: Response,
  signal: AbortSignal,
  cancelTransport: (reason?: unknown) => void,
  disposeSignals: () => void,
): Response {
  if (!response.body) {
    disposeSignals();
    return response;
  }
  const reader = response.body.getReader();
  const finalizerToken = {};
  let finished = false;
  const finish = () => {
    if (finished) return;
    finished = true;
    disposeSignals();
    responseFinalizer.unregister(finalizerToken);
  };
  const body = new ReadableStream<Uint8Array>({
    async pull(controller) {
      try {
        const result = await reader.read();
        if (result.done) {
          finish();
          controller.close();
          return;
        }
        controller.enqueue(result.value);
      } catch (error) {
        finish();
        const reason: unknown = signal.reason;
        controller.error(
          signal.aborted
            ? reason instanceof Error
              ? reason
              : new DOMException("Document request aborted", "AbortError")
            : error,
        );
      }
    },
    async cancel(reason) {
      cancelTransport(
        reason instanceof Error
          ? reason
          : new DOMException("Response body cancelled", "AbortError"),
      );
      try {
        await reader.cancel(reason);
      } catch (error) {
        if (!signal.aborted) throw error;
      } finally {
        finish();
      }
    },
  });
  const consumer = new Response(body, {
    headers: response.headers,
    status: response.status,
    statusText: response.statusText,
  });
  const consumerMethods = new Set<PropertyKey>([
    "arrayBuffer",
    "blob",
    "bytes",
    "formData",
    "json",
    "text",
  ]);
  const cancellationError = (error: unknown): unknown => {
    if (!signal.aborted) return error;
    const reason: unknown = signal.reason;
    return reason instanceof Error
      ? reason
      : new DOMException("Document request aborted", "AbortError");
  };
  const view = (bodyResponse: Response): Response =>
    new Proxy(response, {
      get(target, property) {
        if (property === "body") return bodyResponse.body;
        if (property === "bodyUsed") return bodyResponse.bodyUsed;
        if (consumerMethods.has(property)) {
          const method = Reflect.get(bodyResponse, property, bodyResponse);
          if (typeof method !== "function") return method;
          return async (...args: unknown[]) => {
            try {
              return await Reflect.apply(method, bodyResponse, args);
            } catch (error) {
              throw cancellationError(error);
            }
          };
        }
        if (property === "clone") return () => view(bodyResponse.clone());
        const value = Reflect.get(target, property, target);
        return typeof value === "function" ? value.bind(target) : value;
      },
    });
  // Reader удерживает сам stream, поэтому cleanup не срабатывает, пока body
  // читается даже если вызывающий больше не хранит объект Response.
  responseFinalizer.register(body, finish, finalizerToken);
  return view(consumer);
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
    return globalThis.fetch(request, { signal }).then(
      (response) =>
        responseWithLifetime(response, signal, linked.cancel, linked.dispose),
      (error: unknown) => {
        linked.dispose();
        throw error;
      },
    );
  } catch (error) {
    linked.dispose();
    throw error;
  }
};
