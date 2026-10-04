import type {
  Artifact,
  AssistantTurn,
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
  execution?: RunEvent["execution"];
  phase?: NonNullable<RunEvent["message"]>["phase"];
  revision?: number;
  historical: boolean;
}

interface ActivityContext {
  initiator?: string;
  target?: string;
  platform?: string;
  nodes?: readonly RunNode[];
}

export function isTranscriptNearBottom(position: {
  scrollHeight: number;
  clientHeight: number;
  scrollTop: number;
}): boolean {
  return (
    position.scrollHeight - position.clientHeight - position.scrollTop <= 96
  );
}

// Привязка берётся только из опубликованного события, не из текущего графа.
export function executionKey(
  execution: RunEvent["execution"],
): string | undefined {
  if (
    !execution ||
    !execution.runRef ||
    !execution.nodeRef ||
    !execution.sessionRef ||
    !execution.turnRef ||
    !Number.isSafeInteger(execution.turnNumber) ||
    execution.turnNumber < 1 ||
    !Number.isSafeInteger(execution.attempt) ||
    execution.attempt < 1
  )
    return undefined;
  return JSON.stringify([
    execution.runRef,
    execution.nodeRef,
    execution.sessionRef,
    execution.turnRef,
    execution.turnNumber,
    execution.attempt,
  ]);
}

export function buildRunTranscriptItems(
  events: readonly RunEvent[],
  context: ActivityContext = {},
): RunActivityItem[] {
  const nodes = new Map(context.nodes?.map((node) => [node.ref, node]));
  const items: RunActivityItem[] = [];
  const revisions = new Map<string, number>();
  const positionByKey = new Map<string, number>();
  const finalScopes = new Set(
    events
      .filter((event) => publishedRunMessage(event)?.phase === "FINAL")
      .map((event) => executionKey(event.execution))
      .filter(Boolean),
  );
  for (const event of [...events].sort(
    (left, right) =>
      left.sequence - right.sequence || left.ref.localeCompare(right.ref),
  )) {
    const scope = executionKey(event.execution);
    if (
      scope &&
      !event.message &&
      event.messageKind === "FINAL_MESSAGE" &&
      finalScopes.has(scope)
    )
      continue;
    const message = publishedRunMessage(event);
    const tool =
      scope &&
      event.toolCall &&
      event.toolCall.ref &&
      typeof event.toolCall.revision === "number" &&
      Number.isSafeInteger(event.toolCall.revision) &&
      event.toolCall.revision >= 1
        ? event.toolCall
        : undefined;
    const kind: RunActivityItem["kind"] = tool
      ? "tool"
      : message?.phase === "USER"
        ? "initiator"
        : message
          ? "agent"
          : "system";
    const revision = tool?.revision ?? message?.revision;
    const key =
      scope && revision !== undefined
        ? JSON.stringify([
            scope,
            tool ? "tool" : "message",
            tool?.ref ?? message?.ref,
          ])
        : undefined;
    const previousPosition = key ? positionByKey.get(key) : undefined;
    if (key && revision !== undefined && revision <= (revisions.get(key) ?? 0))
      continue;
    const previous =
      previousPosition !== undefined ? items[previousPosition] : undefined;
    const presented = event as Partial<PresentedRunEvent>;
    const item: RunActivityItem = {
      id: previous?.id ?? key ?? event.ref,
      kind,
      actor:
        event.actor?.name ??
        (kind === "initiator"
          ? context.initiator
          : scope
            ? (nodes.get(event.execution?.nodeRef ?? "")?.displayName ??
              context.target)
            : undefined) ??
        context.platform ??
        "",
      summary: event.message
        ? message?.text
        : (presented.displaySummary ?? event.summary),
      progress: message
        ? undefined
        : event.message
          ? undefined
          : (presented.displayProgress ?? event.progress),
      nodeRef:
        scope && event.messageKind !== "OWNER_GATE"
          ? event.execution?.nodeRef
          : undefined,
      occurredAt: previous?.occurredAt ?? event.occurredAt,
      sequence: previous?.sequence ?? event.sequence,
      state: event.nodeState ?? event.runState,
      messageKind: event.messageKind,
      toolCall: tool,
      artifactRef: event.artifactRef,
      artifact: event.artifact,
      execution: scope ? event.execution : undefined,
      phase: message?.phase,
      revision,
      historical: !scope,
    };
    if (previousPosition !== undefined) items[previousPosition] = item;
    else {
      if (key) positionByKey.set(key, items.length);
      items.push(item);
    }
    if (key && revision !== undefined) revisions.set(key, revision);
  }
  return items.sort(
    (left, right) =>
      Number(left.historical) - Number(right.historical) ||
      (left.execution?.turnNumber ?? 0) - (right.execution?.turnNumber ?? 0) ||
      (left.sequence ?? 0) - (right.sequence ?? 0) ||
      left.id.localeCompare(right.id),
  );
}

