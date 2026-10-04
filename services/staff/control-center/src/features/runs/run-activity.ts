import type {
  Artifact,
  AssistantConversation,
  AssistantTurn,
  Run,
  RunEvent,
  RunNode,
} from "@/shared/api/generated/openapi/types.gen";
import { assertRunOwner } from "@/features/runs/run-owner";

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
  eventType?: RunEvent["type"];
  serviceProgressCode?: "WORKLOAD_SCHEDULED" | "MODEL_REQUEST_RUNNING";
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
      eventType: event.type,
      serviceProgressCode:
        transcriptServiceProgressCode(event.summary) ??
        transcriptServiceProgressCode(event.progress),
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

const activeTranscriptStates = new Set([
  "CREATED",
  "QUEUED",
  "PENDING",
  "READY",
  "CLAIMED",
  "RUNNING",
]);
const terminalTranscriptStates = new Set([
  "SUCCEEDED",
  "FAILED",
  "CANCELLED",
  "SKIPPED",
]);
const serviceStartEvents = new Set([
  "RUN_CREATED",
  "TURN_QUEUED",
  "TURN_STARTED",
]);
const serviceProgressEvents = new Set([
  "TURN_PROGRESS",
  "RUN_STATE_CHANGED",
  "NODE_STATE_CHANGED",
]);
const serviceTerminalEvents = new Set([
  "TURN_COMPLETED",
  "RUN_STATE_CHANGED",
  "NODE_STATE_CHANGED",
]);
const serviceProgressCodes = new Set([
  "WORKLOAD_SCHEDULED",
  "MODEL_REQUEST_RUNNING",
]);
const assistantFailureMessageCodes = new Set([
  "PROVIDER_RESULT_UNVERIFIABLE",
  "PROVIDER_RESULT_UNKNOWN",
  "PROVIDER_AUTHENTICATION_REQUIRED",
  "PROVIDER_USAGE_LIMIT_EXCEEDED",
  "PROVIDER_OVERLOADED",
  "PROVIDER_POLICY_DENIED",
  "RUNTIME_CONFIGURATION_STALE",
  "RUNTIME_PROVIDER_UNAVAILABLE",
]);

export function assistantFailureMessageKey(
  value: string | undefined,
  state: string | undefined,
): string | undefined {
  if (state !== "FAILED") return undefined;
  const code = value?.trim().replace(/^i18n:/, "");
  return code && assistantFailureMessageCodes.has(code)
    ? `serverMessages.${code}`
    : undefined;
}

function transcriptServiceProgressCode(
  value: string | undefined,
): RunActivityItem["serviceProgressCode"] {
  const code = value?.trim().replace(/^i18n:/, "");
  return code === "WORKLOAD_SCHEDULED" || code === "MODEL_REQUEST_RUNNING"
    ? code
    : undefined;
}

export interface PresentedTranscriptItem extends RunActivityItem {
  working: boolean;
  serviceHistory?: readonly RunActivityItem[];
}

function isTranscriptService(item: RunActivityItem): boolean {
  return Boolean(
    !item.historical &&
    executionKey(item.execution) &&
    item.kind === "system" &&
    !item.phase &&
    !item.toolCall &&
    !item.artifact &&
    !item.artifactRef &&
    (!item.messageKind ||
      ["STATE", "FINAL_MESSAGE"].includes(item.messageKind) ||
      (item.eventType === "TURN_PROGRESS" &&
        item.messageKind === "INTERMEDIATE_MESSAGE" &&
        Boolean(
          item.serviceProgressCode ??
          transcriptServiceProgressCode(item.summary),
        ))) &&
    (serviceStartEvents.has(item.eventType ?? "") ||
      (serviceProgressEvents.has(item.eventType ?? "") &&
        activeTranscriptStates.has(item.state ?? "")) ||
      serviceProgressCodes.has(
        item.serviceProgressCode ??
          transcriptServiceProgressCode(item.summary) ??
          "",
      ) ||
      (terminalTranscriptStates.has(item.state ?? "") &&
        (serviceTerminalEvents.has(item.eventType ?? "") ||
          item.messageKind === "FINAL_MESSAGE"))),
  );
}

