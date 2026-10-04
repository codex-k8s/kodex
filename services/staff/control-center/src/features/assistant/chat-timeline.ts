import {
  executionKey,
  activeTranscriptItemId,
  buildRunTranscriptItems,
} from "@/features/runs/run-activity";
import { assertRunOwner } from "@/features/runs/run-owner";
import type {
  AssistantConversation,
  AssistantTurn,
  Run,
  RunEvent,
  RunGraph,
} from "@/shared/api/generated/openapi/types.gen";

export type AssistantChatTimelineEntry =
  | { kind: "TURN"; id: string; turn: AssistantTurn }
  | { kind: "ACTIVITY"; id: string; events: RunEvent[]; isolated: boolean };

export function activeAssistantChatItemId(
  timeline: readonly AssistantChatTimelineEntry[],
  closedExecutionKeys: readonly string[],
): string | null {
  return activeTranscriptItemId(
    buildRunTranscriptItems(
      timeline.flatMap((entry) =>
        entry.kind === "ACTIVITY" && !entry.isolated ? entry.events : [],
      ),
    ),
    closedExecutionKeys,
  );
}

// Порядок receipts принадлежит conversation. События вставляются после
// единственного persisted USER только при совпадении полного owner/run binding.
// Неизвестные привязки сохраняются отдельно и не заменяют сообщения receipts.
export function buildAssistantChatTimeline(
  conversation: AssistantConversation | undefined,
  visibleTurns: readonly AssistantTurn[],
  organizationRef: string | undefined,
  runs: Readonly<Record<string, Run>>,
  graphs: Readonly<Record<string, RunGraph>>,
  events: readonly RunEvent[],
): AssistantChatTimelineEntry[] {
  if (!conversation) return [];
  const visible = new Set(visibleTurns.map((turn) => turn.ref));
  const byAnchor = new Map<string, RunEvent[]>();
  const assigned = new Set<RunEvent>();
  const turns = [...conversation.turns].sort(
    (left, right) =>
      left.sequence - right.sequence || left.ref.localeCompare(right.ref),
  );
  for (const anchor of turns) {
    if (anchor.role !== "USER" || !anchor.runRef) continue;
    if (
      turns.filter(
        (turn) => turn.role === "USER" && turn.runRef === anchor.runRef,
      ).length !== 1
    )
      continue;
    const run = runs[anchor.runRef];
    if (
      !run ||
      !organizationRef ||
      run.ref !== anchor.runRef ||
      run.source !== "SYSTEM_ASSISTANT" ||
      run.target.type !== "SYSTEM_ASSISTANT"
    )
      continue;
    try {
      assertRunOwner(run, organizationRef);
    } catch {
      continue;
    }
    const pin = run.assistantPin;
    if (
      !pin ||
      pin.conversationRef !== conversation.ref ||
      pin.scope !== conversation.assistantScope ||
      pin.assistantRef !== conversation.assistantRef ||
      pin.projectRef !== conversation.projectRef ||
      pin.profileRef !== conversation.assistantProfileRef
    )
      continue;
    const graph = graphs[run.rootRunRef] ?? graphs[run.ref];
    if (!graph || (graph.runRef !== run.rootRunRef && graph.runRef !== run.ref))
      continue;
    const bound = events.filter((event) => {
      const execution = event.execution;
      return Boolean(
        executionKey(execution) &&
        execution &&
        event.runRef === run.ref &&
        execution.runRef === run.ref &&
        execution.sessionRef === run.sessionRef &&
        execution.turnRef === anchor.ref &&
        execution.turnNumber === anchor.sequence &&
        execution.attempt === run.attempt &&
        graph.nodes.some(
          (node) =>
            node.ref === execution.nodeRef &&
            node.runRef === run.ref &&
            node.type === "AGENT_EXECUTION" &&
            node.turnRef === anchor.ref &&
            node.attempt === execution.attempt &&
            (node.agentRef === undefined || node.agentRef === pin.assistantRef),
        ),
      );
    });
    if (bound.length) {
      byAnchor.set(anchor.ref, bound);
      for (const event of bound) assigned.add(event);
    }
  }
  const result: AssistantChatTimelineEntry[] = [];
  for (const turn of turns) {
    if (visible.has(turn.ref))
      result.push({ kind: "TURN", id: turn.ref, turn });
    const bound = byAnchor.get(turn.ref);
    if (bound)
      result.push({
        kind: "ACTIVITY",
        id: `activity:${turn.ref}`,
        events: bound,
        isolated: false,
      });
  }
  const isolated = events.filter((event) => !assigned.has(event));
  if (isolated.length)
    result.push({
      kind: "ACTIVITY",
      id: "activity:isolated",
      events: isolated,
      isolated: true,
    });
  return result;
}
