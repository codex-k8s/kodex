import { describe, expect, it, vi } from "vitest";

import {
  authoritativeRunRefreshKey,
  createRunRefreshScheduler,
} from "@/features/platform/run-refresh";
import type { Run, RunEvent } from "@/shared/api/generated/openapi/types.gen";

function run(state: Run["state"], lastEventSequence: number): Run {
  return {
    ref: "run_refresh01",
    rootRunRef: "run_refresh01",
    projectRef: "prj_refresh01",
    sessionRef: "ses_refresh01",
    target: {
      type: "AGENT",
      ref: "agt_refresh01",
      displayName: "Координатор",
      version: 1,
    },
    title: "Проверка обновления",
    source: "CONTROL_CENTER",
    initiator: { ref: "usr_refresh01", displayName: "Владелец" },
    state,
    attempt: 1,
    version: lastEventSequence,
    graphRevision: lastEventSequence,
    lastEventSequence,
    usage: {
      totalTokens: 0,
      inputTokens: 0,
      cachedInputTokens: 0,
      cacheWriteInputTokens: 0,
      outputTokens: 0,
      reasoningOutputTokens: 0,
      modelContextWindow: 0,
    },
    inputArtifactRefs: [],
    artifactRefs: [],
    gateRefs: [],
    incidents: [],
    createdAt: "2026-08-27T00:00:00Z",
    nextActions: [],
  };
}

function event(type: RunEvent["type"], sequence: number): RunEvent {
  return {
    ref: `evt_refresh${String(sequence)}`,
    runRef: "run_refresh01",
    sequence,
    type,
    summary: "Состояние изменено",
    occurredAt: "2026-08-27T00:00:00Z",
    graphRevision: sequence,
    run: {
      ref: "run_refresh01",
      version: sequence,
      state: "RUNNING",
      graphRevision: sequence,
      lastEventSequence: sequence,
      usage: run("RUNNING", sequence).usage,
      artifactRefs: [],
      gateRefs: [],
      nextActions: [],
    },
  };
}

function deferred(): { promise: Promise<void>; resolve: () => void } {
  let resolve!: () => void;
  const promise = new Promise<void>((ready) => {
    resolve = ready;
  });
  return { promise, resolve };
}

function retryableProblem(retryable = true): Error & { retryable: boolean } {
  return Object.assign(new Error("refresh failed"), { retryable });
}

