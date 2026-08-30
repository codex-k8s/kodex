export type RealtimeStatusState =
  | "initial-loading"
  | "live"
  | "background-refresh"
  | "reconnecting"
  | "offline";

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

export function realtimeStatusPresentation(
  state: RealtimeStatusState,
): RealtimeStatusPresentation {
  return presentationByState[state];
}
