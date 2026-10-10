import type {
  Artifact,
  AssistantConversation,
  AssistantTurn,
  Run,
  RunEvent,
  RunNode,
} from "@/shared/api/generated/openapi/types.gen";
import { assertRunOwner } from "@/features/runs/run-owner";
import { serverMessageKey } from "@/shared/ui/server-message";

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
  messageOrigin?: NonNullable<RunEvent["message"]>["source"]["origin"];
  revision?: number;
  historical: boolean;
  eventType?: RunEvent["type"];
  serviceProgressCode?: "WORKLOAD_SCHEDULED" | "MODEL_REQUEST_RUNNING";
  serviceCancellationCode?: RunEvent["serviceCode"];
  serviceCompletionCode?: "ROOT_PROCESS_COMPLETED";
  executionNodeType?: RunNode["type"];
  executionSafeErrorCode?: RunNode["safeErrorCode"];
  integrationInvocationRef?: string;
}

// Только exact машинная квитанция успешной подготовки плана, не его описание.
export function isAssistantPlanToolReceipt(
  tool: NonNullable<RunActivityItem["toolCall"]>,
): boolean {
  return (
    tool.state === "SUCCEEDED" &&
    tool.tool === "propose_configuration_plan" &&
    /^propose_configuration_plan:pln_[A-Za-z0-9_-]{8,124}$/.test(
      tool.safeResult,
    )
  );
}

// Только каноническая безопасная квитанция; произвольный результат не скрывается.
export function isSuccessfulIntegrationToolReceipt(
  tool: NonNullable<RunActivityItem["toolCall"]>,
): boolean {
  return Boolean(successfulIntegrationInvocationRef(tool));
}

function successfulIntegrationInvocationRef(
  tool: NonNullable<RunActivityItem["toolCall"]>,
): string | undefined {
  const receipt = integrationToolReceipt(tool);
  return receipt?.state === "SUCCEEDED" ? receipt.invocationRef : undefined;
}

type IntegrationReceiptState =
  | "SUCCEEDED"
  | "FAILED"
  | "REJECTED"
  | "CANCELLED"
  | "WAITING_APPROVAL"
  | "UNKNOWN_OUTCOME";

// Состояние отображения не заменяет исходное состояние вызова в аудите.
export function integrationToolPresentationState(
  tool: NonNullable<RunActivityItem["toolCall"]>,
):
  | "SUCCEEDED"
  | "FAILED"
  | "REJECTED"
  | "CANCELLED"
  | "WAITING_HUMAN"
  | "NEEDS_ATTENTION"
  | undefined {
  const receipt = integrationToolReceipt(tool);
  if (receipt?.state === "WAITING_APPROVAL") return "WAITING_HUMAN";
  if (receipt?.state === "UNKNOWN_OUTCOME") return "NEEDS_ATTENTION";
  return receipt?.state;
}

function integrationToolReceipt(
  tool: NonNullable<RunActivityItem["toolCall"]>,
): { invocationRef: string; state: IntegrationReceiptState } | undefined {
  if (
    tool.state !== "SUCCEEDED" ||
    tool.safeResult.length > 512 ||
    ![
      "invoke_integration",
      "context7_resolve_library_id",
      "context7_query_docs",
    ].includes(tool.tool)
  )
    return undefined;
  try {
    const value: unknown = JSON.parse(tool.safeResult);
    if (!value || typeof value !== "object" || Array.isArray(value))
      return undefined;
    const receipt = value as Record<string, unknown>;
    const valid =
      receipt.version === 1 &&
      typeof receipt.state === "string" &&
      [
        "SUCCEEDED",
        "FAILED",
        "REJECTED",
        "CANCELLED",
        "WAITING_APPROVAL",
        "UNKNOWN_OUTCOME",
      ].includes(receipt.state) &&
      typeof receipt.invocationRef === "string" &&
      /^inv_[A-Za-z0-9_-]{8,124}$/.test(receipt.invocationRef) &&
      typeof receipt.inputSHA256 === "string" &&
      /^[a-f0-9]{64}$/.test(receipt.inputSHA256) &&
      tool.safeResult ===
        JSON.stringify({
          version: 1,
          invocationRef: receipt.invocationRef,
          state: receipt.state,
          inputSHA256: receipt.inputSHA256,
        });
    return valid
      ? {
          invocationRef: String(receipt.invocationRef),
          state: receipt.state as IntegrationReceiptState,
        }
      : undefined;
  } catch {
    return undefined;
  }
}

