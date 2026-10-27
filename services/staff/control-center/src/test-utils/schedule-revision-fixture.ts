import type {
  ScheduleInput,
  ScheduleRevision,
} from "@/shared/api/generated/openapi/types.gen";

// Создаёт synthetic ревизию с переданными входом, номером и digest для теста.
export function scheduleRevisionFixture(
  input: ScheduleInput,
  value: number,
  digest: string,
): ScheduleRevision {
  return {
    ref: `schedule_revision_${String(value)}`,
    revision: value,
    digest,
    targetVersion: 2,
    targetDigest: "b".repeat(64),
    name: input.name,
    target: {
      type: "AGENT",
      ref: input.targetRef,
      displayName: "Аналитик продаж",
      version: 2,
    },
    preset: input.preset,
    cronExpression: "0 9 * * *",
    timezone: input.timezone,
    input: input.input,
    sessionPolicy: input.sessionPolicy,
    notificationPolicy: input.notificationPolicy,
    dstGapPolicy: "SHIFT_FORWARD",
    dstFoldPolicy: "RUN_ONCE_EARLIEST",
    misfirePolicy: "COALESCE",
    overlapPolicy: "FORBID",
    automationText: "",
    promptInputs: {},
    createdAt: "2026-08-30T06:00:00Z",
  };
}
