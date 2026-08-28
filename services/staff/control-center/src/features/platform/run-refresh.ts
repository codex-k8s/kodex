import type { Run, RunEvent } from "@/shared/api/generated/openapi/types.gen";

const terminalStates = new Set<Run["state"]>([
  "SUCCEEDED",
  "FAILED",
  "CANCELLED",
]);

const defaultRetryDelaysMs = [250, 750, 1_500] as const;

export function authoritativeRunRefreshKey(
  run: Run | undefined,
  events: Record<number, RunEvent>,
): string | undefined {
  if (!run) return undefined;
  let sequence = terminalStates.has(run.state) ? run.lastEventSequence : 0;
  for (const event of Object.values(events)) {
    if (
      event.type === "ARTIFACT_AVAILABLE" ||
      terminalStates.has(event.run.state)
    )
      sequence = Math.max(sequence, event.sequence);
  }
  return sequence > 0 ? `${run.rootRunRef}:${String(sequence)}` : undefined;
}

export interface RunRefreshScheduler {
  request(runRef: string): Promise<void>;
  cancel(): void;
  dispose(): void;
}

interface RunRefreshSchedulerOptions {
  retryDelaysMs?: readonly number[];
  shouldRetry?: (error: unknown) => boolean;
  wait?: (delayMs: number, signal: AbortSignal) => Promise<void>;
}

function waitForDelay(delayMs: number, signal: AbortSignal): Promise<void> {
  if (signal.aborted) return Promise.resolve();
  return new Promise((resolve) => {
    const finish = () => {
      window.clearTimeout(timer);
      signal.removeEventListener("abort", finish);
      resolve();
    };
    const timer = window.setTimeout(finish, delayMs);
    signal.addEventListener("abort", finish, { once: true });
  });
}

export function createRunRefreshScheduler(
  refresh: (runRef: string) => Promise<void>,
  options: RunRefreshSchedulerOptions = {},
): RunRefreshScheduler {
  const retryDelaysMs = options.retryDelaysMs ?? defaultRetryDelaysMs;
  const shouldRetry = options.shouldRetry ?? (() => false);
  const wait = options.wait ?? waitForDelay;
  let active: { epoch: number; task: Promise<void> } | undefined;
  let activeRunRef: string | undefined;
  let pendingRunRef: string | undefined;
  let retryController: AbortController | undefined;
  let epoch = 0;
  let disposed = false;

  function isCurrent(currentEpoch: number): boolean {
    return !disposed && currentEpoch === epoch;
  }

  async function refreshWithRetry(
    runRef: string,
    currentEpoch: number,
  ): Promise<void> {
    for (let attempt = 0; ; attempt += 1) {
      if (!isCurrent(currentEpoch)) return;
      try {
        await refresh(runRef);
        return;
      } catch (error) {
        const delayMs = retryDelaysMs[attempt];
        if (
          delayMs === undefined ||
          !shouldRetry(error) ||
          !isCurrent(currentEpoch)
        )
          return;
        const controller = new AbortController();
        retryController = controller;
        await wait(delayMs, controller.signal);
        if (retryController === controller) retryController = undefined;
      }
    }
  }

  async function drain(currentEpoch: number): Promise<void> {
    while (isCurrent(currentEpoch) && pendingRunRef) {
      const nextRunRef = pendingRunRef;
      pendingRunRef = undefined;
      activeRunRef = nextRunRef;
      await refreshWithRetry(nextRunRef, currentEpoch);
      if (isCurrent(currentEpoch)) activeRunRef = undefined;
    }
  }

  function startDrain(currentEpoch: number): Promise<void> {
    const task = drain(currentEpoch);
    active = { epoch: currentEpoch, task };
    void task.finally(() => {
      if (active?.task !== task) return;
      active = undefined;
      activeRunRef = undefined;
      if (!disposed && pendingRunRef) void startDrain(epoch);
    });
    return task;
  }

  function cancel(): void {
    epoch += 1;
    pendingRunRef = undefined;
    activeRunRef = undefined;
    retryController?.abort();
    retryController = undefined;
    active = undefined;
  }

  return {
    request(runRef: string): Promise<void> {
      if (disposed) return Promise.resolve();
      if (active?.epoch === epoch && activeRunRef === runRef)
        return active.task;
      pendingRunRef = runRef;
      if (!active || active.epoch !== epoch) return startDrain(epoch);
      return active.task;
    },
    cancel,
    dispose(): void {
      if (disposed) return;
      disposed = true;
      cancel();
    },
  };
}
