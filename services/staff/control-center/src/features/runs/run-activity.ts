import { indexRunSessionOwnership } from "@/features/runs/run-session-graph";
import type {
  Artifact,
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
  artifactRef?: string;
  artifact?: Artifact;
}

const agentMessageKinds = new Set<RunEvent["messageKind"]>([
  "ASSISTANT_MESSAGE",
  "INTERMEDIATE_MESSAGE",
  "FINAL_MESSAGE",
]);

export function buildRunActivityItems(
  run: Run,
  nodes: RunNode[],
  events: PresentedRunEvent[],
  initiatorSummary?: string,
): RunActivityItem[] {
  const nodeByRef = new Map(nodes.map((node) => [node.ref, node]));
  const sessionOwnership = indexRunSessionOwnership(nodes);
  const items: RunActivityItem[] = [];
  if (initiatorSummary?.trim()) {
    items.push({
      id: `initiator-${run.ref}`,
      kind: "initiator",
      actor: run.initiator.displayName,
      summary: initiatorSummary.trim(),
      occurredAt: run.createdAt,
    });
  }

  for (const event of [...events].sort(
    (left, right) => left.sequence - right.sequence,
  )) {
    const sessionNodeRef = event.nodeRef
      ? sessionOwnership.get(event.nodeRef)
      : undefined;
    const node = sessionNodeRef ? nodeByRef.get(sessionNodeRef) : undefined;
    const kind: RunActivityItem["kind"] = event.toolCall
      ? "tool"
      : event.actor?.kind === "USER" || event.messageKind === "USER_MESSAGE"
        ? "initiator"
        : agentMessageKinds.has(event.messageKind) ||
            event.actor?.kind === "AGENT" ||
            event.actor?.kind === "SYSTEM_ASSISTANT"
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
      // Human Gate changes are run-wide owner decisions. They remain visible
      // even when the timeline is narrowed to one agent session.
      nodeRef: event.messageKind === "OWNER_GATE" ? undefined : sessionNodeRef,
      occurredAt: event.occurredAt,
      sequence: event.sequence,
      state: event.nodeState ?? event.runState,
      messageKind: event.messageKind,
      toolCall: event.toolCall,
      artifactRef: event.artifactRef,
      artifact: event.artifact,
    });
  }

  return items;
}
