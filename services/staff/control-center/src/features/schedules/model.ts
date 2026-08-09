import type {
  BindScheduleConfiguration,
  OwnerScheduleView,
  ScheduleCalendar,
  ScheduleDeliveryPolicy,
  ScheduleMisfirePolicy,
  ScheduleNotificationPolicy,
  ScheduleOverlapPolicy,
  ScheduleSessionPolicy,
} from "@/shared/api/generated/openapi/types.gen";

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

export type ScheduleView = OwnerScheduleView;
