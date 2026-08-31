import { defineStore } from "pinia";
import { onScopeDispose, reactive, ref } from "vue";

import type {
  RunEvent,
  RunGraph,
} from "@/shared/api/generated/openapi/types.gen";
import type { PlatformInvalidatedEnvelope } from "@/shared/api/generated/asyncapi/PlatformInvalidatedEnvelope";
import type { StreamKind } from "@/shared/api/generated/asyncapi/StreamKind";
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

interface ActiveRun {
  requestRef?: string;
  timer?: number;
}

interface SessionConnection {
  socket?: WebSocket;
  timer?: number;
  requestRef?: string;
  resumeRunRefs?: Set<string>;
  attempt: number;
  stopped: boolean;
}

type PlatformKind = PlatformInvalidatedEnvelope["kind"];

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
  "RUN",
]);

const platformStreamRef = "PLATFORM";
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

export function hasCompleteRunSnapshot(
  graph: RunGraph | undefined,
  events: Record<number, RunEvent> | undefined,
  cursor: number,
): boolean {
  if (!Number.isSafeInteger(cursor) || cursor < 0) return false;
  if ((graph?.sequence ?? -1) < cursor) return false;
  return cursor === 0 || Boolean(events?.[cursor]);
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function requestRef(): string {
  return crypto.randomUUID().replaceAll("-", "");
}

function sessionStreamURL(): string {
  const url = new URL(runtimeConfig().realtimeUrl);
  url.pathname = `${url.pathname.replace(/\/$/, "")}/session/stream`;
  url.protocol = "wss:";
  url.searchParams.set("locale", currentLocale());
  return url.toString();
}

function hasStreamIdentity(
  value: Record<string, unknown>,
  kind: StreamKind,
  ref: string,
): boolean {
  return (
    value.streamKind === kind &&
    value.streamRef === ref &&
    Number.isSafeInteger(value.cursor) &&
    Number(value.cursor) >= 0
  );
}

export const useRealtimeStore = defineStore("realtime", () => {
  const state = reactive<Record<string, ConnectionState>>({});
  const platformState = reactive<ConnectionState>({
    state: "offline",
    attempt: 0,
  });
  const platformSequence = ref(0);
  const activeRuns = new Map<string, ActiveRun>();
  const session: SessionConnection = { attempt: 0, stopped: false };
  let platformWanted = false;
  const platform = usePlatformStore();

  function hasConsumers(): boolean {
    return platformWanted || activeRuns.size > 0;
  }

  function activeSocket(socket: WebSocket): boolean {
    return session.socket === socket && !session.stopped;
  }

  function markOffline(): void {
    session.attempt += 1;
    if (platformWanted) {
      Object.assign(platformState, {
        state: "offline",
        attempt: session.attempt,
      });
    }
    for (const runRef of activeRuns.keys()) {
      const previous = state[runRef];
      state[runRef] = {
        state: "offline",
        attempt: session.attempt,
        problemCode: previous?.problemCode,
        problemTitle: previous?.problemTitle,
      };
    }
  }

  function scheduleReconnect(): void {
    if (session.stopped || !hasConsumers()) return;
    if (session.timer !== undefined) window.clearTimeout(session.timer);
    markOffline();
    const delay = Math.min(10_000, 500 * 2 ** Math.min(session.attempt, 5));
    session.timer = window.setTimeout(() => {
      session.timer = undefined;
      connect();
    }, delay);
  }

  function send(value: unknown): boolean {
    if (!session.socket || session.socket.readyState !== WebSocket.OPEN)
      return false;
    session.socket.send(JSON.stringify(value));
    return true;
  }

  function subscribeRun(runRef: string): void {
    const active = activeRuns.get(runRef);
    if (!active) return;
    const nextRequestRef = requestRef();
    active.requestRef = nextRequestRef;
    state[runRef] = {
      state: "recovering",
      attempt: state[runRef]?.attempt ?? session.attempt,
    };
    send({
      type: "SUBSCRIBE_RUN",
      requestRef: nextRequestRef,
      runRef,
      afterSequence: platform.graphs[runRef]?.sequence ?? 0,
    });
  }

  function scheduleRunRecovery(runRef: string): void {
    const active = activeRuns.get(runRef);
    if (
      !active ||
      !session.socket ||
      session.socket.readyState !== WebSocket.OPEN
    )
      return;
    if (active.timer !== undefined) window.clearTimeout(active.timer);
    const attempt = (state[runRef]?.attempt ?? 0) + 1;
    state[runRef] = { ...state[runRef], state: "recovering", attempt };
    const delay = Math.min(10_000, 500 * 2 ** Math.min(attempt, 5));
    active.timer = window.setTimeout(() => {
      active.timer = undefined;
      subscribeRun(runRef);
    }, delay);
  }

  function failProtocol(socket: WebSocket, reason: string): void {
    if (activeSocket(socket)) socket.close(1002, reason);
  }

  async function processPlatformEnvelope(
    socket: WebSocket,
    envelope: Record<string, unknown>,
  ): Promise<boolean> {
    if (!hasStreamIdentity(envelope, "PLATFORM", platformStreamRef))
      return false;
    if (envelope.requestRef !== session.requestRef) return true;
    const cursor = Number(envelope.cursor);
    if (envelope.type === "PLATFORM_RESYNC_REQUIRED") {
      if (envelope.reason !== "AUTHORITATIVE_READ_REQUIRED") return false;
      Object.assign(platformState, {
        state: "recovering",
        attempt: session.attempt,
      });
      await platform.reloadPlatformState();
      if (activeSocket(socket)) platformSequence.value = cursor;
      return true;
    }
    if (envelope.type === "PLATFORM_INVALIDATED") {
      if (
        typeof envelope.eventName !== "string" ||
        typeof envelope.kind !== "string" ||
        !platformKinds.has(envelope.kind as PlatformKind)
      )
        return false;
      const outcome = reducePlatformSequence(platformSequence.value, cursor);
      if (outcome === "duplicate") return true;
      if (outcome !== "applied") {
        socket.close(clientReconnectCloseCode, "PLATFORM_GAP_DETECTED");
        return true;
      }
      await platform.reloadPlatformKind(envelope.kind as PlatformKind);
      if (activeSocket(socket)) platformSequence.value = cursor;
      return true;
    }
    if (envelope.type === "PLATFORM_READY") {
      if (cursor !== platformSequence.value) {
        socket.close(
          clientReconnectCloseCode,
          "PLATFORM_READY_CURSOR_MISMATCH",
        );
        return true;
      }
      if (platformWanted) {
        Object.assign(platformState, {
          state: "live",
          attempt: 0,
          problemCode: undefined,
          problemTitle: undefined,
        });
      }
      return true;
    }
    return false;
  }

  async function processRunEnvelope(
    socket: WebSocket,
    envelope: Record<string, unknown>,
  ): Promise<boolean> {
    if (envelope.streamKind !== "RUN" || typeof envelope.streamRef !== "string")
      return false;
    const runRef = envelope.streamRef;
    const active = activeRuns.get(runRef);
    if (!active) return true;
    if (!hasStreamIdentity(envelope, "RUN", runRef)) return false;
    if ("requestRef" in envelope && envelope.requestRef !== active.requestRef)
      return true;
    const cursor = Number(envelope.cursor);
    if (envelope.type === "RUN_GRAPH_SNAPSHOT") {
      if (!isRecord(envelope.snapshot) || envelope.snapshot.runRef !== runRef)
        return false;
      const snapshot = envelope.snapshot as unknown as RunGraph;
      if (
        snapshot.sequence !== cursor ||
        !Array.isArray(snapshot.nodes) ||
        !Array.isArray(snapshot.edges)
      )
        return false;
      state[runRef] = {
        state: "recovering",
        attempt: state[runRef]?.attempt ?? 0,
      };
      await platform.loadRun(runRef);
      if (
        platform.problems.run ||
        !hasCompleteRunSnapshot(
          platform.graphs[runRef],
          platform.events[runRef],
          cursor,
        )
      )
        scheduleRunRecovery(runRef);
      return true;
    }
    if (envelope.type === "RUN_EVENT") {
      if (!isRecord(envelope.event)) return false;
      const event = envelope.event as unknown as RunEvent;
      if (event.runRef !== runRef || event.sequence !== cursor) return false;
      const outcome = platform.applyRunEvent(event);
      if (outcome === "gap") {
        scheduleRunRecovery(runRef);
      } else if (outcome === "invalid") {
        failProtocol(socket, "INVALID_RUN_DELTA");
      }
      return true;
    }
    if (envelope.type === "RUN_READY") {
      if (
        !hasCompleteRunSnapshot(
          platform.graphs[runRef],
          platform.events[runRef],
          cursor,
        )
      ) {
        scheduleRunRecovery(runRef);
        return true;
      }
      state[runRef] = { state: "live", attempt: 0 };
      return true;
    }
    if (envelope.type === "RUN_RESYNC_REQUIRED") {
      if (typeof envelope.reason !== "string") return false;
      state[runRef] = {
        state: "recovering",
        attempt: state[runRef]?.attempt ?? 0,
        problemCode: envelope.reason,
      };
      return true;
    }
    return envelope.type === "RUN_UNSUBSCRIBED";
  }

  function processHeartbeat(envelope: Record<string, unknown>): boolean {
    if (
      envelope.type !== "STREAM_HEARTBEAT" ||
      typeof envelope.serverTime !== "string" ||
      typeof envelope.streamKind !== "string" ||
      typeof envelope.streamRef !== "string" ||
      !Number.isSafeInteger(envelope.cursor) ||
      Number(envelope.cursor) < 0
    )
      return false;
    const cursor = Number(envelope.cursor);
    if (
      envelope.streamKind === "PLATFORM" &&
      envelope.streamRef === platformStreamRef
    ) {
      if (cursor !== platformSequence.value) return false;
      if (platformWanted)
        Object.assign(platformState, {
          state: "live",
          attempt: 0,
          lastHeartbeat: envelope.serverTime,
        });
      return true;
    }
    if (envelope.streamKind === "RUN" && !activeRuns.has(envelope.streamRef))
      return true;
    if (envelope.streamKind === "RUN") {
      if (
        !hasCompleteRunSnapshot(
          platform.graphs[envelope.streamRef],
          platform.events[envelope.streamRef],
          cursor,
        )
      )
        return false;
      state[envelope.streamRef] = {
        state: "live",
        attempt: 0,
        lastHeartbeat: envelope.serverTime,
      };
      return true;
    }
    return false;
  }

  function processStreamProblem(envelope: Record<string, unknown>): boolean {
    if (
      envelope.type !== "STREAM_PROBLEM" ||
      typeof envelope.streamKind !== "string" ||
      typeof envelope.streamRef !== "string" ||
      typeof envelope.code !== "string" ||
      typeof envelope.title !== "string" ||
      typeof envelope.retryable !== "boolean"
    )
      return false;
    if (envelope.streamKind === "RUN") {
      const active = activeRuns.get(envelope.streamRef);
      if (!active || envelope.requestRef !== active.requestRef) return true;
      if (envelope.requestRef === session.requestRef)
        session.resumeRunRefs?.delete(envelope.streamRef);
      state[envelope.streamRef] = {
        state: envelope.retryable ? "recovering" : "offline",
        attempt: state[envelope.streamRef]?.attempt ?? 0,
        problemCode: envelope.code,
        problemTitle: envelope.title,
      };
      if (envelope.retryable) scheduleRunRecovery(envelope.streamRef);
      return true;
    }
    if (
      envelope.streamKind === "PLATFORM" &&
      envelope.streamRef === platformStreamRef &&
      envelope.requestRef === session.requestRef
    ) {
      Object.assign(platformState, {
        state: envelope.retryable ? "recovering" : "offline",
        problemCode: envelope.code,
        problemTitle: envelope.title,
      });
      return true;
    }
    return false;
  }

  async function processEnvelope(
    socket: WebSocket,
    envelope: Record<string, unknown>,
  ): Promise<void> {
    if (envelope.type === "SESSION_PROBLEM") {
      if (
        typeof envelope.code !== "string" ||
        typeof envelope.title !== "string" ||
        typeof envelope.retryable !== "boolean"
      ) {
        failProtocol(socket, "INVALID_SESSION_PROBLEM");
        return;
      }
      Object.assign(platformState, {
        state: envelope.retryable ? "recovering" : "offline",
        problemCode: envelope.code,
        problemTitle: envelope.title,
      });
      for (const runRef of activeRuns.keys()) {
        state[runRef] = {
          state: envelope.retryable ? "recovering" : "offline",
          attempt: state[runRef]?.attempt ?? 0,
          problemCode: envelope.code,
          problemTitle: envelope.title,
        };
      }
      return;
    }
    if (envelope.type === "SESSION_READY") {
      const expectedRefs = new Set<string>([
        platformStreamRef,
        ...(session.resumeRunRefs ?? []),
      ]);
      const streams = Array.isArray(envelope.streams) ? envelope.streams : [];
      const validStreams =
        streams.length === expectedRefs.size &&
        streams.every((stream) => {
          if (!isRecord(stream)) return false;
          const ref = stream.streamRef;
          const kind = stream.streamKind;
          const cursor = stream.cursor;
          if (
            typeof ref !== "string" ||
            !Number.isSafeInteger(cursor) ||
            Number(cursor) < 0 ||
            (kind !== "PLATFORM" && kind !== "RUN")
          )
            return false;
          if (!expectedRefs.delete(ref)) return false;
          if ((kind === "PLATFORM") !== (ref === platformStreamRef))
            return false;
          if (ref === platformStreamRef)
            return cursor === platformSequence.value;
          return (
            !activeRuns.has(ref) ||
            hasCompleteRunSnapshot(
              platform.graphs[ref],
              platform.events[ref],
              Number(cursor),
            )
          );
        }) &&
        expectedRefs.size === 0;
      if (envelope.requestRef !== session.requestRef || !validStreams)
        failProtocol(socket, "INVALID_SESSION_READY");
      else session.resumeRunRefs = undefined;
      return;
    }
    if (await processPlatformEnvelope(socket, envelope)) return;
    if (await processRunEnvelope(socket, envelope)) return;
    if (processHeartbeat(envelope)) return;
    if (processStreamProblem(envelope)) return;
    failProtocol(socket, "INVALID_SESSION_ENVELOPE");
  }

  function connect(): void {
    if (session.stopped || !hasConsumers()) return;
    if (
      session.socket &&
      (session.socket.readyState === WebSocket.CONNECTING ||
        session.socket.readyState === WebSocket.OPEN)
    )
      return;
    if (!navigator.onLine) {
      scheduleReconnect();
      return;
    }
    if (platformWanted)
      Object.assign(platformState, {
        state: "connecting",
        attempt: session.attempt,
      });
    for (const runRef of activeRuns.keys())
      state[runRef] = { state: "connecting", attempt: session.attempt };

    let socket: WebSocket;
    try {
      socket = new WebSocket(sessionStreamURL(), [
        "kodex.session.v1",
        `csrf.${csrfToken()}`,
      ]);
    } catch {
      scheduleReconnect();
      return;
    }
    session.socket = socket;
    let processing = Promise.resolve();
    socket.addEventListener("open", () => {
      if (!activeSocket(socket)) return;
      const sessionRequestRef = requestRef();
      session.requestRef = sessionRequestRef;
      const runs = [...activeRuns.keys()].sort().map((runRef) => {
        const active = activeRuns.get(runRef);
        if (active) active.requestRef = sessionRequestRef;
        state[runRef] = { state: "recovering", attempt: session.attempt };
        return {
          runRef,
          afterSequence: platform.graphs[runRef]?.sequence ?? 0,
        };
      });
      session.resumeRunRefs = new Set(runs.map(({ runRef }) => runRef));
      send({
        type: "SESSION_RESUME",
        requestRef: sessionRequestRef,
        platformAfterSequence: platformSequence.value,
        runs,
      });
      if (platformWanted)
        Object.assign(platformState, {
          state: "recovering",
          attempt: session.attempt,
        });
    });
    socket.addEventListener("message", (message) => {
      if (!activeSocket(socket)) return;
      if (typeof message.data !== "string" || message.data.length > 65_536) {
        failProtocol(socket, "INVALID_SESSION_FRAME");
        return;
      }
      let value: unknown;
      try {
        value = JSON.parse(message.data);
      } catch {
        failProtocol(socket, "INVALID_JSON");
        return;
      }
      if (!isRecord(value) || typeof value.type !== "string") {
        failProtocol(socket, "INVALID_SESSION_ENVELOPE");
        return;
      }
      processing = processing
        .then(() => processEnvelope(socket, value))
        .catch(() => {
          if (activeSocket(socket))
            socket.close(clientReconnectCloseCode, "REALTIME_REDUCER_FAILED");
        });
    });
    socket.addEventListener("close", () => {
      if (session.socket !== socket) return;
      session.socket = undefined;
      session.requestRef = undefined;
      session.resumeRunRefs = undefined;
      if (!session.stopped && hasConsumers()) scheduleReconnect();
    });
    socket.addEventListener("error", () => {
      if (activeSocket(socket)) socket.close();
    });
  }

  function openRun(runRef: string): void {
    if (activeRuns.has(runRef)) return;
    const active: ActiveRun = {};
    activeRuns.set(runRef, active);
    state[runRef] = { state: "connecting", attempt: session.attempt };
    session.stopped = false;
    if (session.socket?.readyState === WebSocket.OPEN) subscribeRun(runRef);
    else connect();
  }

  function closeRun(runRef: string): void {
    const active = activeRuns.get(runRef);
    if (!active) return;
    if (active.timer !== undefined) window.clearTimeout(active.timer);
    if (session.socket?.readyState === WebSocket.OPEN) {
      send({ type: "UNSUBSCRIBE_RUN", requestRef: requestRef(), runRef });
    }
    activeRuns.delete(runRef);
    Reflect.deleteProperty(state, runRef);
    if (!hasConsumers()) disconnect("NO_SUBSCRIPTIONS");
  }

  function openPlatform(): void {
    if (platformWanted) return;
    platformWanted = true;
    session.stopped = false;
    Object.assign(platformState, {
      state: "connecting",
      attempt: session.attempt,
    });
    connect();
  }

  function closePlatform(): void {
    platformWanted = false;
    Object.assign(platformState, {
      state: "offline",
      attempt: 0,
      lastHeartbeat: undefined,
      problemCode: undefined,
      problemTitle: undefined,
    });
    if (!hasConsumers()) {
      platformSequence.value = 0;
      disconnect("NO_SUBSCRIPTIONS");
    }
  }

  function disconnect(reason: string): void {
    if (session.timer !== undefined) window.clearTimeout(session.timer);
    session.timer = undefined;
    session.socket?.close(1000, reason);
    session.socket = undefined;
    session.requestRef = undefined;
    session.resumeRunRefs = undefined;
    session.attempt = 0;
  }

  function closeAll(): void {
    session.stopped = true;
    for (const active of activeRuns.values())
      if (active.timer !== undefined) window.clearTimeout(active.timer);
    activeRuns.clear();
    for (const runRef of Object.keys(state))
      Reflect.deleteProperty(state, runRef);
    platformWanted = false;
    platformSequence.value = 0;
    Object.assign(platformState, {
      state: "offline",
      attempt: 0,
      lastHeartbeat: undefined,
      problemCode: undefined,
      problemTitle: undefined,
    });
    disconnect("STORE_CLOSED");
  }

  function handleOnline(): void {
    if (session.timer !== undefined) window.clearTimeout(session.timer);
    session.timer = undefined;
    connect();
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