interface ActivityContext {
  initiator?: string;
  target?: string;
  platform?: string;
  nodes?: readonly RunNode[];
  graphRootRunRef?: string;
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

// Root stream связывает child execution только через авторитетный owner graph.
export function isGraphChildExecutionBound(
  event: RunEvent,
  context: ActivityContext,
): boolean {
  const candidates = context.nodes?.filter(
    (node) => node.ref === event.execution?.nodeRef,
  );
  const node = candidates?.[0];
  return Boolean(
    executionKey(event.execution) &&
    context.graphRootRunRef &&
    event.runRef === context.graphRootRunRef &&
    event.execution?.runRef !== context.graphRootRunRef &&
    candidates?.length === 1 &&
    node?.type === "AGENT_EXECUTION" &&
    node.runRef === event.execution?.runRef &&
    node.turnRef === event.execution.turnRef &&
    node.attempt === event.execution.attempt &&
    context.nodes?.some((parent) => parent.childRunRefs.includes(node.runRef)),
  );
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
    const callback = message?.source.origin === "CALLBACK_CONTINUATION";
    const tool =
      scope &&
      event.toolCall &&
      event.toolCall.ref &&
      typeof event.toolCall.revision === "number" &&
      Number.isSafeInteger(event.toolCall.revision) &&
      event.toolCall.revision >= 1
        ? event.toolCall
        : undefined;
    // USER обозначает входное задание, а сторону сообщения задаёт его автор.
    const kind: RunActivityItem["kind"] = tool
      ? "tool"
      : callback
        ? "agent"
        : message?.phase === "USER" && event.actor?.kind === "USER"
          ? "initiator"
          : message &&
              (message.phase !== "USER" ||
                event.actor?.kind === "AGENT" ||
                event.actor?.kind === "SYSTEM_ASSISTANT")
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
    // Привязка повторного подтверждения не выводится из текста или aggregateRef.
    const graphExecutionNodes = context.nodes?.filter(
      (node) => node.ref === event.execution?.nodeRef,
    );
    const graphExecutionNode = graphExecutionNodes?.[0];
    // Root stream хранит события дочернего run: lineage берётся из owner graph.
    const graphChildBound = isGraphChildExecutionBound(event, context);
    const integrationBound = Boolean(
      scope &&
      event.execution &&
      (event.runRef === event.execution.runRef || graphChildBound) &&
      event.nodeRef === event.execution.nodeRef &&
      event.run.ref === event.runRef &&
      Number.isSafeInteger(event.run.version) &&
      event.run.version >= 1,
    );
    const executionNode = event.node ?? graphExecutionNode;
    const executionNodeBound = Boolean(
      scope &&
      event.execution &&
      (event.runRef === event.execution.runRef || graphChildBound) &&
      event.nodeRef === event.execution.nodeRef &&
      event.run.ref === event.runRef &&
      Number.isSafeInteger(event.run.version) &&
      event.run.version >= 1 &&
      executionNode?.ref === event.execution.nodeRef &&
      executionNode.runRef === event.execution.runRef &&
      executionNode.turnRef === event.execution.turnRef &&
      executionNode.attempt === event.execution.attempt,
    );
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
      summary: callback
        ? "i18n:CALLBACK_CONTINUATION_PUBLIC"
        : event.message
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
      messageKind: callback ? "INTERMEDIATE_MESSAGE" : event.messageKind,
      toolCall: tool,
      artifactRef: event.artifactRef,
      artifact: event.artifact,
      execution: scope ? event.execution : undefined,
      phase: callback ? undefined : message?.phase,
      messageOrigin: message?.source.origin,
      revision,
      historical: !scope,
      eventType: event.type,
      serviceProgressCode:
        transcriptServiceProgressCode(event.summary) ??
        transcriptServiceProgressCode(event.progress),
      serviceCancellationCode: transcriptServiceCancellationCode(
        event.serviceCode,
      ),
      executionNodeType: executionNodeBound ? executionNode?.type : undefined,
      executionSafeErrorCode: executionNodeBound
        ? executionNode?.safeErrorCode
        : undefined,
      serviceCompletionCode:
        executionNodeBound &&
        executionNode?.type === "ROOT_PROCESS" &&
        event.type === "NODE_STATE_CHANGED" &&
        event.messageKind === "STATE" &&
        event.serviceCode === "ROOT_PROCESS_COMPLETED" &&
        event.nodeState === executionNode.state &&
        ["SUCCEEDED", "FAILED", "CANCELLED"].includes(event.nodeState ?? "") &&
        !event.message &&
        !event.toolCall &&
        !event.artifact &&
        !event.artifactRef &&
        !event.gate &&
        !event.gateRef &&
        !event.edge &&
        !event.edgeRef &&
        !event.incident &&
        !event.progress?.trim()
          ? "ROOT_PROCESS_COMPLETED"
          : undefined,
      integrationInvocationRef: integrationBound
        ? tool
          ? successfulIntegrationInvocationRef(tool)
          : !event.message && !event.progress?.trim()
            ? event.integrationInvocationRef
            : undefined
        : undefined,
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
      // Номера ходов и sequence разных сессий не задают общую хронологию.
      Date.parse(left.occurredAt) - Date.parse(right.occurredAt) ||
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
    ["ORDINARY", "CALLBACK_CONTINUATION"].includes(message.source.origin) &&
    (message.source.origin !== "CALLBACK_CONTINUATION" ||
      (message.phase === "USER" &&
        event.actor?.kind === "AGENT" &&
        message.ref === event.execution?.turnRef)) &&
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
export function assistantFailureMessageKey(
  value: string | undefined,
  state: string | undefined,
): string | undefined {
  if (state !== "FAILED") return undefined;
  const code = value?.trim().replace(/^i18n:/, "");
  if (!code || !/^[A-Z][A-Z0-9_]*$/.test(code)) return undefined;
  return serverMessageKey(`i18n:${code}`) ?? "workboard.runFailedSummary";
}

function transcriptServiceProgressCode(
  value: string | undefined,
): RunActivityItem["serviceProgressCode"] {
  const code = value?.trim().replace(/^i18n:/, "");
  return code === "WORKLOAD_SCHEDULED" || code === "MODEL_REQUEST_RUNNING"
    ? code
    : undefined;
}

function transcriptServiceCancellationCode(
  value: string | undefined,
): RunActivityItem["serviceCancellationCode"] {
  return value === "RUN_CANCELLED" ||
    value === "RUN_NODE_CANCELLED" ||
    value === "ASSISTANT_TURN_CANCELLED"
    ? value
    : undefined;
}

export interface PresentedTranscriptItem extends RunActivityItem {
  working: boolean;
  serviceHistory?: readonly RunActivityItem[];
  completedServiceHistory?: readonly RunActivityItem[];
}

function isTranscriptService(item: RunActivityItem): boolean {
  const cancelledProgress = Boolean(
    item.eventType === "TURN_PROGRESS" &&
    item.messageKind === "INTERMEDIATE_MESSAGE" &&
    item.state === "CANCELLED" &&
    !item.progress?.trim() &&
    !item.integrationInvocationRef &&
    transcriptServiceCancellationCode(item.serviceCancellationCode),
  );
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
      cancelledProgress ||
      (item.eventType === "TURN_PROGRESS" &&
        item.messageKind === "INTERMEDIATE_MESSAGE" &&
        Boolean(
          item.serviceProgressCode ??
          transcriptServiceProgressCode(item.summary),
        ))) &&
    (serviceStartEvents.has(item.eventType ?? "") ||
      cancelledProgress ||
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
      !(
        item.phase === "USER" && item.messageOrigin !== "CALLBACK_CONTINUATION"
      ) &&
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

// Обычный drawer сохраняет индикатор между инструментами на последнем ответе
// или компактной служебной записи. Завершённый инструмент не становится busy.
export function ordinaryRunActiveTranscriptItemId(
  run: Run,
  nodes: readonly RunNode[],
  items: readonly RunActivityItem[],
): string | null {
  if (run.state !== "RUNNING") return null;
  const bound = items.filter((item) => {
    const execution = item.execution;
    return Boolean(
      executionKey(execution) &&
      execution &&
      !item.historical &&
      execution.runRef === run.ref &&
      execution.sessionRef === run.sessionRef &&
      nodes.some(
        (node) =>
          node.ref === execution.nodeRef &&
          node.runRef === run.ref &&
          node.type === "AGENT_EXECUTION" &&
          node.state === "RUNNING" &&
          node.turnRef === execution.turnRef &&
          node.attempt === execution.attempt,
      ),
    );
  });
  const active = activeTranscriptItemId(bound);
  if (active) return active;
  const closed = bound.flatMap((item) => {
    const scope = executionKey(item.execution);
    return scope &&
      !item.toolCall &&
      !item.artifact &&
      (item.phase === "FINAL" || terminalTranscriptStates.has(item.state ?? ""))
      ? [scope]
      : [];
  });
  // FINAL и terminal остаются в наборе: общий предикат закрывает exact scope.
  return activeTranscriptItemId(
    bound.filter(
      (item) =>
        !item.toolCall &&
        !item.artifact &&
        !item.artifactRef &&
        (Boolean(item.phase) || isTranscriptService(item)),
    ),
    closed,
  );
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
  const anchors = conversation?.turns.filter(
    (turn) => turn.role === "USER" && turn.runRef === run?.ref,
  );
  const anchor = anchors?.length === 1 ? anchors[0] : undefined;
  if (
    !conversation ||
    conversation.state !== "ACTIVE" ||
    !organizationRef ||
    !run ||
    !anchor ||
    !Number.isSafeInteger(anchor.runVersion) ||
    (anchor.runVersion ?? 0) < run.version ||
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
  closedExecutionKeys: readonly string[] = [],
): PresentedTranscriptItem[] {
  const successfulInvocations = new Set<string>();
  for (const item of items) {
    const scope = executionKey(item.execution);
    if (
      scope &&
      !item.historical &&
      item.kind === "tool" &&
      !item.phase &&
      item.eventType === "TOOL_CALL_RECORDED" &&
      item.messageKind === "TOOL_CALL" &&
      item.integrationInvocationRef &&
      item.toolCall &&
      Number.isSafeInteger(item.toolCall.revision) &&
      (item.toolCall.revision ?? 0) >= 2 &&
      isSuccessfulIntegrationToolReceipt(item.toolCall)
    )
      successfulInvocations.add(
        JSON.stringify([scope, item.integrationInvocationRef]),
      );
  }
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
  const presented: PresentedTranscriptItem[] = items.flatMap((item, index) => {
    // Общая отмена Run не доказывает turn/attempt: сохраняем отдельно в details.
    if (isUnboundRunCancellation(item))
      return [
        {
          ...item,
          working: false,
          serviceHistory: [item],
        },
      ];
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
  const finals = new Map<string, PresentedTranscriptItem[]>();
  // Типизированный итог агрегата не становится вторым ответом того же хода.
  // Разные nodeRef сохраняются внутри истории, а остальные pins совпадают точно.
  const foldedRoots = new Set<string>();
  for (const root of presented) {
    const history = root.serviceHistory;
    if (
      root.working ||
      root.progress?.trim() ||
      root.executionNodeType !== "ROOT_PROCESS" ||
      root.serviceCompletionCode !== "ROOT_PROCESS_COMPLETED" ||
      !history ||
      history.some(
        (step) =>
          terminalTranscriptStates.has(step.state ?? "") &&
          step.serviceCompletionCode !== "ROOT_PROCESS_COMPLETED",
      ) ||
      !root.execution
    )
      continue;
    const targets = presented.filter(
      (entry) =>
        entry.executionNodeType === "AGENT_EXECUTION" &&
        !entry.historical &&
        !entry.working &&
        entry.state === root.state &&
        (root.executionSafeErrorCode === undefined ||
          entry.executionSafeErrorCode === root.executionSafeErrorCode) &&
        terminalTranscriptStates.has(entry.state ?? "") &&
        entry.execution &&
        entry.execution.runRef === root.execution?.runRef &&
        entry.execution.sessionRef === root.execution.sessionRef &&
        entry.execution.turnRef === root.execution.turnRef &&
        entry.execution.turnNumber === root.execution.turnNumber &&
        entry.execution.attempt === root.execution.attempt &&
        (entry.phase === "FINAL" ||
          entry.serviceHistory?.some(
            (step) =>
              step.eventType === "TURN_COMPLETED" &&
              step.messageKind === "FINAL_MESSAGE",
          )),
    );
    const target = targets.length === 1 ? targets[0] : undefined;
    if (!target) continue;
    const combined = [
      ...(target.serviceHistory ?? target.completedServiceHistory ?? []),
      ...history,
    ].sort(
      (left, right) =>
        Date.parse(left.occurredAt) - Date.parse(right.occurredAt) ||
        (left.sequence ?? 0) - (right.sequence ?? 0) ||
        left.id.localeCompare(right.id),
    );
    if (target.serviceHistory) target.serviceHistory = combined;
    else target.completedServiceHistory = combined;
    foldedRoots.add(root.id);
  }
  for (const item of presented) {
    const key = executionKey(item.execution);
    if (!key || item.historical || item.phase !== "FINAL") continue;
    finals.set(key, [...(finals.get(key) ?? []), item]);
  }
  const attached = new Map<string, readonly RunActivityItem[]>();
  const hidden = new Set<string>();
  for (const item of presented) {
    const key = executionKey(item.execution);
    const target = key ? finals.get(key) : undefined;
    if (
      !item.serviceHistory ||
      item.working ||
      item.historical ||
      ["FAILED", "CANCELLED"].includes(item.state ?? "") ||
      target?.length !== 1
    )
      continue;
    const final = target[0];
    if (!final) continue;
    attached.set(final.id, item.serviceHistory);
    hidden.add(item.id);
  }
  return presented
    .filter((item) => {
      if (foldedRoots.has(item.id)) return false;
      if (hidden.has(item.id)) return false;
      const scope = executionKey(item.execution);
      // Квитанция инструмента уже показывает этот exact успешный результат.
      // Сервер назначает привязку только integration completion, не по тексту.
      // Без exact успешной квитанции ошибки и неизвестный исход не скрываются.
      if (
        scope &&
        !item.historical &&
        item.kind === "system" &&
        item.eventType === "TURN_PROGRESS" &&
        item.messageKind === "INTERMEDIATE_MESSAGE" &&
        !item.phase &&
        !item.progress?.trim() &&
        !item.toolCall &&
        !item.artifact &&
        !item.artifactRef &&
        item.integrationInvocationRef &&
        successfulInvocations.has(
          JSON.stringify([scope, item.integrationInvocationRef]),
        )
      )
        return false;
      // До FINAL пустая successful квитанция не образует отдельный пузырь.
      // История остаётся в исходных events и присоединяется к FINAL при rejoin.
      return !(
        scope &&
        closedExecutionKeys.includes(scope) &&
        item.serviceHistory &&
        !item.working &&
        item.state === "SUCCEEDED" &&
        !item.summary?.trim() &&
        !item.progress?.trim() &&
        item.serviceHistory.every((step) =>
          [step.summary, step.progress].every((value) => {
            const code = value?.trim().replace(/^i18n:/, "");
            return (
              !code ||
              serviceStartEvents.has(code) ||
              serviceProgressCodes.has(code) ||
              code === "RUN_STARTED"
            );
          }),
        )
      );
    })
    .map((item) => {
      const completedServiceHistory = attached.get(item.id);
      return completedServiceHistory
        ? { ...item, completedServiceHistory }
        : item;
    });
}

export function isUnboundRunCancellation(item: RunActivityItem): boolean {
  return Boolean(
    item.historical &&
    !item.execution &&
    item.kind === "system" &&
    item.eventType === "RUN_STATE_CHANGED" &&
    item.messageKind === "STATE" &&
    item.state === "CANCELLED" &&
    !item.phase &&
    !item.toolCall &&
    !item.artifact &&
    !item.artifactRef &&
    !item.progress?.trim() &&
    !item.integrationInvocationRef &&
    transcriptServiceCancellationCode(item.serviceCancellationCode),
  );
}

export function assistantTurnIsEmptyTerminalReceipt(
  turn: AssistantTurn,
  conversation: AssistantConversation | undefined,
  organizationRef: string | undefined,
  run: Run | undefined,
  nodes: readonly RunNode[],
  events: readonly RunEvent[],
): boolean {
  return Boolean(
    turn.role === "ASSISTANT" &&
    turn.state === "COMPLETED" &&
    !turn.plan &&
    !turn.content.trim() &&
    run &&
    turn.runRef === run.ref &&
    Number.isSafeInteger(turn.runVersion) &&
    (turn.runVersion ?? 0) >= run.version &&
    conversation?.turns.some(
      (item) =>
        item.ref === turn.ref &&
        item.runRef === turn.runRef &&
        item.runVersion === turn.runVersion &&
        item.role === turn.role &&
        item.state === turn.state &&
        item.content === turn.content &&
        !item.plan,
    ) &&
    assistantTerminalTranscriptScopes(
      conversation,
      organizationRef,
      run,
      nodes,
      events,
    ).length > 0,
  );
}

// Отказ до запуска runner уже показан exact terminal событием узла.
// Убирается только его машинная квитанция, не самостоятельный ответ агента.
export function assistantTurnIsDuplicateFailureReceipt(
  turn: AssistantTurn,
  conversation: AssistantConversation | undefined,
  organizationRef: string | undefined,
  run: Run | undefined,
  nodes: readonly RunNode[],
  events: readonly RunEvent[],
): boolean {
  if (
    !run ||
    run.state !== "FAILED" ||
    run.safeErrorCode !== "RUNTIME_INPUT_INVALID" ||
    !run.safeErrorMessage ||
    turn.role !== "ASSISTANT" ||
    turn.state !== "FAILED" ||
    turn.plan ||
    turn.runRef !== run.ref ||
    turn.content !== run.safeErrorMessage ||
    !Number.isSafeInteger(turn.runVersion) ||
    (turn.runVersion ?? 0) < run.version ||
    !conversation?.turns.some(
      (item) =>
        item.ref === turn.ref &&
        item.runRef === turn.runRef &&
        item.runVersion === turn.runVersion &&
        item.role === turn.role &&
        item.state === turn.state &&
        item.content === turn.content &&
        !item.plan,
    )
  )
    return false;
  const scopes = assistantTerminalTranscriptScopes(
    conversation,
    organizationRef,
    run,
    nodes,
    events,
  );
  return events.some((event) => {
    const scope = executionKey(event.execution);
    return Boolean(
      scope &&
      scopes.includes(scope) &&
      event.type === "NODE_STATE_CHANGED" &&
      event.runState === "FAILED" &&
      event.nodeState === "FAILED" &&
      nodes.some(
        (node) =>
          node.ref === event.execution?.nodeRef && node.state === "FAILED",
      ),
    );
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
    graphRootRunRef: run.rootRunRef,
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
