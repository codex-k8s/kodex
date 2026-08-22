import { defineStore } from "pinia";
import { reactive } from "vue";

import type {
  RunEvent,
  RunGraph,
} from "@/shared/api/generated/openapi/types.gen";
import { csrfToken } from "@/shared/api/mutation";
import { runtimeConfig } from "@/shared/config/runtime";
import { usePlatformStore } from "@/features/platform/store";

export type StreamState = "connecting" | "live" | "offline" | "recovering";

interface ConnectionState {
  state: StreamState;
  attempt: number;
  lastHeartbeat?: string;
  problemCode?: string;
}

interface ActiveStream {
  socket?: WebSocket;
  stopped: boolean;
  timer?: number;
}

interface SnapshotWire {
  type: "GRAPH_SNAPSHOT";
  runRef: string;
  sequence: number;
  snapshot: RunGraph;
}

interface EventWire {
  type: "RUN_EVENT";
  runRef: string;
  sequence: number;
  event: RunEvent;
}

interface ResyncWire {
  type: "RESYNC_REQUIRED";
  runRef: string;
  reason: string;
}

interface ProblemWire {
  type: "PROBLEM";
  code: string;
  retryable: boolean;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function isSnapshotWire(
  value: Record<string, unknown>,
  runRef: string,
): value is Record<string, unknown> & SnapshotWire {
  if (
    value.type !== "GRAPH_SNAPSHOT" ||
    value.runRef !== runRef ||
    !Number.isSafeInteger(value.sequence) ||
    !isRecord(value.snapshot)
  )
    return false;
  const snapshot = value.snapshot;
  return (
    snapshot.runRef === runRef &&
    Array.isArray(snapshot.nodes) &&
    Array.isArray(snapshot.edges)
  );
}

function isEventWire(
  value: Record<string, unknown>,
  runRef: string,
): value is Record<string, unknown> & EventWire {
  if (
    value.type !== "RUN_EVENT" ||
    value.runRef !== runRef ||
    !Number.isSafeInteger(value.sequence) ||
    !isRecord(value.event)
  )
    return false;
  return (
    value.event.runRef === runRef && value.event.sequence === value.sequence
  );
}

function streamURL(runRef: string): string {
  const url = new URL(runtimeConfig().realtimeUrl);
  url.pathname = `${url.pathname.replace(/\/$/, "")}/runs/${encodeURIComponent(runRef)}/stream`;
  url.protocol = "wss:";
  return url.toString();
}

export const useRealtimeStore = defineStore("realtime", () => {
  const state = reactive<Record<string, ConnectionState>>({});
  const active = new Map<string, ActiveStream>();
  const platform = usePlatformStore();

  function scheduleReconnect(runRef: string, stream: ActiveStream): void {
    if (stream.stopped) return;
    if (stream.timer !== undefined) window.clearTimeout(stream.timer);
    const attempt = (state[runRef]?.attempt ?? 0) + 1;
    state[runRef] = { state: "offline", attempt };
    const delay = Math.min(10_000, 500 * 2 ** Math.min(attempt, 5));
    stream.timer = window.setTimeout(() => connect(runRef, stream), delay);
  }

  function connect(runRef: string, stream: ActiveStream): void {
    if (stream.stopped) return;
    if (!navigator.onLine) {
      scheduleReconnect(runRef, stream);
      return;
    }
    const previousAttempt = state[runRef]?.attempt ?? 0;
    state[runRef] = { state: "connecting", attempt: previousAttempt };
    let socket: WebSocket;
    try {
      socket = new WebSocket(streamURL(runRef), [
        "mattercodex.run.v1",
        `csrf.${csrfToken()}`,
      ]);
    } catch {
      scheduleReconnect(runRef, stream);
      return;
    }
    stream.socket = socket;
    socket.addEventListener("open", () => {
      const afterSequence = platform.graphs[runRef]?.sequence ?? 0;
      socket.send(
        JSON.stringify({
          type: "RESUME",
          requestRef: crypto.randomUUID().replaceAll("-", ""),
          afterSequence,
        }),
      );
      state[runRef] = { state: "recovering", attempt: previousAttempt };
    });
    socket.addEventListener("message", (message) => {
      if (typeof message.data !== "string" || message.data.length > 65_536)
        return;
      let envelope: unknown;
      try {
        envelope = JSON.parse(message.data);
      } catch {
        socket.close(1002, "INVALID_JSON");
        return;
      }
      if (!isRecord(envelope) || typeof envelope.type !== "string") return;
      const type = envelope.type;
      if (type === "GRAPH_SNAPSHOT" && isSnapshotWire(envelope, runRef)) {
        platform.applyRunSnapshot(envelope.snapshot);
        state[runRef] = { state: "live", attempt: 0 };
      } else if (type === "RUN_EVENT" && isEventWire(envelope, runRef)) {
        const outcome = platform.applyRunEvent(envelope.event);
        if (outcome === "gap") {
          state[runRef] = { state: "recovering", attempt: previousAttempt };
          socket.close(1012, "GAP_DETECTED");
        } else if (
          outcome === "applied" &&
          [
            "NODE_ADDED",
            "EDGE_ADDED",
            "ARTIFACT_AVAILABLE",
            "OWNER_GATE_OPENED",
          ].includes(envelope.event.type)
        ) {
          void platform.loadRun(runRef);
        }
      } else if (
        type === "RESYNC_REQUIRED" &&
        envelope.runRef === runRef &&
        typeof envelope.reason === "string"
      ) {
        const resync = envelope as unknown as ResyncWire;
        state[runRef] = {
          state: "recovering",
          attempt: previousAttempt,
          problemCode: resync.reason,
        };
        socket.close(1012, "RESYNC_REQUIRED");
      } else if (type === "HEARTBEAT" && "serverTime" in envelope) {
        state[runRef] = {
          state: "live",
          attempt: 0,
          lastHeartbeat: String(envelope.serverTime),
        };
      } else if (
        type === "PROBLEM" &&
        typeof envelope.code === "string" &&
        typeof envelope.retryable === "boolean"
      ) {
        const problem = envelope as unknown as ProblemWire;
        state[runRef] = {
          state: problem.retryable ? "recovering" : "offline",
          attempt: previousAttempt,
          problemCode: problem.code,
        };
      }
    });
    socket.addEventListener("close", () => scheduleReconnect(runRef, stream));
    socket.addEventListener("error", () => socket.close());
  }

  function openRun(runRef: string): void {
    closeRun(runRef);
    const stream: ActiveStream = { stopped: false };
    active.set(runRef, stream);
    connect(runRef, stream);
  }

  function closeRun(runRef: string): void {
    const stream = active.get(runRef);
    if (!stream) return;
    stream.stopped = true;
    if (stream.timer !== undefined) window.clearTimeout(stream.timer);
    stream.socket?.close(1000, "VIEW_CLOSED");
    active.delete(runRef);
    Reflect.deleteProperty(state, runRef);
  }

  function closeAll(): void {
    for (const runRef of [...active.keys()]) closeRun(runRef);
  }

  window.addEventListener("online", () => {
    for (const [runRef, stream] of active) {
      stream.socket?.close();
      connect(runRef, stream);
    }
  });

  return { state, openRun, closeRun, closeAll };
});
