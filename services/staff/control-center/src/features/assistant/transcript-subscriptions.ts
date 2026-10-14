interface TranscriptReads {
  loadRun: (ref: string, signal: AbortSignal) => Promise<void>;
  loadHistory: (ref: string, signal: AbortSignal) => Promise<void>;
  ready: (ref: string) => boolean;
  sequence: (ref: string) => number;
  historySequence: (ref: string) => number;
  acquire: (ref: string) => () => void;
}

interface TranscriptRead {
  controller: AbortController;
  release?: () => void;
  running: boolean;
  attemptedSequence?: number;
}

// Единственный планировщик открытого чата: новые Run первыми, не более двух
// чтений, один inflight на Run. Смена scope отменяет только собственные leases.
export function createAssistantTranscriptSubscriptions(reads: TranscriptReads) {
  const entries = new Map<string, TranscriptRead>();
  let scope: string | undefined;
  let order: string[] = [];
  let running = 0;

  function needsRead(ref: string, entry: TranscriptRead): boolean {
    if (entry.running || entry.controller.signal.aborted) return false;
    const sequence = reads.sequence(ref);
    return (
      entry.attemptedSequence !== sequence &&
      (!entry.release || reads.historySequence(ref) < sequence)
    );
  }

  function pump(): void {
    for (const ref of order) {
      const entry = entries.get(ref);
      if (running >= 2) return;
      if (!entry || !needsRead(ref, entry)) continue;
      entry.running = true;
      running += 1;
      void (async () => {
        try {
          if (!reads.ready(ref))
            await reads.loadRun(ref, entry.controller.signal);
          if (entry.controller.signal.aborted) return;
          entry.attemptedSequence = reads.sequence(ref);
          if (!reads.ready(ref)) return;
          entry.release ??= reads.acquire(ref);
          if (reads.historySequence(ref) < reads.sequence(ref))
            await reads.loadHistory(ref, entry.controller.signal);
        } finally {
          entry.running = false;
          running -= 1;
          pump();
        }
      })();
    }
  }

  function sync(nextScope: string | undefined, refs: readonly string[]): void {
    const wanted = new Set(nextScope === undefined ? [] : refs);
    for (const [ref, entry] of entries) {
      if (scope === nextScope && wanted.has(ref)) continue;
      entry.controller.abort();
      entry.release?.();
      entries.delete(ref);
    }
    scope = nextScope;
    order = [...wanted].reverse();
    for (const ref of order)
      if (!entries.has(ref))
        entries.set(ref, { controller: new AbortController(), running: false });
    pump();
  }

  function retry(ref: string): void {
    const entry = entries.get(ref);
    if (!entry || entry.running) return;
    entry.attemptedSequence = undefined;
    pump();
  }

  return { sync, retry, close: () => sync(undefined, []) };
}