// Это только представление: исходные события, revisions и их порядок не меняются.
// Работающий статус принадлежит последней записи текущего exact хода/попытки.
export function activeTranscriptItemId(
  items: readonly RunActivityItem[],
  closedExecutionKeys: readonly string[] = [],
): string | null {
  const current = new Map<string, NonNullable<RunActivityItem["execution"]>>();
  const closed = new Set<string>(closedExecutionKeys);
  const states = new Map<string, string>();
  const sessionKey = (execution: NonNullable<RunActivityItem["execution"]>) =>
    JSON.stringify([execution.runRef, execution.nodeRef, execution.sessionRef]);
  for (const item of items) {
    const scope = executionKey(item.execution);
    if (!scope || !item.execution || item.historical) continue;
    const key = sessionKey(item.execution);
    const previous = current.get(key);
    if (
      !previous ||
      item.execution.turnNumber > previous.turnNumber ||
      (item.execution.turnNumber === previous.turnNumber &&
        item.execution.attempt > previous.attempt)
    )
      current.set(key, item.execution);
    if (!item.toolCall && !item.artifact) {
      if (
        item.phase === "FINAL" ||
        terminalTranscriptStates.has(item.state ?? "")
      )
        closed.add(scope);
      if (item.state) states.set(scope, item.state);
    }
  }
  const candidates = items.filter((item) => {
    const scope = executionKey(item.execution);
    return (
      scope &&
      item.execution &&
      !item.historical &&
      item.kind !== "initiator" &&
      !closed.has(scope) &&
      executionKey(current.get(sessionKey(item.execution))) === scope &&
      (activeTranscriptStates.has(states.get(scope) ?? "") ||
        item.toolCall?.state === "RUNNING")
    );
  });
  const latest = candidates.at(-1);
  return latest?.toolCall && latest.toolCall.state !== "RUNNING"
    ? null
    : (latest?.id ?? null);
}

