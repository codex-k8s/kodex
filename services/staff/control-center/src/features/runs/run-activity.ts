import type {
  Run,
  RunEvent,
  RunNode,
} from "@/shared/api/generated/openapi/types.gen";

export type PresentedRunEvent = RunEvent & {
  displaySummary: string;
  displayProgress?: string;
};

export interface RunActivityItem {
  id: string;
  kind: "initiator" | "agent" | "tool" | "system";
  actor: string;
  summary?: string;
  progress?: string;
  nodeRef?: string;
  occurredAt: string;
  sequence?: number;
  state?: RunEvent["nodeState"] | RunEvent["runState"];
  messageKind?: RunEvent["messageKind"];
  toolCall?: RunEvent["toolCall"];
}

const agentMessageTypes = new Set<RunEvent["type"]>([
  "TURN_STARTED",
  "TURN_PROGRESS",
  "TURN_COMPLETED",
]);

export function buildRunActivityItems(
  run: Run,
  nodes: RunNode[],
  events: PresentedRunEvent[],
  initiatorSummary?: string,
): RunActivityItem[] {
  const nodeByRef = new Map(nodes.map((node) => [node.ref, node]));
  const items: RunActivityItem[] = [
    {
      id: `initiator-${run.ref}`,
      kind: "initiator",
      actor: run.initiator.displayName,
      summary: initiatorSummary,
      occurredAt: run.createdAt,
    },
  ];

  for (const event of [...events].sort(
    (left, right) => left.sequence - right.sequence,
  )) {
    const node = event.nodeRef ? nodeByRef.get(event.nodeRef) : undefined;
    const kind: RunActivityItem["kind"] = event.toolCall
      ? "tool"
      : event.actor?.kind === "USER"
        ? "initiator"
        : event.actor?.kind === "AGENT" ||
            event.actor?.kind === "SYSTEM_ASSISTANT" ||
            agentMessageTypes.has(event.type)
          ? "agent"
          : "system";
    items.push({
      id: event.ref,
      kind,
      actor:
        event.actor?.name ??
        (kind === "initiator"
          ? run.initiator.displayName
          : kind === "agent" || kind === "tool"
            ? (node?.displayName ?? run.target.displayName)
            : (node?.displayName ?? run.title)),
      summary: event.displaySummary,
      progress: event.displayProgress,
      nodeRef: event.nodeRef,
      occurredAt: event.occurredAt,
      sequence: event.sequence,
      state: event.nodeState ?? event.runState,
      messageKind: event.messageKind,
      toolCall: event.toolCall,
    });
  }

  return items;
}
