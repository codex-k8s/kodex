import type { OwnerSessionMetadata } from "../src/shared/api/generated/openapi/types.gen";

const invalidMetadata = "Session proof metadata is invalid";

export function sessionTiming(
  value: unknown,
): Pick<
  OwnerSessionMetadata,
  | "version"
  | "serverTime"
  | "renewAfter"
  | "absoluteExpiresAt"
  | "expiresAt"
  | "accessExpiresAt"
  | "renewalMode"
> {
  if (!value || typeof value !== "object") throw new Error(invalidMetadata);
  const data = value as Record<string, unknown>;
  const fields = [
    "serverTime",
    "renewAfter",
    "absoluteExpiresAt",
    "expiresAt",
    "accessExpiresAt",
  ] as const;
  for (const field of fields) {
    if (
      typeof data[field] !== "string" ||
      !Number.isFinite(Date.parse(data[field]))
    )
      throw new Error(invalidMetadata);
  }
  if (
    !Number.isSafeInteger(data.version) ||
    Number(data.version) < 1 ||
    data.renewalMode !== "BACKEND_REFRESH"
  )
    throw new Error(invalidMetadata);
  return Object.fromEntries(
    [...fields, "version", "renewalMode"].map((key) => [key, data[key]]),
  ) as ReturnType<typeof sessionTiming>;
}

export function renewalWindow(
  value: unknown,
  budgetMs: number,
): { waitMs: number; absoluteExpiresAt: string; version: number } {
  const metadata = sessionTiming(value);
  const now = Date.parse(metadata.serverTime);
  const waitMs = Date.parse(metadata.renewAfter) - now;
  if (
    waitMs < 15_000 ||
    waitMs + 60_000 > budgetMs ||
    Math.min(
      Date.parse(metadata.absoluteExpiresAt),
      Date.parse(metadata.expiresAt),
      Date.parse(metadata.accessExpiresAt),
    ) <=
      now + waitMs + 45_000
  ) {
    throw new Error(
      "Session proof requires a fresh session with a natural renewal inside its budget",
    );
  }
  return {
    waitMs,
    absoluteExpiresAt: metadata.absoluteExpiresAt,
    version: metadata.version,
  };
}

export interface SocketProof {
  ready: number;
  closed: boolean;
  problems: number;
  malformed: number;
  resumeCursors: number[];
  readyCursors: number[];
  businessEvents: number;
  maximumPlatformCursor: number;
}

export function socketProof(): SocketProof {
  return {
    ready: 0,
    closed: false,
    problems: 0,
    malformed: 0,
    resumeCursors: [],
    readyCursors: [],
    businessEvents: 0,
    maximumPlatformCursor: 0,
  };
}

// Содержимое фрейма не покидает обработчик: только закрытые счётчики и cursor.
export function observeFrame(
  proof: SocketProof,
  payload: string | Buffer,
  direction: "sent" | "received",
): void {
  try {
    if (Buffer.byteLength(payload) > 65_536) throw new Error("Invalid frame");
    const parsed: unknown = JSON.parse(payload.toString());
    if (!parsed || typeof parsed !== "object" || Array.isArray(parsed))
      throw new Error("Invalid frame");
    const frame = parsed as Record<string, unknown>;
    if (direction === "sent" && frame.type === "SESSION_RESUME") {
      if (
        !Number.isSafeInteger(frame.platformAfterSequence) ||
        Number(frame.platformAfterSequence) < 0
      )
        throw new Error("Invalid cursor");
      proof.resumeCursors.push(Number(frame.platformAfterSequence));
    }
    if (direction !== "received") return;
    if (frame.type === "SESSION_PROBLEM" || frame.type === "STREAM_PROBLEM")
      proof.problems++;
    if (frame.type === "PLATFORM_INVALIDATED" || frame.type === "RUN_EVENT")
      proof.businessEvents++;
    if (frame.type === "PLATFORM_INVALIDATED") {
      if (!Number.isSafeInteger(frame.cursor) || Number(frame.cursor) < 0)
        throw new Error("Invalid cursor");
      proof.maximumPlatformCursor = Math.max(
        proof.maximumPlatformCursor,
        Number(frame.cursor),
      );
    }
    if (frame.type === "SESSION_READY") {
      if (!Array.isArray(frame.streams)) throw new Error("Invalid streams");
      const streams: unknown[] = frame.streams;
      const platform = streams.filter(
        (stream): stream is Record<string, unknown> =>
          stream !== null &&
          typeof stream === "object" &&
          "streamKind" in stream &&
          stream.streamKind === "PLATFORM" &&
          "streamRef" in stream &&
          stream.streamRef === "PLATFORM",
      );
      if (
        platform.length !== 1 ||
        !Number.isSafeInteger(platform[0]?.cursor) ||
        Number(platform[0]?.cursor) < 0
      )
        throw new Error("Invalid cursor");
      proof.ready++;
      proof.readyCursors.push(Number(platform[0]?.cursor));
      proof.maximumPlatformCursor = Math.max(
        proof.maximumPlatformCursor,
        Number(platform[0]?.cursor),
      );
    }
  } catch {
    proof.malformed++;
  }
}

export function resumedAfterRenewal(sockets: SocketProof[]): boolean {
  const first = sockets[0];
  const last = sockets.at(-1);
  return Boolean(
    first &&
    last &&
    first !== last &&
    first.ready === 1 &&
    first.closed &&
    last.ready === 1 &&
    !last.closed &&
    last.resumeCursors.length === 1 &&
    Number(last.readyCursors[0]) >= Number(last.resumeCursors[0]) &&
    Number(last.resumeCursors[0]) >=
      Math.max(first.maximumPlatformCursor, Number(first.readyCursors[0])) &&
    sockets.every((socket) => socket.problems === 0 && socket.malformed === 0),
  );
}

export function installProtocolObserver(): void {
  const protocols: string[] = [];
  Object.defineProperty(window, "__kodexSessionProofProtocols", {
    value: protocols,
  });
  const NativeWebSocket = window.WebSocket;
  window.WebSocket = new Proxy(NativeWebSocket, {
    construct(target, args: ConstructorParameters<typeof WebSocket>) {
      const socket = new target(...args);
      socket.addEventListener("open", () =>
        protocols.push(socket.protocol === "kodex.session.v2" ? "v2" : "other"),
      );
      return socket;
    },
  });
}
