import type {
  TelegramSubscription,
  TelegramSubscriptionInput,
  InteractionDeliveryView,
} from "@/shared/api/generated/openapi/types.gen";

export type NotificationEvent = TelegramSubscription["eventTypes"][number];
export const notificationEvents: readonly NotificationEvent[] = [
  "FINISHED",
  "FAILED",
  "HUMAN_GATE",
];
export type ConnectionChoice = { ref: string; name: string };
const reference = /^[A-Za-z0-9_-]{8,128}$/;
const subscriptionStates = [
  "ACTIVE",
  "DISABLED",
  "EXPIRED",
  "EXHAUSTED",
  "SCOPE_REVOKED",
];
const deliveryStates = [
  "WAITING_APPROVAL",
  "DUE",
  "CLAIMED",
  "SUCCEEDED",
  "FAILED",
  "CANCELLED",
  "UNKNOWN_OUTCOME",
];
const validVersion = (value: number) =>
  Number.isSafeInteger(value) && value > 0;
const timestamp = (value: string) =>
  typeof value === "string" && Number.isFinite(Date.parse(value));

export function validEvents(value: unknown): value is NotificationEvent[] {
  return (
    Array.isArray(value) &&
    value.length > 0 &&
    value.length <= 3 &&
    new Set(value).size === value.length &&
    value.every((event: unknown) =>
      notificationEvents.includes(event as NotificationEvent),
    )
  );
}

// Ответ заново проверяется перед сохранением UI state. Будущие private поля не копируются.
export function subscriptionView(
  value: TelegramSubscription | null | undefined,
  project: string,
): TelegramSubscription {
  if (
    !value ||
    !reference.test(value.ref) ||
    value.projectRef !== project ||
    !reference.test(value.connectionRef) ||
    !validVersion(value.version) ||
    typeof value.enabled !== "boolean" ||
    typeof value.canManage !== "boolean" ||
    !subscriptionStates.includes(value.state) ||
    !validEvents(value.eventTypes) ||
    !/^[a-f0-9]{64}$/.test(value.scopeDigest) ||
    !Number.isSafeInteger(value.botId) ||
    value.botId < 1 ||
    value.botId > 4503599627370495 ||
    !/^…-?[0-9]{1,8}$/.test(value.chatDisplay) ||
    !/^(NONE|[1-9][0-9]{0,9})$/.test(value.topicDisplay) ||
    !timestamp(value.expiresAt) ||
    !timestamp(value.updatedAt) ||
    !Number.isInteger(value.maxEffects) ||
    value.maxEffects < 1 ||
    value.maxEffects > 10000 ||
    !Number.isInteger(value.usedEffects) ||
    value.usedEffects < 0 ||
    value.usedEffects > value.maxEffects
  )
    throw new Error("Telegram subscription response is invalid");
  return {
    ref: value.ref,
    projectRef: value.projectRef,
    connectionRef: value.connectionRef,
    version: value.version,
    enabled: value.enabled,
    canManage: value.canManage,
    state: value.state,
    eventTypes: [...value.eventTypes],
    scopeDigest: value.scopeDigest,
    botId: value.botId,
    chatDisplay: value.chatDisplay,
    topicDisplay: value.topicDisplay,
    expiresAt: value.expiresAt,
    updatedAt: value.updatedAt,
    maxEffects: value.maxEffects,
    usedEffects: value.usedEffects,
  };
}

export function deliveryView(
  value: InteractionDeliveryView | null | undefined,
): InteractionDeliveryView {
  if (
    !value ||
    !reference.test(value.ref) ||
    !reference.test(value.connectionRef) ||
    !reference.test(value.runRef) ||
    !validVersion(value.version) ||
    !deliveryStates.includes(value.state) ||
    (value.eventType !== "" && !notificationEvents.includes(value.eventType)) ||
    !Number.isInteger(value.attempt) ||
    value.attempt < 0 ||
    value.attempt > 10 ||
    typeof value.retryable !== "boolean" ||
    (value.retryable && value.state !== "FAILED") ||
    !/^[A-Z0-9_]{0,96}$/.test(value.safeErrorCode) ||
    !timestamp(value.createdAt) ||
    !timestamp(value.availableAt) ||
    (value.completedAt !== undefined && !timestamp(value.completedAt)) ||
    (value.gateRef !== "" && !reference.test(value.gateRef)) ||
    (value.receiptRef !== "" && !reference.test(value.receiptRef))
  )
    throw new Error("Interaction delivery response is invalid");
  return {
    ref: value.ref,
    version: value.version,
    connectionRef: value.connectionRef,
    runRef: value.runRef,
    gateRef: value.gateRef,
    eventType: value.eventType,
    state: value.state,
    attempt: value.attempt,
    retryable: value.retryable,
    safeErrorCode: value.safeErrorCode,
    createdAt: value.createdAt,
    availableAt: value.availableAt,
    completedAt: value.completedAt,
    receiptRef: value.receiptRef,
  };
}

export function enableProposal(
  connectionRef: string,
  events: NotificationEvent[],
  hours: number,
  maxEffects: number,
): TelegramSubscriptionInput {
  if (
    !reference.test(connectionRef) ||
    !validEvents(events) ||
    !Number.isInteger(hours) ||
    hours < 1 ||
    hours > 168 ||
    !Number.isInteger(maxEffects) ||
    maxEffects < 1 ||
    maxEffects > 10000
  )
    throw new Error("Telegram subscription proposal is invalid");
  return {
    enabled: true,
    connectionRef,
    eventTypes: [...events],
    durationSeconds: hours * 3600,
    maxEffects,
  };
}
