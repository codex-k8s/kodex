import { defineStore } from "pinia";
import { onScopeDispose, reactive, ref } from "vue";

import type {
  RunEvent,
  RunGraph,
} from "@/shared/api/generated/openapi/types.gen";
import { csrfToken } from "@/shared/api/mutation";
import { runtimeConfig } from "@/shared/config/runtime";
import { usePlatformStore } from "@/features/platform/store";
import { currentLocale } from "@/shared/locale";

export type StreamState = "connecting" | "live" | "offline" | "recovering";

interface ConnectionState {
  state: StreamState;
  attempt: number;
  lastHeartbeat?: string;
  problemCode?: string;
  problemTitle?: string;
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

interface ReadyWire {
  type: "STREAM_READY";
  runRef: string;
  latestSequence: number;
}

interface ProblemWire {
  type: "PROBLEM";
  code: string;
  title: string;
  retryable: boolean;
}

type PlatformKind =
  | "PROJECT"
  | "AGENT"
  | "ARTIFACT"
  | "INSTRUCTIONS"
  | "WORKFLOW"
  | "SCHEDULE"
  | "INTEGRATION_CONNECTION"
  | "INTEGRATION_GRANT"
  | "MEMBERSHIP"
  | "PLATFORM_MEMBERSHIP"
  | "SYSTEM_ASSISTANT"
  | "ROLE_IMAGE_RECIPE";

interface PlatformInvalidatedWire {
  type: "PLATFORM_INVALIDATED";
  sequence: number;
  eventName: string;
  kind: PlatformKind;
}

const platformKinds = new Set<PlatformKind>([
  "PROJECT",
  "AGENT",
  "ARTIFACT",
  "INSTRUCTIONS",
  "WORKFLOW",
  "SCHEDULE",
  "INTEGRATION_CONNECTION",
  "INTEGRATION_GRANT",
  "MEMBERSHIP",
  "PLATFORM_MEMBERSHIP",
  "SYSTEM_ASSISTANT",
  "ROLE_IMAGE_RECIPE",
]);

// Browsers only permit applications to send close code 1000 or 3000-4999.
const clientReconnectCloseCode = 4000;

export type PlatformSequenceOutcome =
  | "applied"
  | "duplicate"
  | "gap"
  | "invalid";

export function reducePlatformSequence(
  current: number,
  incoming: number,
): PlatformSequenceOutcome {
  if (
    !Number.isSafeInteger(current) ||
    current < 0 ||
    !Number.isSafeInteger(incoming) ||
    incoming < 1
  )
    return "invalid";
  if (incoming <= current) return "duplicate";
  if (incoming !== current + 1) return "gap";
  return "applied";
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function isActiveSocket(stream: ActiveStream, socket: WebSocket): boolean {
  return stream.socket === socket && !stream.stopped;
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
  url.searchParams.set("locale", currentLocale());
  return url.toString();
}

function platformStreamURL(): string {
  const url = new URL(runtimeConfig().realtimeUrl);
  url.pathname = `${url.pathname.replace(/\/$/, "")}/platform/stream`;
  url.protocol = "wss:";
  url.searchParams.set("locale", currentLocale());
  return url.toString();
}

export const useRealtimeStore = defineStore("realtime", () => {
  const state = reactive<Record<string, ConnectionState>>({});
  const platformState = reactive<ConnectionState>({
    state: "offline",
    attempt: 0,
  });
  const platformSequence = ref(0);
  const active = new Map<string, ActiveStream>();
  let activePlatform: ActiveStream | undefined;
  const platform = usePlatformStore();

  function scheduleReconnect(runRef: string, stream: ActiveStream): void {
    if (stream.stopped) return;
    if (stream.timer !== undefined) window.clearTimeout(stream.timer);
    const previous = state[runRef];
    const attempt = (previous?.attempt ?? 0) + 1;
    state[runRef] = {
      state: "offline",
      attempt,
      problemCode: previous?.problemCode,
      problemTitle: previous?.problemTitle,
    };
    const delay = Math.min(10_000, 500 * 2 ** Math.min(attempt, 5));
    stream.timer = window.setTimeout(() => connect(runRef, stream), delay);
  }

  function connect(runRef: string, stream: ActiveStream): void {
    if (stream.stopped) return;
    if (
      stream.socket &&
      (stream.socket.readyState === WebSocket.CONNECTING ||
        stream.socket.readyState === WebSocket.OPEN)
    )
      return;
    if (!navigator.onLine) {
      scheduleReconnect(runRef, stream);
      return;
    }
    const previousAttempt = state[runRef]?.attempt ?? 0;
    state[runRef] = { state: "connecting", attempt: previousAttempt };
    let socket: WebSocket;
    try {
      socket = new WebSocket(streamURL(runRef), [
        "kodex.run.v1",
        `csrf.${csrfToken()}`,
      ]);
    } catch {
      scheduleReconnect(runRef, stream);
      return;
    }
    stream.socket = socket;
    socket.addEventListener("open", () => {
      if (stream.socket !== socket || stream.stopped) return;
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
      if (stream.socket !== socket || stream.stopped) return;
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
        state[runRef] = { state: "recovering", attempt: previousAttempt };
      } else if (type === "RUN_EVENT" && isEventWire(envelope, runRef)) {
        const outcome = platform.applyRunEvent(envelope.event);
        if (outcome === "gap" || outcome === "invalid") {
          state[runRef] = { state: "recovering", attempt: previousAttempt };
          socket.close(
            clientReconnectCloseCode,
            outcome === "gap" ? "GAP_DETECTED" : "INVALID_DELTA",
          );
        }
      } else if (
        type === "STREAM_READY" &&
        envelope.runRef === runRef &&
        Number.isSafeInteger(envelope.latestSequence)
      ) {
        const ready = envelope as unknown as ReadyWire;
        if ((platform.graphs[runRef]?.sequence ?? 0) !== ready.latestSequence) {
          state[runRef] = { state: "recovering", attempt: previousAttempt };
          socket.close(clientReconnectCloseCode, "READY_SEQUENCE_MISMATCH");
          return;
        }
        state[runRef] = { state: "live", attempt: 0 };
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
      } else if (type === "HEARTBEAT" && "serverTime" in envelope) {
        state[runRef] = {
          state: "live",
          attempt: 0,
          lastHeartbeat: String(envelope.serverTime),
        };
      } else if (
        type === "PROBLEM" &&
        typeof envelope.code === "string" &&
        typeof envelope.title === "string" &&
        typeof envelope.retryable === "boolean"
      ) {
        const problem = envelope as unknown as ProblemWire;
        state[runRef] = {
          state: problem.retryable ? "recovering" : "offline",
          attempt: previousAttempt,
          problemCode: problem.code,
          problemTitle: problem.title,
        };
      }
    });
    socket.addEventListener("close", () => {
      if (stream.socket !== socket) return;
      stream.socket = undefined;
      scheduleReconnect(runRef, stream);
    });
    socket.addEventListener("error", () => {
      if (stream.socket === socket) socket.close();
    });
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

  function schedulePlatformReconnect(stream: ActiveStream): void {
    if (stream.stopped) return;
    if (stream.timer !== undefined) window.clearTimeout(stream.timer);
    const attempt = platformState.attempt + 1;
    Object.assign(platformState, { state: "offline", attempt });
    const delay = Math.min(10_000, 500 * 2 ** Math.min(attempt, 5));
    stream.timer = window.setTimeout(() => connectPlatform(stream), delay);
  }

  function connectPlatform(stream: ActiveStream): void {
    if (stream.stopped) return;
    if (
      stream.socket &&
      (stream.socket.readyState === WebSocket.CONNECTING ||
        stream.socket.readyState === WebSocket.OPEN)
    )
      return;
    if (!navigator.onLine) {
      schedulePlatformReconnect(stream);
      return;
    }
    const previousAttempt = platformState.attempt;
    Object.assign(platformState, {
      state: "connecting",
      attempt: previousAttempt,
      problemCode: undefined,
      problemTitle: undefined,
    });
    let socket: WebSocket;
    try {
      socket = new WebSocket(platformStreamURL(), [
        "kodex.platform.v1",
        `csrf.${csrfToken()}`,
      ]);
    } catch {
      schedulePlatformReconnect(stream);
      return;
    }
    stream.socket = socket;
    let processing = Promise.resolve();
    socket.addEventListener("open", () => {
      if (stream.socket !== socket || stream.stopped) return;
      socket.send(
        JSON.stringify({
          type: "RESUME",
          requestRef: crypto.randomUUID().replaceAll("-", ""),
          afterSequence: platformSequence.value,
        }),
      );
      Object.assign(platformState, {
        state: "recovering",
        attempt: previousAttempt,
      });
    });
    socket.addEventListener("message", (message) => {
      if (stream.socket !== socket || stream.stopped) return;
      if (typeof message.data !== "string" || message.data.length > 65_536)
        return;
      let envelope: unknown;
      try {
        envelope = JSON.parse(message.data);
      } catch {
        socket.close(1002, "INVALID_JSON");
        return;
      }
      processing = processing
        .then(async () => {
          if (
            stream.socket !== socket ||
            stream.stopped ||
            !isRecord(envelope) ||
            typeof envelope.type !== "string"
          )
            return;
          if (envelope.type === "PLATFORM_RESYNC_REQUIRED") {
            if (
              !Number.isSafeInteger(envelope.currentSequence) ||
              Number(envelope.currentSequence) < 0 ||
              envelope.reason !== "AUTHORITATIVE_READ_REQUIRED"
            ) {
              socket.close(1002, "INVALID_RESYNC");
              return;
            }
            Object.assign(platformState, {
              state: "recovering",
              attempt: previousAttempt,
            });
            await platform.reloadPlatformState();
            if (isActiveSocket(stream, socket))
              platformSequence.value = Number(envelope.currentSequence);
            return;
          }
          if (envelope.type === "PLATFORM_INVALIDATED") {
            if (
              !Number.isSafeInteger(envelope.sequence) ||
              Number(envelope.sequence) < 1 ||
              typeof envelope.eventName !== "string" ||
              typeof envelope.kind !== "string" ||
              !platformKinds.has(envelope.kind as PlatformKind)
            ) {
              socket.close(1002, "INVALID_PLATFORM_EVENT");
              return;
            }
            const invalidation = envelope as unknown as PlatformInvalidatedWire;
            const outcome = reducePlatformSequence(
              platformSequence.value,
              invalidation.sequence,
            );
            if (outcome === "duplicate") return;
            if (outcome !== "applied") {
              socket.close(clientReconnectCloseCode, "GAP_DETECTED");
              return;
            }
            await platform.reloadPlatformKind(invalidation.kind);
            if (isActiveSocket(stream, socket))
              platformSequence.value = invalidation.sequence;
            return;
          }
          if (envelope.type === "PLATFORM_STREAM_READY") {
            if (
              !Number.isSafeInteger(envelope.latestSequence) ||
              Number(envelope.latestSequence) !== platformSequence.value
            ) {
              socket.close(clientReconnectCloseCode, "READY_SEQUENCE_MISMATCH");
              return;
            }
            Object.assign(platformState, {
              state: "live",
              attempt: 0,
              problemCode: undefined,
              problemTitle: undefined,
            });
            return;
          }
          if (
            envelope.type === "PROBLEM" &&
            typeof envelope.code === "string" &&
            typeof envelope.title === "string" &&
            typeof envelope.retryable === "boolean"
          ) {
            const problem = envelope as unknown as ProblemWire;
            Object.assign(platformState, {
              state: problem.retryable ? "recovering" : "offline",
              attempt: previousAttempt,
              problemCode: problem.code,
              problemTitle: problem.title,
            });
            return;
          }
          if (envelope.type === "PLATFORM_HEARTBEAT") {
            if (
              !Number.isSafeInteger(envelope.latestSequence) ||
              Number(envelope.latestSequence) !== platformSequence.value ||
              typeof envelope.serverTime !== "string"
            ) {
              socket.close(
                clientReconnectCloseCode,
                "HEARTBEAT_SEQUENCE_MISMATCH",
              );
              return;
            }
            Object.assign(platformState, {
              state: "live",
              attempt: 0,
              lastHeartbeat: envelope.serverTime,
              problemCode: undefined,
              problemTitle: undefined,
            });
          }
        })
        .catch(() => {
          if (stream.socket !== socket || stream.stopped) return;
          Object.assign(platformState, {
            state: "recovering",
            attempt: previousAttempt,
            problemCode: "AUTHORITATIVE_RELOAD_FAILED",
          });
          socket.close(clientReconnectCloseCode, "AUTHORITATIVE_RELOAD_FAILED");
        });
    });
    socket.addEventListener("close", () => {
      if (stream.socket !== socket) return;
      stream.socket = undefined;
      schedulePlatformReconnect(stream);
    });
    socket.addEventListener("error", () => {
      if (stream.socket === socket) socket.close();
    });
  }

  function openPlatform(): void {
    closePlatform();
    const stream: ActiveStream = { stopped: false };
    activePlatform = stream;
    connectPlatform(stream);
  }

  function closePlatform(): void {
    const stream = activePlatform;
    if (!stream) return;
    stream.stopped = true;
    if (stream.timer !== undefined) window.clearTimeout(stream.timer);
    stream.socket?.close(1000, "SHELL_CLOSED");
    activePlatform = undefined;
    platformSequence.value = 0;
    Object.assign(platformState, {
      state: "offline",
      attempt: 0,
      lastHeartbeat: undefined,
      problemCode: undefined,
      problemTitle: undefined,
    });
  }

  function closeAll(): void {
    for (const runRef of [...active.keys()]) closeRun(runRef);
    closePlatform();
  }

  function handleOnline(): void {
    for (const [runRef, stream] of active) {
      if (stream.timer !== undefined) {
        window.clearTimeout(stream.timer);
        stream.timer = undefined;
      }
      connect(runRef, stream);
    }
    if (activePlatform) {
      if (activePlatform.timer !== undefined) {
        window.clearTimeout(activePlatform.timer);
        activePlatform.timer = undefined;
      }
      connectPlatform(activePlatform);
    }
  }

  window.addEventListener("online", handleOnline);
  onScopeDispose(() => {
    window.removeEventListener("online", handleOnline);
    closeAll();
  });

  return {
    state,
    platformState,
    platformSequence,
    openRun,
    closeRun,
    openPlatform,
    closePlatform,
    closeAll,
  };
});
