import { createPinia, setActivePinia } from "pinia";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type {
  RunEvent,
  RunGraph,
} from "@/shared/api/generated/openapi/types.gen";

vi.mock("@/shared/config/runtime", () => ({
  runtimeConfig: () => ({
    realtimeUrl: "wss://kodex.example/api/v1",
  }),
}));
vi.mock("@/shared/api/mutation", () => ({
  csrfToken: () => "csrf-test-value-with-sufficient-length-000000000000",
}));
vi.mock("@/shared/locale", () => ({
  currentLocale: () => "ru",
}));

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
    const values = this.listeners.get(type) ?? [];
    values.push(listener);
    this.listeners.set(type, values);
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

const reconnectCallbacks: Array<() => void> = [];
const windowEvents = new Map<string, () => void>();

function graph(sequence = 1): RunGraph {
  return {
    runRef: "run_realtime01",
    revision: sequence,
    sequence,
    nodes: [],
    edges: [],
  };
}

function gapEvent(): RunEvent {
  return {
    ref: "event_gap0003",
    runRef: "run_realtime01",
    sequence: 3,
    graphRevision: 3,
    type: "RUN_STATE_CHANGED",
    summary: "Выполнение продолжается",
    occurredAt: "2026-08-23T00:00:03Z",
    run: {
      ref: "run_realtime01",
      version: 3,
      state: "RUNNING",
      graphRevision: 3,
      lastEventSequence: 3,
      artifactRefs: [],
      gateRefs: [],
      nextActions: ["OPEN", "CANCEL"],
    },
  };
}

describe("realtime store", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    FakeWebSocket.instances = [];
    reconnectCallbacks.length = 0;
    windowEvents.clear();
    vi.stubGlobal("WebSocket", FakeWebSocket);
    vi.stubGlobal("navigator", { onLine: true });
    vi.stubGlobal("window", {
      setTimeout: (callback: () => void) => {
        reconnectCallbacks.push(callback);
        return reconnectCallbacks.length;
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

  it("отправляет resume с authoritative sequence и становится live", () => {
    const store = useRealtimeStore();
    store.openRun("run_realtime01");
    const socket = FakeWebSocket.instances[0];
    expect(socket).toBeDefined();
    expect(socket?.url).toContain("locale=ru");

    socket?.open();
    expect(JSON.parse(socket?.sent[0] ?? "{}")).toMatchObject({
      type: "RESUME",
      afterSequence: 0,
    });
    socket?.message({
      type: "GRAPH_SNAPSHOT",
      runRef: "run_realtime01",
      sequence: 1,
      snapshot: graph(),
    });
    socket?.message({
      type: "STREAM_READY",
      runRef: "run_realtime01",
      latestSequence: 1,
    });

    expect(store.state.run_realtime01?.state).toBe("live");
    store.closeAll();
  });

  it("закрывает stream при gap и планирует bounded reconnect", () => {
    const store = useRealtimeStore();
    store.openRun("run_realtime01");
    const first = FakeWebSocket.instances[0];
    first?.open();
    first?.message({
      type: "GRAPH_SNAPSHOT",
      runRef: "run_realtime01",
      sequence: 1,
      snapshot: graph(),
    });

    first?.message({
      type: "RUN_EVENT",
      runRef: "run_realtime01",
      sequence: 3,
      event: gapEvent(),
    });

    expect(first?.closeCode).toBe(1012);
    expect(first?.closeReason).toBe("GAP_DETECTED");
    expect(store.state.run_realtime01).toMatchObject({
      state: "offline",
      attempt: 1,
    });
    expect(reconnectCallbacks).toHaveLength(1);

    reconnectCallbacks[0]?.();
    expect(FakeWebSocket.instances).toHaveLength(2);
    expect(store.state.run_realtime01).toMatchObject({
      state: "connecting",
      attempt: 1,
    });
    store.closeAll();
  });

  it("сохраняет локализованную ошибку platform stream при reconnect", async () => {
    const store = useRealtimeStore();
    store.openPlatform();
    const socket = FakeWebSocket.instances[0];
    expect(socket?.url).toContain("locale=ru");
    socket?.open();
    socket?.message({
      type: "PROBLEM",
      status: 503,
      code: "PLATFORM_UNAVAILABLE",
      title: "Обновления платформы временно недоступны",
      retryable: true,
    });
    await Promise.resolve();
    await Promise.resolve();
    socket?.close(1013, "PLATFORM_UNAVAILABLE");

    expect(store.platformState).toMatchObject({
      state: "offline",
      attempt: 1,
      problemCode: "PLATFORM_UNAVAILABLE",
      problemTitle: "Обновления платформы временно недоступны",
    });
    store.closeAll();
  });
});
