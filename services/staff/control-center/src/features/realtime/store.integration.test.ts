import { createPinia, setActivePinia } from "pinia";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type {
  RunEvent,
  RunGraph,
} from "@/shared/api/generated/openapi/types.gen";

vi.mock("@/shared/config/runtime", () => ({
  runtimeConfig: () => ({ realtimeUrl: "wss://kodex.example/api/v1" }),
}));
vi.mock("@/shared/api/mutation", () => ({
  csrfToken: () => "csrf-test-value-with-sufficient-length-000000000000",
}));
vi.mock("@/shared/locale", () => ({ currentLocale: () => "ru" }));

import { usePlatformStore } from "@/features/platform/store";
import { useRealtimeStore } from "@/features/realtime/store";

type SocketListener = (event: { data?: string }) => void;

class FakeWebSocket {
  static readonly CONNECTING = 0;
  static readonly OPEN = 1;
  static readonly CLOSED = 3;
  static instances: FakeWebSocket[] = [];

  readonly listeners = new Map<string, SocketListener[]>();
  readonly sent: string[] = [];
  readonly url: string;
  readonly protocols: string[];
  readyState = FakeWebSocket.CONNECTING;
  closeCode?: number;
  closeReason?: string;

  constructor(url: string, protocols: string[]) {
    this.url = url;
    this.protocols = protocols;
    FakeWebSocket.instances.push(this);
  }

  addEventListener(type: string, listener: SocketListener): void {
    this.listeners.set(type, [...(this.listeners.get(type) ?? []), listener]);
  }

  send(value: string): void {
    this.sent.push(value);
  }

  close(code = 1000, reason = ""): void {
    this.closeCode = code;
    this.closeReason = reason;
    this.readyState = FakeWebSocket.CLOSED;
    this.emit("close");
  }

  emit(type: string, data?: string): void {
    for (const listener of this.listeners.get(type) ?? []) listener({ data });
  }

  open(): void {
    this.readyState = FakeWebSocket.OPEN;
    this.emit("open");
  }

  message(value: unknown): void {
    this.emit("message", JSON.stringify(value));
  }
}

const scheduled: Array<() => void> = [];
const windowEvents = new Map<string, () => void>();
const authoritativeRunSequences = new Map<string, number>();

async function flushProcessing(): Promise<void> {
  for (let index = 0; index < 50; index += 1) await Promise.resolve();
}

function sent(socket: FakeWebSocket, index: number): Record<string, unknown> {
  return JSON.parse(socket.sent[index] ?? "{}") as Record<string, unknown>;
}

function socketAt(index: number): FakeWebSocket {
  const socket = FakeWebSocket.instances[index];
  if (!socket) throw new Error(`WebSocket ${String(index)} was not created`);
  return socket;
}

function graph(runRef: string, sequence = 1): RunGraph {
  return { runRef, revision: sequence, sequence, nodes: [], edges: [] };
}

function event(runRef: string, sequence: number): RunEvent {
  return {
    ref: `event_${runRef}_${String(sequence)}`,
    runRef,
    sequence,
    graphRevision: sequence,
    type: "RUN_STATE_CHANGED",
    summary: "Выполнение продолжается",
    occurredAt: "2026-08-23T00:00:03Z",
    run: {
      ref: runRef,
      version: sequence,
      state: "RUNNING",
      graphRevision: sequence,
      lastEventSequence: sequence,
      usage: {
        totalTokens: 0,
        inputTokens: 0,
        cachedInputTokens: 0,
        cacheWriteInputTokens: 0,
        outputTokens: 0,
        reasoningOutputTokens: 0,
        modelContextWindow: 0,
      },
      artifactRefs: [],
      gateRefs: [],
      nextActions: ["OPEN", "CANCEL"],
    },
  };
}

function hydrateRun(
  platform: ReturnType<typeof usePlatformStore>,
  runRef: string,
  sequence: number,
): void {
  platform.graphs[runRef] = graph(runRef, sequence);
  platform.events[runRef] = Object.fromEntries(
    Array.from({ length: sequence }, (_, index) => {
      const next = index + 1;
      return [next, event(runRef, next)];
    }),
  );
}

function resumeRequest(socket: FakeWebSocket): Record<string, unknown> {
  return sent(socket, 0);
}

function requestRef(socket: FakeWebSocket): string {
  return String(resumeRequest(socket).requestRef);
}

