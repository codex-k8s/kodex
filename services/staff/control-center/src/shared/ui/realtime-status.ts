export type RealtimeStatusState =
  | "initial-loading"
  | "live"
  | "background-refresh"
  | "reconnecting"
  | "offline";

export type RealtimeConnectionState =
  | "CONNECTING"
  | "CONNECTED"
  | "RECOVERING"
  | "DISCONNECTED";

export type RealtimeStatusLabels = Record<RealtimeStatusState, string>;

export type RealtimeStatusTone =
  | "neutral"
  | "success"
  | "accent"
  | "warning"
  | "danger";

export interface RealtimeStatusPresentation {
  tone: RealtimeStatusTone;
  animated: boolean;
  preservesCurrentData: boolean;
}

const presentationByState: Record<
  RealtimeStatusState,
  RealtimeStatusPresentation
> = {
  "initial-loading": {
    tone: "neutral",
    animated: true,
    preservesCurrentData: false,
  },
  live: {
    tone: "success",
    animated: false,
    preservesCurrentData: true,
  },
  "background-refresh": {
    tone: "accent",
    animated: true,
    preservesCurrentData: true,
  },
  reconnecting: {
    tone: "warning",
    animated: true,
    preservesCurrentData: true,
  },
  offline: {
    tone: "danger",
    animated: false,
    preservesCurrentData: true,
  },
};

const connectionStateByPresentation: Record<
  RealtimeStatusState,
  RealtimeConnectionState
> = {
  "initial-loading": "CONNECTING",
  live: "CONNECTED",
  "background-refresh": "RECOVERING",
  reconnecting: "RECOVERING",
  offline: "DISCONNECTED",
};

export function realtimeStatusPresentation(
  state: RealtimeStatusState,
): RealtimeStatusPresentation {
  return presentationByState[state];
}

export function realtimeConnectionState(
  state: RealtimeStatusState,
): RealtimeConnectionState {
  return connectionStateByPresentation[state];
}