describe("authoritative Run refresh", () => {
  it("запрашивает refresh только после terminal state или появления артефакта", () => {
    expect(authoritativeRunRefreshKey(run("RUNNING", 1), {})).toBeUndefined();
    expect(
      authoritativeRunRefreshKey(run("RUNNING", 2), {
        2: event("ARTIFACT_AVAILABLE", 2),
      }),
    ).toBe("run_refresh01:2");
    expect(authoritativeRunRefreshKey(run("SUCCEEDED", 3), {})).toBe(
      "run_refresh01:3",
    );
  });

  it("объединяет повторные запросы во время refresh и не создаёт цикл", async () => {
    const first = deferred();
    const calls: string[] = [];
    const refresh = vi.fn(async (runRef: string) => {
      calls.push(runRef);
      if (calls.length === 1) await first.promise;
    });
    const scheduler = createRunRefreshScheduler(refresh);

    const initial = scheduler.request("run_first01");
    const duplicate = scheduler.request("run_first01");
    const latest = scheduler.request("run_latest01");
    const lateDuplicate = scheduler.request("run_first01");
    expect(calls).toEqual(["run_first01"]);

    first.resolve();
    await Promise.all([initial, duplicate, latest, lateDuplicate]);
    expect(calls).toEqual(["run_first01", "run_latest01"]);

    await scheduler.request("run_after01");
    expect(calls).toEqual(["run_first01", "run_latest01", "run_after01"]);
    scheduler.dispose();
  });

  it("повторяет transient failure с ограниченным backoff", async () => {
    const refresh = vi
      .fn<(runRef: string) => Promise<void>>()
      .mockRejectedValueOnce(retryableProblem())
      .mockRejectedValueOnce(retryableProblem())
      .mockResolvedValue(undefined);
    const wait = vi.fn((delayMs: number, signal: AbortSignal) => {
      void delayMs;
      void signal;
      return Promise.resolve();
    });
    const scheduler = createRunRefreshScheduler(refresh, {
      retryDelaysMs: [100, 300, 500],
      shouldRetry: (error) =>
        typeof error === "object" &&
        error !== null &&
        "retryable" in error &&
        error.retryable === true,
      wait,
    });

    await scheduler.request("run_retry01");

    expect(refresh).toHaveBeenCalledTimes(3);
    expect(wait.mock.calls.map(([delay]) => delay)).toEqual([100, 300]);
    scheduler.dispose();
  });

  it("оставляет постоянную ошибку после ограниченного числа повторов", async () => {
    const refresh = vi.fn(() => Promise.reject(retryableProblem()));
    const wait = vi.fn((delayMs: number, signal: AbortSignal) => {
      void delayMs;
      void signal;
      return Promise.resolve();
    });
    const scheduler = createRunRefreshScheduler(refresh, {
      retryDelaysMs: [100, 300],
      shouldRetry: () => true,
      wait,
    });

    await scheduler.request("run_failed01");

    expect(refresh).toHaveBeenCalledTimes(3);
    expect(wait).toHaveBeenCalledTimes(2);
    scheduler.dispose();
  });

  it("не повторяет non-retryable failure", async () => {
    const refresh = vi.fn(() => Promise.reject(retryableProblem(false)));
    const wait = vi.fn((delayMs: number, signal: AbortSignal) => {
      void delayMs;
      void signal;
      return Promise.resolve();
    });
    const scheduler = createRunRefreshScheduler(refresh, {
      retryDelaysMs: [100, 300],
      shouldRetry: (error) =>
        typeof error === "object" &&
        error !== null &&
        "retryable" in error &&
        error.retryable === true,
      wait,
    });

    await scheduler.request("run_forbidden01");

    expect(refresh).toHaveBeenCalledOnce();
    expect(wait).not.toHaveBeenCalled();
    scheduler.dispose();
  });

  it("отменяет backoff при navigation и принимает новый run", async () => {
    const backoffStarted = deferred();
    const wait = vi.fn((_delay: number, signal: AbortSignal) => {
      backoffStarted.resolve();
      return new Promise<void>((resolve) => {
        signal.addEventListener("abort", () => resolve(), { once: true });
      });
    });
    const calls: string[] = [];
    const refresh = vi.fn((runRef: string) => {
      calls.push(runRef);
      return runRef === "run_old01"
        ? Promise.reject(retryableProblem())
        : Promise.resolve();
    });
    const scheduler = createRunRefreshScheduler(refresh, {
      retryDelaysMs: [100],
      shouldRetry: () => true,
      wait,
    });

    const oldRequest = scheduler.request("run_old01");
    await backoffStarted.promise;
    scheduler.cancel();
    await scheduler.request("run_new01");
    await oldRequest;

    expect(calls).toEqual(["run_old01", "run_new01"]);
    expect(wait.mock.calls[0]?.[1].aborted).toBe(true);
    scheduler.dispose();
  });

  it("после dispose не выполняет отложенный или новый refresh", async () => {
    const backoffStarted = deferred();
    const wait = vi.fn((_delay: number, signal: AbortSignal) => {
      backoffStarted.resolve();
      return new Promise<void>((resolve) => {
        signal.addEventListener("abort", () => resolve(), { once: true });
      });
    });
    const refresh = vi.fn(() => Promise.reject(retryableProblem()));
    const scheduler = createRunRefreshScheduler(refresh, {
      retryDelaysMs: [100],
      shouldRetry: () => true,
      wait,
    });

    const request = scheduler.request("run_dispose01");
    await backoffStarted.promise;
    scheduler.dispose();
    await request;
    await scheduler.request("run_after_dispose01");

    expect(refresh).toHaveBeenCalledOnce();
  });
});
