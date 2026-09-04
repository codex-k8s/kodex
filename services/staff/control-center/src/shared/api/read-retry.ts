import { asProblem, type AppProblem } from "@/shared/api/problem";

const readRetryDelaysMs = [0, 200, 600, 1_500] as const;

export async function readWithRetry<T>(
  request: () => Promise<T>,
  delaysMs: readonly number[] = readRetryDelaysMs,
): Promise<T> {
  let lastProblem: AppProblem | undefined;
  for (const delayMs of delaysMs) {
    if (delayMs > 0) await delay(delayMs);
    try {
      return await request();
    } catch (error) {
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

function delay(milliseconds: number): Promise<void> {
  return new Promise((resolve) => globalThis.setTimeout(resolve, milliseconds));
}
