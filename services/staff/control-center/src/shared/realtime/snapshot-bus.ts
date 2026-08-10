import type { RealtimeSnapshot } from "@/shared/api/adapters/realtime";

type SnapshotChannel = RealtimeSnapshot["channel"];
type ChannelSnapshot<C extends SnapshotChannel> = RealtimeSnapshot & {
  channel: C;
};
type SnapshotListener<C extends SnapshotChannel> = (
  snapshot: ChannelSnapshot<C>,
) => void;

const listeners = new Map<
  SnapshotChannel,
  Set<(value: RealtimeSnapshot) => void>
>();
const disconnectListeners = new Set<() => void>();

/** Typed complete-replace boundary: realtime transport не знает feature stores. */
export function subscribeRealtimeSnapshot<C extends SnapshotChannel>(
  channel: C,
  listener: SnapshotListener<C>,
): () => void {
  const current = listeners.get(channel) ?? new Set();
  const untyped = listener as (value: RealtimeSnapshot) => void;
  current.add(untyped);
  listeners.set(channel, current);
  return () => current.delete(untyped);
}

export function publishRealtimeSnapshot(snapshot: RealtimeSnapshot): void {
  listeners.get(snapshot.channel)?.forEach((listener) => listener(snapshot));
}

export function subscribeRealtimeDisconnect(listener: () => void): () => void {
  disconnectListeners.add(listener);
  return () => disconnectListeners.delete(listener);
}

export function publishRealtimeDisconnect(): void {
  disconnectListeners.forEach((listener) => listener());
}