export function publishedRunMessage(event: RunEvent): RunEvent["message"] {
  const message = event.message;
  if (!message) return undefined;
  const byteLength = new TextEncoder().encode(message.text).length;
  const withinBudget =
    message.phase === "USER"
      ? Array.from(message.text).length <= 100000 && byteLength <= 400000
      : byteLength <= 65536;
  return executionKey(event.execution) &&
    message.ref &&
    Number.isSafeInteger(message.revision) &&
    message.revision >= 1 &&
    ["USER", "COMMENTARY", "FINAL"].includes(message.phase) &&
    withinBudget
    ? message
    : undefined;
}

// Summary из истории заменяется только событием exact persisted USER/run/node
// binding. Receipt ASSISTANT имеет собственный ref: он не является turnRef.
export function assistantTurnHasAuthoritativeActivity(
  turn: AssistantTurn,
  turns: readonly AssistantTurn[],
  run: Run | undefined,
  nodes: readonly RunNode[],
  events: readonly RunEvent[],
): boolean {
  if (!run || turn.runRef !== run.ref || turn.role === "SYSTEM_RECEIPT")
    return false;
  const anchors = turns.filter(
    (item) => item.role === "USER" && item.runRef === run.ref,
  );
  if (anchors.length !== 1) return false;
  const anchor = anchors[0];
  if (!anchor) return false;
  if (turn.role === "USER" && turn.ref !== anchor.ref) return false;
  return events.some((event) => {
    const execution = event.execution;
    if (
      !executionKey(execution) ||
      !execution ||
      event.runRef !== run.ref ||
      execution.runRef !== run.ref ||
      execution.sessionRef !== run.sessionRef ||
      execution.turnRef !== anchor.ref ||
      execution.turnNumber !== anchor.sequence ||
      execution.attempt !== run.attempt
    )
      return false;
    const node = nodes.find(
      (item) => item.ref === execution.nodeRef && item.runRef === run.ref,
    );
    if (
      !node ||
      node.turnRef !== anchor.ref ||
      node.attempt !== execution.attempt ||
      node.type !== "AGENT_EXECUTION"
    )
      return false;
    const message = publishedRunMessage(event);
    if (turn.role === "USER") return message?.phase === "USER";
    if (message?.phase === "FINAL") return true;
    // Успешная служебная запись не заменяет ещё не полученный полный ответ.
    return (
      (turn.state === "FAILED" &&
        event.type === "TURN_COMPLETED" &&
        event.nodeState === "FAILED") ||
      (turn.state === "CANCELLED" &&
        event.nodeState === "CANCELLED" &&
        event.type === "TURN_COMPLETED")
    );
  });
}

export function buildRunActivityItems(
  run: Run,
  nodes: RunNode[],
  events: PresentedRunEvent[],
  initiatorSummary?: string,
): RunActivityItem[] {
  const items = buildRunTranscriptItems(events, {
    nodes,
    initiator: run.initiator.displayName,
    target: run.target.displayName,
    platform: run.title,
  });
  if (
    initiatorSummary?.trim() &&
    !items.some((item) => item.phase === "USER")
  ) {
    items.unshift({
      id: `initiator-${run.ref}`,
      kind: "initiator",
      actor: run.initiator.displayName,
      summary: initiatorSummary.trim(),
      occurredAt: run.createdAt,
      historical: true,
    });
  }

  return items;
}