function runEnvelope(
  socket: FakeWebSocket,
  runRef: string,
  value: Record<string, unknown>,
): Record<string, unknown> {
  return {
    requestRef: requestRef(socket),
    streamKind: "RUN",
    streamRef: runRef,
    ...value,
  };
}

function runSnapshot(
  socket: FakeWebSocket,
  runRef: string,
  sequence = 1,
): Record<string, unknown> {
  authoritativeRunSequences.set(runRef, sequence);
  return runEnvelope(socket, runRef, {
    type: "RUN_GRAPH_SNAPSHOT",
    cursor: sequence,
    snapshot: graph(runRef, sequence),
  });
}

describe("browser-session realtime multiplexer", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    FakeWebSocket.instances = [];
    scheduled.length = 0;
    windowEvents.clear();
    authoritativeRunSequences.clear();
    const platform = usePlatformStore();
    vi.spyOn(platform, "loadRun").mockImplementation((runRef) => {
      const sequence = authoritativeRunSequences.get(runRef);
      if (sequence !== undefined) hydrateRun(platform, runRef, sequence);
      return Promise.resolve();
    });
    vi.stubGlobal("WebSocket", FakeWebSocket);
    vi.stubGlobal("navigator", { onLine: true });
    vi.stubGlobal("crypto", {
      randomUUID: vi
        .fn()
        .mockReturnValueOnce("00000000-0000-4000-8000-000000000001")
        .mockReturnValueOnce("00000000-0000-4000-8000-000000000002")
        .mockReturnValueOnce("00000000-0000-4000-8000-000000000003")
        .mockReturnValue("00000000-0000-4000-8000-000000000004"),
    });
    vi.stubGlobal("window", {
      setTimeout: (callback: () => void) => {
        scheduled.push(callback);
        return scheduled.length;
      },
      clearTimeout: vi.fn(),
      addEventListener: (type: string, listener: () => void) => {
        windowEvents.set(type, listener);
      },
      removeEventListener: (type: string) => {
        windowEvents.delete(type);
      },
    });
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("открывает один session socket и возобновляет platform с двумя run", () => {
    const store = useRealtimeStore();
    store.openPlatform();
    store.openRun("run_realtime01");
    store.openRun("run_realtime02");

    expect(FakeWebSocket.instances).toHaveLength(1);
    const socket = socketAt(0);
    expect(socket.url).toBe(
      "wss://kodex.example/api/v1/session/stream?locale=ru",
    );
    expect(socket.protocols).toEqual([
      "kodex.session.v1",
      "csrf.csrf-test-value-with-sufficient-length-000000000000",
    ]);

    socket.open();
    expect(resumeRequest(socket)).toEqual({
      type: "SESSION_RESUME",
      requestRef: "00000000000040008000000000000001",
      platformAfterSequence: 0,
      runs: [
        { runRef: "run_realtime01", afterSequence: 0 },
        { runRef: "run_realtime02", afterSequence: 0 },
      ],
    });
    store.closeAll();
  });

  it("подписывает и отписывает run на том же socket", () => {
    const store = useRealtimeStore();
    store.openPlatform();
    const socket = socketAt(0);
    socket.open();

    store.openRun("run_realtime01");
    expect(FakeWebSocket.instances).toHaveLength(1);
    expect(sent(socket, 1)).toEqual({
      type: "SUBSCRIBE_RUN",
      requestRef: "00000000000040008000000000000002",
      runRef: "run_realtime01",
      afterSequence: 0,
    });

    store.closeRun("run_realtime01");
    expect(sent(socket, 2)).toEqual({
      type: "UNSUBSCRIBE_RUN",
      requestRef: "00000000000040008000000000000003",
      runRef: "run_realtime01",
    });
    expect(socket.readyState).toBe(FakeWebSocket.OPEN);
    store.closeAll();
  });

  it("не смешивает dynamic subscribe с immutable initial resume", async () => {
    const store = useRealtimeStore();
    store.openRun("run_realtime01");
    const socket = socketAt(0);
    socket.open();
    store.openRun("run_realtime02");
    socket.message(runSnapshot(socket, "run_realtime01"));
    socket.message({
      type: "SESSION_READY",
      requestRef: requestRef(socket),
      streams: [
        { streamKind: "PLATFORM", streamRef: "PLATFORM", cursor: 0 },
        { streamKind: "RUN", streamRef: "run_realtime01", cursor: 1 },
      ],
    });
    await flushProcessing();

    expect(socket.readyState).toBe(FakeWebSocket.OPEN);
    expect(sent(socket, 1)).toMatchObject({
      type: "SUBSCRIBE_RUN",
      runRef: "run_realtime02",
    });
    store.closeAll();
  });

  it("восстанавливает platform cursor и все активные run cursors", async () => {
    const platform = usePlatformStore();
    vi.spyOn(platform, "reloadPlatformKind").mockResolvedValue(undefined);
    const store = useRealtimeStore();
    store.openPlatform();
    store.openRun("run_realtime01");
    store.openRun("run_realtime02");
    const first = socketAt(0);
    first.open();

    first.message({
      type: "PLATFORM_INVALIDATED",
      requestRef: requestRef(first),
      streamKind: "PLATFORM",
      streamRef: "PLATFORM",
      cursor: 1,
      eventName: "RUN_CHANGED",
      kind: "RUN",
    });
    first.message(runSnapshot(first, "run_realtime01", 2));
    first.message(runSnapshot(first, "run_realtime02", 4));
    await flushProcessing();

    first.close(1006, "CONNECTION_LOST");
    expect(scheduled).toHaveLength(1);
    scheduled.shift()?.();
    const second = socketAt(1);
    second.open();
    expect(resumeRequest(second)).toMatchObject({
      type: "SESSION_RESUME",
      platformAfterSequence: 1,
      runs: [
        { runRef: "run_realtime01", afterSequence: 2 },
        { runRef: "run_realtime02", afterSequence: 4 },
      ],
    });
    store.refreshSession();
    const renewed = socketAt(2);
    renewed.open();
    expect(resumeRequest(renewed)).toMatchObject({
      type: "SESSION_RESUME",
      platformAfterSequence: 1,
      runs: [
        { runRef: "run_realtime01", afterSequence: 2 },
        { runRef: "run_realtime02", afterSequence: 4 },
      ],
    });
    store.closeAll();
  });

  it("восстанавливает gap одного run без разрыва остальных потоков", async () => {
    const store = useRealtimeStore();
    store.openPlatform();
    store.openRun("run_realtime01");
    store.openRun("run_realtime02");
    const socket = socketAt(0);
    socket.open();
    socket.message(runSnapshot(socket, "run_realtime01"));
    socket.message(runSnapshot(socket, "run_realtime02"));
    await flushProcessing();
    socket.message(
      runEnvelope(socket, "run_realtime01", {
        type: "RUN_EVENT",
        cursor: 3,
        event: event("run_realtime01", 3),
      }),
    );
    await flushProcessing();

    expect(socket.readyState).toBe(FakeWebSocket.OPEN);
    expect(store.state.run_realtime01).toMatchObject({
      state: "recovering",
      attempt: 1,
    });
    expect(store.state.run_realtime02?.state).not.toBe("offline");
    expect(scheduled).toHaveLength(1);

    scheduled.shift()?.();
    expect(sent(socket, 1)).toMatchObject({
      type: "SUBSCRIBE_RUN",
      runRef: "run_realtime01",
      afterSequence: 1,
    });
    expect(FakeWebSocket.instances).toHaveLength(1);
    store.closeAll();
  });

  it("не принимает snapshot без непрерывной авторитетной истории", async () => {
    const store = useRealtimeStore();
    store.openRun("run_realtime01");
    const socket = socketAt(0);
    socket.open();

    socket.message(
      runEnvelope(socket, "run_realtime01", {
        type: "RUN_GRAPH_SNAPSHOT",
        cursor: 2,
        snapshot: graph("run_realtime01", 2),
      }),
    );
    await flushProcessing();

    expect(socket.readyState).toBe(FakeWebSocket.OPEN);
    expect(store.state.run_realtime01).toMatchObject({
      state: "recovering",
      attempt: 1,
    });
    expect(scheduled).toHaveLength(1);
    store.closeAll();
  });

  it("принимает готовность точного набора потоков и отклоняет подмену", async () => {
    const store = useRealtimeStore();
    store.openRun("run_realtime01");
    const socket = socketAt(0);
    socket.open();
    socket.message(runSnapshot(socket, "run_realtime01"));
    await flushProcessing();
    socket.message({
      type: "SESSION_READY",
      requestRef: requestRef(socket),
      streams: [
        { streamKind: "PLATFORM", streamRef: "PLATFORM", cursor: 0 },
        { streamKind: "RUN", streamRef: "run_foreign01", cursor: 1 },
      ],
    });
    await flushProcessing();

    expect(socket.closeCode).toBe(1002);
    expect(socket.closeReason).toBe("INVALID_SESSION_READY");
    store.closeAll();
  });
});
