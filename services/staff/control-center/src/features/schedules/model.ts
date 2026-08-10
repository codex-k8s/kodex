import type {
  BindScheduleConfiguration,
  OwnerScheduleView,
  Resource,
  ScheduleOccurrence,
  ScheduleSelectorCatalog,
  ScheduleCalendar,
  ScheduleDeliveryPolicy,
  ScheduleMisfirePolicy,
  ScheduleNotificationPolicy,
  ScheduleOverlapPolicy,
  ScheduleSessionPolicy,
} from "@/shared/api/generated/openapi/types.gen";

export type ScheduleLifecycleState =
  | "ACTIVE"
  | "PAUSED"
  | "ARCHIVED"
  | "DELETION_PENDING"
  | "DELETED";
export type ScheduleNextAction =
  | "UPDATE"
  | "RUN_NOW"
  | "PAUSE"
  | "RESUME"
  | "DELETE"
  | "VIEW_OCCURRENCES";
export type ScheduleRecoveryAction = "REPAIR" | "CANCEL" | "SKIP";

export interface ScheduleSelectionModel {
  ref: string;
  kind: "AGENT" | "INSTRUCTION_SET" | "PROVIDER_POOL";
  stableKey: string;
  displayName: string;
  state: string;
}

export interface ScheduleCatalogModel {
  selectors: ScheduleSelectionModel[];
  presets: Array<{
    key: string;
    displayName: string;
    description: string;
  }>;
  defaults: {
    calendar: ScheduleCalendar;
    overlapPolicy: ScheduleOverlapPolicy;
    misfirePolicy: ScheduleMisfirePolicy;
    deliveryPolicy: ScheduleDeliveryPolicy;
    maximumAttempts: number;
    initialBackoffSeconds: number;
    maximumBackoffSeconds: number;
    deadLetterAfterSeconds: number;
    sessionPolicy: ScheduleSessionPolicy;
    notificationPolicy: ScheduleNotificationPolicy;
    maximumExecutionSeconds: number;
    coalesce: boolean;
  };
}

export interface ScheduleResourceOption {
  id: string;
  name: string;
  stableKey?: string;
  mediaType?: string;
  scanStatus?: string;
}

interface ScheduleBoundSelection {
  selector: string;
  displayName: string;
}

interface SchedulePromptModel {
  kind: "INLINE" | "SELECTOR";
  inlineMarkdown?: string;
  artifactSelector?: string;
  displayName: string;
}

export interface ScheduleView {
  scheduleRef: string;
  displayName: string;
  version: number;
  state: ScheduleLifecycleState;
  presetKey: string;
  timezone: string;
  cron?: string;
  intervalSeconds?: number;
  advancedOverrides: string[];
  calendar: ScheduleCalendar;
  overlapPolicy: ScheduleOverlapPolicy;
  misfirePolicy: ScheduleMisfirePolicy;
  misfireGraceSeconds: number;
  deliveryPolicy: ScheduleDeliveryPolicy;
  maximumAttempts: number;
  initialBackoffSeconds: number;
  maximumBackoffSeconds: number;
  deadLetterAfterSeconds: number;
  sessionPolicy: ScheduleSessionPolicy;
  notificationPolicy: ScheduleNotificationPolicy;
  maximumExecutionSeconds: number;
  coalesce: boolean;
  nextRunAt?: string;
  agentSelection: ScheduleBoundSelection;
  instructionSelection: ScheduleBoundSelection;
  providerPoolSelection: ScheduleBoundSelection;
  roomSelection: ScheduleBoundSelection;
  prompt: SchedulePromptModel;
  nextActions: ScheduleNextAction[];
}

export interface ScheduleOccurrenceModel {
  occurrenceId: string;
  scheduleId: string;
  scheduledFor: string;
  state: string;
  attempt: number;
  version: number;
  recoveryEvidenceSha256?: string;
  recoveryActions: ScheduleRecoveryAction[];
}

export interface ScheduleDraft {
  name: string;
  agentStableKey: string;
  instructionSetStableKey: string;
  providerPoolStableKey: string;
  presetKey: string;
  timezone: string;
  promptKind: "INLINE" | "SELECTOR";
  inlineMarkdown: string;
  artifactSelector: string;
  roomStableKey: string;
  advanced: boolean;
  cron: string;
  intervalSeconds: number;
  maximumAttempts: number;
  calendar: ScheduleCalendar;
  overlapPolicy: ScheduleOverlapPolicy;
  misfirePolicy: ScheduleMisfirePolicy;
  misfireGraceSeconds: number;
  deliveryPolicy: ScheduleDeliveryPolicy;
  initialBackoffSeconds: number;
  maximumBackoffSeconds: number;
  deadLetterAfterSeconds: number;
  sessionPolicy: ScheduleSessionPolicy;
  notificationPolicy: ScheduleNotificationPolicy;
  coalesce: boolean;
  maximumExecutionSeconds: number;
}

