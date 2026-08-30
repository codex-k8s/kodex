import type { RealtimeStatusState } from "@/shared/ui/realtime-status";

export type ShellRealtimeStreamState =
  | "connecting"
  | "live"
  | "offline"
  | "recovering";

export interface ShellRealtimeStateInput {
  online: boolean;
  started: boolean;
  streamState: ShellRealtimeStreamState;
}

export function resolveShellRealtimeState(
  input: ShellRealtimeStateInput,
): RealtimeStatusState {
  if (!input.online) return "offline";
  if (!input.started || input.streamState === "connecting")
    return "initial-loading";
  if (input.streamState === "live") return "live";
  if (input.streamState === "recovering") return "reconnecting";
  return "offline";
}
