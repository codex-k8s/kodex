import { asProblem, type AppProblem } from "@/shared/api/problem";
import { assertOwnerRequest, ownerRequestSignal } from "./owner-lifetime";

const readRetryDelaysMs = [0, 200, 600, 1_500] as const;

export async function readWithRetry<T>(
  request: () => Promise<T>,
  delaysMs: readonly number[] = readRetryDelaysMs,
  signal?: AbortSignal,
): Promise<T> {
  let lastProblem: AppProblem | undefined;
  const scope = ownerRequestSignal();
  for (const delayMs of delaysMs) {
    signal?.throwIfAborted();
    if (delayMs > 0) await delay(delayMs, signal);
    try {
      assertOwnerRequest(scope);
      signal?.throwIfAborted();
      const value = await request();
      assertOwnerRequest(scope);
      signal?.throwIfAborted();
      return value;
    } catch (error) {
      assertOwnerRequest(scope);
      signal?.throwIfAborted();
      lastProblem = asProblem(error);
      if (
        !lastProblem.retryable ||
        lastProblem.kind === "unauthorized" ||
        lastProblem.kind === "forbidden" ||
        delayMs === delaysMs.at(-1)
      ) {
        throw lastProblem;
      }
    }
  }
  throw lastProblem ?? asProblem(new Error("Read request did not start"));
}

function delay(milliseconds: number, signal?: AbortSignal): Promise<void> {
  return new Promise((resolve, reject) => {
    const timer = globalThis.setTimeout(() => {
      signal?.removeEventListener("abort", abort);
      resolve();
    }, milliseconds);
    function abort(): void {
      globalThis.clearTimeout(timer);
      signal?.removeEventListener("abort", abort);
      const reason: unknown = signal?.reason;
      reject(
        reason instanceof Error
          ? reason
          : new DOMException("Read request aborted", "AbortError"),
      );
    }
    signal?.addEventListener("abort", abort, { once: true });
  });
}
