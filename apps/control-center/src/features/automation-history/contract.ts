export type AutomationRunStatus =
  | "queued"
  | "running"
  | "waiting_owner"
  | "succeeded"
  | "failed";
export type AutomationRunOutcome =
  | ""
  | "no_action"
  | "action_taken"
  | "requires_human"
  | "failed";
export type HumanDecisionStatus = "open" | "resolved";
export type DeliveryStatus = "pending" | "delivered" | "not_required";
export type AutomationNextAction =
  | "retry_same_callback"
  | "wait_for_owner_response"
  | "none";

// Поля повторяют structured output `mattermost_complete_automation`; UI не принимает агентское summary.
export interface AutomationCallbackReceipt {
  schedule_run_id: string;
  status: AutomationRunStatus;
  outcome: AutomationRunOutcome;
  duplicate: boolean;
  owner_attention_id?: number;
  human_decision_status?: HumanDecisionStatus;
  delivery_status?: DeliveryStatus;
  next_action?: AutomationNextAction;
}

const scheduledRunPattern = /^scheduled-run-[a-f0-9]{32}$/;
const statuses = new Set<AutomationRunStatus>([
  "queued",
  "running",
  "waiting_owner",
  "succeeded",
  "failed",
]);
const outcomes = new Set<AutomationRunOutcome>([
  "",
  "no_action",
  "action_taken",
  "requires_human",
  "failed",
]);
const decisions = new Set<HumanDecisionStatus>(["open", "resolved"]);
const deliveries = new Set<DeliveryStatus>([
  "pending",
  "delivered",
  "not_required",
]);
const actions = new Set<AutomationNextAction>([
  "retry_same_callback",
  "wait_for_owner_response",
  "none",
]);

export function parseAutomationCallbackReceipts(
  input: unknown,
): AutomationCallbackReceipt[] {
  if (!Array.isArray(input)) {
    return [];
  }
  return input.flatMap((item) => {
    if (!isRecord(item)) return [];
    const scheduleRunID = item.schedule_run_id;
    const status = item.status;
    const outcome = item.outcome;
    if (
      typeof scheduleRunID !== "string" ||
      !scheduledRunPattern.test(scheduleRunID) ||
      typeof status !== "string" ||
      !statuses.has(status as AutomationRunStatus) ||
      typeof outcome !== "string" ||
      !outcomes.has(outcome as AutomationRunOutcome) ||
      typeof item.duplicate !== "boolean"
    ) {
      return [];
    }
    const receipt: AutomationCallbackReceipt = {
      schedule_run_id: scheduleRunID,
      status: status as AutomationRunStatus,
      outcome: outcome as AutomationRunOutcome,
      duplicate: item.duplicate,
    };
    if (
      typeof item.owner_attention_id === "number" &&
      Number.isSafeInteger(item.owner_attention_id) &&
      item.owner_attention_id > 0
    ) {
      receipt.owner_attention_id = item.owner_attention_id;
    }
    if (
      typeof item.human_decision_status === "string" &&
      decisions.has(item.human_decision_status as HumanDecisionStatus)
    ) {
      receipt.human_decision_status =
        item.human_decision_status as HumanDecisionStatus;
    }
    if (
      typeof item.delivery_status === "string" &&
      deliveries.has(item.delivery_status as DeliveryStatus)
    ) {
      receipt.delivery_status = item.delivery_status as DeliveryStatus;
    }
    if (
      typeof item.next_action === "string" &&
      actions.has(item.next_action as AutomationNextAction)
    ) {
      receipt.next_action = item.next_action as AutomationNextAction;
    }
    return [receipt];
  });
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}
