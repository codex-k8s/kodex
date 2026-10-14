import { describe, expect, it, vi } from "vitest";
import { createAssistantTranscriptSubscriptions } from "./transcript-subscriptions";

function deferred() {
  let resolve!: () => void;
  const promise = new Promise<void>((done) => {
    resolve = done;
  });
  return { promise, resolve };
}

function fixture() {
  const graph = new Map<string, number>();
  const history = new Map<string, number>();
  const releases = new Map<string, ReturnType<typeof vi.fn>>();
  const reads = {
    loadRun: vi.fn((ref: string, signal: AbortSignal) => {
      expect(signal.aborted).toBe(false);
      graph.set(ref, 1);
      return Promise.resolve();
    }),
    loadHistory: vi.fn((ref: string, signal: AbortSignal) => {
      expect(signal.aborted).toBe(false);
      history.set(ref, graph.get(ref) ?? -1);
      return Promise.resolve();
    }),
    ready: (ref: string) => graph.has(ref),
    sequence: (ref: string) => graph.get(ref) ?? -1,
    historySequence: (ref: string) => history.get(ref) ?? 0,
    acquire: vi.fn((ref: string) => {
      const release = vi.fn();
      releases.set(ref, release);
      return release;
    }),
  };
  return {
    graph,
    history,
    releases,
    reads,
    subscriptions: createAssistantTranscriptSubscriptions(reads),
  };
}

describe("чтения transcript открытого чата", () => {
  it("ставит новые Run первыми и не блокирует их старой историей при concurrency2", async () => {
    const value = fixture();
    const pending = new Map(
      ["old", "middle", "new"].map((ref) => [ref, deferred()]),
    );
    value.reads.loadHistory.mockImplementation(async (ref) => {
      await pending.get(ref)?.promise;
      value.history.set(ref, 1);
    });
    value.subscriptions.sync("conversation", ["old", "middle", "new"]);
    await vi.waitFor(() =>
      expect(value.reads.loadHistory).toHaveBeenCalledTimes(2),
    );
    expect(value.reads.loadRun.mock.calls.map(([ref]) => ref)).toEqual([
      "new",
      "middle",
    ]);
    pending.get("new")?.resolve();
    await vi.waitFor(() =>
      expect(value.reads.loadHistory).toHaveBeenCalledTimes(3),
    );
    expect(value.history.get("new")).toBe(1);
    expect(value.history.get("old")).toBeUndefined();
    pending.get("middle")?.resolve();
    pending.get("old")?.resolve();
    await vi.waitFor(() => expect(value.history.get("old")).toBe(1));
    value.subscriptions.close();
  });

  it("объединяет обновления snapshot и догружает новый cursor после одного inflight", async () => {
    const value = fixture(),
      pending = deferred();
    value.graph.set("run", 7);
    value.reads.loadHistory.mockImplementationOnce(async () => {
      await pending.promise;
      value.history.set("run", 7);
    });
    value.subscriptions.sync("conversation", ["run"]);
    value.graph.set("run", 9);
    value.subscriptions.sync("conversation", ["run"]);
    value.subscriptions.sync("conversation", ["run"]);
    expect(value.reads.loadHistory).toHaveBeenCalledTimes(1);
    pending.resolve();
    await vi.waitFor(() => expect(value.history.get("run")).toBe(9));
    expect(value.reads.loadHistory).toHaveBeenCalledTimes(2);
    expect(value.reads.acquire).toHaveBeenCalledTimes(1);
    expect(value.reads.loadRun).not.toHaveBeenCalled();
    value.subscriptions.sync("conversation", ["run"]);
    expect(value.reads.loadHistory).toHaveBeenCalledTimes(2);
    value.subscriptions.close();
  });

  it("догружает историю после пустого initial run и full snapshot без RUN_EVENT", async () => {
    const value = fixture();
    value.graph.set("run", 0);
    value.subscriptions.sync("conversation", ["run"]);
    expect(value.reads.loadHistory).not.toHaveBeenCalled();
    value.graph.set("run", 9);
    value.subscriptions.sync("conversation", ["run"]);
    await vi.waitFor(() => expect(value.history.get("run")).toBe(9));
    expect(value.reads.acquire).toHaveBeenCalledTimes(1);
    value.subscriptions.close();
  });

  it("не скрывает сбой бесконечным retry и повторяет только явный запрос", async () => {
    const value = fixture();
    value.graph.set("run", 9);
    value.reads.loadHistory.mockImplementationOnce(() => Promise.resolve());
    value.subscriptions.sync("conversation", ["run"]);
    await Promise.resolve();
    value.subscriptions.sync("conversation", ["run"]);
    expect(value.reads.loadHistory).toHaveBeenCalledTimes(1);
    value.subscriptions.retry("run");
    await vi.waitFor(() => expect(value.history.get("run")).toBe(9));
    expect(value.reads.loadHistory).toHaveBeenCalledTimes(2);
    value.subscriptions.close();
  });

  it("не приобретает WS lease при неуспешном initial read, но допускает явный retry", async () => {
    const value = fixture();
    value.reads.loadRun.mockImplementationOnce(() => Promise.resolve());
    value.subscriptions.sync("conversation", ["run"]);
    await Promise.resolve();
    value.subscriptions.sync("conversation", ["run"]);
    expect(value.reads.loadRun).toHaveBeenCalledTimes(1);
    expect(value.reads.acquire).not.toHaveBeenCalled();
    expect(value.reads.loadHistory).not.toHaveBeenCalled();
    value.subscriptions.retry("run");
    await vi.waitFor(() => expect(value.history.get("run")).toBe(1));
    expect(value.reads.loadRun).toHaveBeenCalledTimes(2);
    value.subscriptions.close();
  });

  it("смена conversation/закрытие отменяет только собственные чтения и leases", async () => {
    const value = fixture(),
      pending = deferred();
    value.graph.set("old", 9);
    value.reads.loadHistory.mockImplementationOnce(async (_ref, signal) => {
      await pending.promise;
      expect(signal.aborted).toBe(true);
    });
    value.subscriptions.sync("old-conversation", ["old"]);
    const oldSignal = value.reads.loadHistory.mock.calls[0]?.[1];
    value.subscriptions.sync("new-conversation", ["new"]);
    expect(oldSignal?.aborted).toBe(true);
    expect(value.releases.get("old")).toHaveBeenCalledTimes(1);
    await vi.waitFor(() => expect(value.history.get("new")).toBe(1));
    pending.resolve();
    await Promise.resolve();
    expect(value.reads.acquire.mock.calls.map(([ref]) => ref)).toEqual([
      "old",
      "new",
    ]);
    value.subscriptions.close();
    expect(value.releases.get("new")).toHaveBeenCalledTimes(1);
  });

  it("удаление одного Run не отменяет соседнюю сохранённую подписку", () => {
    const value = fixture();
    value.graph.set("first", 0);
    value.graph.set("second", 0);
    value.subscriptions.sync("conversation", ["first", "second"]);
    value.subscriptions.sync("conversation", ["second"]);
    expect(value.releases.get("first")).toHaveBeenCalledTimes(1);
    expect(value.releases.get("second")).not.toHaveBeenCalled();
    value.subscriptions.close();
  });
});