export function buildScheduleCommand(
  draft: ScheduleDraft,
): BindScheduleConfiguration {
  const prompt =
    draft.promptKind === "INLINE"
      ? { inlineMarkdown: draft.inlineMarkdown }
      : { artifactSelector: draft.artifactSelector };
  const advancedOverrides = draft.advanced
    ? {
        ...(draft.cron
          ? { cron: draft.cron }
          : draft.intervalSeconds > 0
            ? { intervalSeconds: draft.intervalSeconds }
            : {}),
        ...(draft.maximumAttempts > 0
          ? { maximumAttempts: draft.maximumAttempts }
          : {}),
        calendar: draft.calendar,
        overlapPolicy: draft.overlapPolicy,
        misfirePolicy: draft.misfirePolicy,
        misfireGraceSeconds: draft.misfireGraceSeconds,
        deliveryPolicy: draft.deliveryPolicy,
        initialBackoffSeconds: draft.initialBackoffSeconds,
        maximumBackoffSeconds: draft.maximumBackoffSeconds,
        deadLetterAfterSeconds: draft.deadLetterAfterSeconds,
        sessionPolicy: draft.sessionPolicy,
        notificationPolicy: draft.notificationPolicy,
        coalesce: draft.coalesce,
        ...(draft.maximumExecutionSeconds > 0
          ? { maximumExecutionSeconds: draft.maximumExecutionSeconds }
          : {}),
      }
    : undefined;
  return {
    name: draft.name,
    agentStableKey: draft.agentStableKey,
    instructionSetStableKey: draft.instructionSetStableKey,
    providerPoolStableKey: draft.providerPoolStableKey,
    intent: {
      timezone: draft.timezone,
      presetKey: draft.presetKey,
      prompt,
      ...(draft.roomStableKey ? { roomStableKey: draft.roomStableKey } : {}),
      ...(advancedOverrides ? { advancedOverrides } : {}),
    },
  };
}

export const toScheduleView = (value: OwnerScheduleView): ScheduleView => ({
  scheduleRef: value.scheduleRef,
  displayName: value.displayName,
  version: value.version,
  state: value.state as ScheduleLifecycleState,
  presetKey: value.presetKey,
  timezone: value.timezone,
  ...(value.cron ? { cron: value.cron } : {}),
  ...(value.intervalSeconds !== undefined
    ? { intervalSeconds: value.intervalSeconds }
    : {}),
  advancedOverrides: [...value.advancedOverrides],
  calendar: value.calendar,
  overlapPolicy: value.overlapPolicy,
  misfirePolicy: value.misfirePolicy,
  misfireGraceSeconds: value.misfireGraceSeconds,
  deliveryPolicy: value.deliveryPolicy,
  maximumAttempts: value.maximumAttempts,
  initialBackoffSeconds: value.initialBackoffSeconds,
  maximumBackoffSeconds: value.maximumBackoffSeconds,
  deadLetterAfterSeconds: value.deadLetterAfterSeconds,
  sessionPolicy: value.sessionPolicy,
  notificationPolicy: value.notificationPolicy,
  maximumExecutionSeconds: value.maximumExecutionSeconds,
  coalesce: value.coalesce,
  ...(value.nextRunAt ? { nextRunAt: value.nextRunAt } : {}),
  agentSelection: {
    selector: value.agentSelection.selector,
    displayName: value.agentSelection.displayName,
  },
  instructionSelection: {
    selector: value.instructionSelection.selector,
    displayName: value.instructionSelection.displayName,
  },
  providerPoolSelection: {
    selector: value.providerPoolSelection.selector,
    displayName: value.providerPoolSelection.displayName,
  },
  roomSelection: {
    selector: value.roomSelection.selector,
    displayName: value.roomSelection.displayName,
  },
  prompt: {
    kind: value.prompt.kind,
    ...(value.prompt.inlineMarkdown
      ? { inlineMarkdown: value.prompt.inlineMarkdown }
      : {}),
    ...(value.prompt.artifactSelector
      ? { artifactSelector: value.prompt.artifactSelector }
      : {}),
    displayName: value.prompt.displayName,
  },
  nextActions: [...value.nextActions],
});

export const toScheduleOccurrenceModel = (
  value: ScheduleOccurrence,
): ScheduleOccurrenceModel => ({
  occurrenceId: value.occurrenceId,
  scheduleId: value.scheduleId,
  scheduledFor: value.scheduledFor,
  state: value.state,
  attempt: value.attempt,
  version: value.version,
  ...(value.recoveryEvidenceSha256
    ? { recoveryEvidenceSha256: value.recoveryEvidenceSha256 }
    : {}),
  recoveryActions: [...value.recoveryActions],
});

export const toScheduleCatalogModel = (
  value: ScheduleSelectorCatalog,
): ScheduleCatalogModel => ({
  selectors: value.selectors.map((item) => ({
    ref: item.ref,
    kind: item.kind,
    stableKey: item.stableKey,
    displayName: item.displayName,
    state: item.state,
  })),
  presets: value.presets.map((item) => ({
    key: item.key,
    displayName: item.displayName,
    description: item.description,
  })),
  defaults: {
    calendar: value.defaults.calendar,
    overlapPolicy: value.defaults.overlapPolicy,
    misfirePolicy: value.defaults.misfirePolicy,
    deliveryPolicy: value.defaults.deliveryPolicy,
    maximumAttempts: value.defaults.maximumAttempts,
    initialBackoffSeconds: value.defaults.initialBackoffSeconds,
    maximumBackoffSeconds: value.defaults.maximumBackoffSeconds,
    deadLetterAfterSeconds: value.defaults.deadLetterAfterSeconds,
    sessionPolicy: value.defaults.sessionPolicy,
    notificationPolicy: value.defaults.notificationPolicy,
    maximumExecutionSeconds: value.defaults.maximumExecutionSeconds,
    coalesce: value.defaults.coalesce,
  },
});

export const toScheduleResourceOption = (
  value: Resource,
): ScheduleResourceOption => ({
  id: value.id,
  name: value.name,
  ...(value.spec.chat?.stableKey
    ? { stableKey: value.spec.chat.stableKey }
    : {}),
  ...(value.spec.artifact?.mediaType
    ? { mediaType: value.spec.artifact.mediaType }
    : {}),
  ...(value.spec.artifact?.scanStatus
    ? { scanStatus: value.spec.artifact.scanStatus }
    : {}),
});