// Terminal receipt может прийти раньше terminal RunEvent. Привязка проверяется
// по owner snapshot и persisted USER/node, а не по текущему экрану или receipt ref.
export function assistantTerminalTranscriptScopes(
  conversation: AssistantConversation | undefined,
  organizationRef: string | undefined,
  run: Run | undefined,
  nodes: readonly RunNode[],
  events: readonly RunEvent[],
): string[] {
  if (
    !conversation ||
    !organizationRef ||
    !run ||
    run.source !== "SYSTEM_ASSISTANT" ||
    run.target.type !== "SYSTEM_ASSISTANT"
  )
    return [];
  try {
    assertRunOwner(run, organizationRef);
  } catch {
    return [];
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
    return [];
  const terminalReceipt = conversation.turns.some(
    (turn) =>
      turn.role === "ASSISTANT" &&
      turn.runRef === run.ref &&
      ["COMPLETED", "FAILED", "CANCELLED"].includes(turn.state) &&
      Number.isSafeInteger(turn.runVersion) &&
      (turn.runVersion ?? 0) >= run.version,
  );
  const anchors = conversation.turns.filter(
    (turn) => turn.role === "USER" && turn.runRef === run.ref,
  );
  const anchor = anchors.length === 1 ? anchors[0] : undefined;
  if (!anchor) return [];
  const keys = events.flatMap((event) => {
    const scope = executionKey(event.execution);
    const execution = event.execution;
    if (
      !scope ||
      !execution ||
      event.runRef !== run.ref ||
      execution.runRef !== run.ref ||
      execution.sessionRef !== run.sessionRef ||
      execution.turnRef !== anchor.ref ||
      execution.turnNumber !== anchor.sequence ||
      execution.attempt !== run.attempt
    )
      return [];
    const node = nodes.find(
      (candidate) =>
        candidate.ref === execution.nodeRef && candidate.runRef === run.ref,
    );
    if (
      !node ||
      node.type !== "AGENT_EXECUTION" ||
      node.turnRef !== anchor.ref ||
      node.attempt !== run.attempt ||
      (node.agentRef !== undefined && node.agentRef !== pin.assistantRef)
    )
      return [];
    if (
      !terminalReceipt &&
      !terminalTranscriptStates.has(run.state) &&
      !terminalTranscriptStates.has(node.state)
    )
      return [];
    return [scope];
  });
  return [...new Set(keys)];
}

// Fallback нужен лишь до первого exact рабочего события последнего хода.
// Событие другого параллельного run или UNSCOPED история его не заменяет.
export function assistantTranscriptReplacesWorkingFallback(
  conversation: AssistantConversation | undefined,
  organizationRef: string | undefined,
  run: Run | undefined,
  nodes: readonly RunNode[],
  events: readonly RunEvent[],
): boolean {
  const latest = conversation?.turns.at(-1);
  if (
    !conversation ||
    !organizationRef ||
    !run ||
    !latest ||
    latest.role === "SYSTEM_RECEIPT" ||
    latest.runRef !== run.ref ||
    !Number.isSafeInteger(latest.runVersion) ||
    (latest.runVersion ?? 0) < run.version ||
    run.source !== "SYSTEM_ASSISTANT" ||
    run.target.type !== "SYSTEM_ASSISTANT"
  )
    return false;
  try {
    assertRunOwner(run, organizationRef);
  } catch {
    return false;
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
    return false;
  const anchors = conversation.turns.filter(
    (turn) => turn.role === "USER" && turn.runRef === run.ref,
  );
  const anchor = anchors.length === 1 ? anchors[0] : undefined;
  if (!anchor) return false;
  const bound = events.filter((event) => {
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
    return nodes.some(
      (node) =>
        node.ref === execution.nodeRef &&
        node.runRef === run.ref &&
        node.type === "AGENT_EXECUTION" &&
        node.turnRef === anchor.ref &&
        node.attempt === run.attempt &&
        (node.agentRef === undefined || node.agentRef === pin.assistantRef),
    );
  });
  const closed = assistantTerminalTranscriptScopes(
    conversation,
    organizationRef,
    run,
    nodes,
    bound,
  );
  const items = buildRunTranscriptItems(bound);
  return (
    closed.length > 0 ||
    activeTranscriptItemId(items, closed) !== null ||
    items.some((item) => item.phase === "FINAL")
  );
}

export function presentRunTranscriptItems(
  items: readonly RunActivityItem[],
  activeItemId: string | null = activeTranscriptItemId(items),
): PresentedTranscriptItem[] {
  const services = new Map<
    string,
    { items: RunActivityItem[]; last: number }
  >();
  items.forEach((item, index) => {
    if (!isTranscriptService(item)) return;
    const key = executionKey(item.execution);
    if (!key) return;
    const group = services.get(key) ?? { items: [], last: index };
    group.items.push(item);
    group.last = index;
    services.set(key, group);
  });
  return items.flatMap((item, index) => {
    if (!isTranscriptService(item))
      return [{ ...item, working: item.id === activeItemId }];
    const key = executionKey(item.execution);
    const group = key ? services.get(key) : undefined;
    if (!group) return [{ ...item, working: item.id === activeItemId }];
    if (group.last !== index) return [];
    const terminal = group.items.filter((entry) =>
      terminalTranscriptStates.has(entry.state ?? ""),
    );
    const representative =
      [...terminal]
        .reverse()
        .find((entry) => entry.messageKind === "FINAL_MESSAGE") ??
      terminal.at(-1) ??
      item;
    return [
      {
        ...representative,
        id: group.items[0]?.id ?? item.id,
        working: group.items.some((entry) => entry.id === activeItemId),
        serviceHistory: group.items,
      },
    ];
  });
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
