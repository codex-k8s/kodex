import { createRealtimeClient, type RealtimeConnectionState } from "../../shared/ws/realtime-client";
import { parseMissionControlRealtimeEnvelope } from "./lib";
import type { MissionControlRealtimeEvent } from "./types";

export type MissionControlRealtimeConnectionState = Exclude<RealtimeConnectionState, "closed">;

type SubscribeMissionControlRealtimeParams = {
  resumeToken: string;
  onMessage: (message: MissionControlRealtimeEvent) => void;
  onStateChange?: (state: MissionControlRealtimeConnectionState) => void;
  onInitialMessageTimeout?: () => void;
};

const initialMissionControlRealtimeTimeoutMs = 10000;

function buildMissionControlRealtimeURL(resumeToken: string): string {
  const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
  const url = new URL(`${protocol}//${window.location.host}/api/v1/staff/mission-control/realtime`);
  url.searchParams.set("resume_token", resumeToken);
  return url.toString();
}

export function subscribeMissionControlRealtime(params: SubscribeMissionControlRealtimeParams): () => void {
  const client = createRealtimeClient<MissionControlRealtimeEvent>({
    url: buildMissionControlRealtimeURL(params.resumeToken),
    parseMessage: parseMissionControlRealtimeEnvelope,
    onMessage: params.onMessage,
    firstMessageTimeoutMs: initialMissionControlRealtimeTimeoutMs,
    onFirstMessageTimeout: params.onInitialMessageTimeout,
    onStateChange: (state) => {
      if (state === "closed") return;
      params.onStateChange?.(state);
    },
  });

  client.start();
  return () => client.stop();
}
