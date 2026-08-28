import type {
  Schedule,
  ScheduleInput,
} from "@/shared/api/generated/openapi/types.gen";
import { AppProblem } from "@/shared/api/problem";

export function scheduleInput(schedule: Schedule): ScheduleInput {
  const targetType = schedule.target.type;
  if (!isSchedulePreset(schedule.preset))
    throw new AppProblem({
      status: 502,
      code: "SCHEDULE_PRESET_UNSUPPORTED",
      retryable: false,
      kind: "unavailable",
    });
  const dayOfWeek = schedule.dayOfWeek || undefined;
  return {
    name: schedule.name,
    targetRef: schedule.target.ref,
    targetType,
    preset: schedule.preset,
    timeOfDay: schedule.timeOfDay ?? "00:00",
    ...(dayOfWeek ? { dayOfWeek } : {}),
    timezone: schedule.timezone,
    input: { ...(schedule.input ?? {}) },
    sessionPolicy: schedule.sessionPolicy,
    notificationPolicy: schedule.notificationPolicy,
  };
}

export function isSchedulePreset(
  value: string,
): value is ScheduleInput["preset"] {
  return ["HOURLY", "DAILY", "WEEKDAYS", "WEEKLY"].includes(value);
}

function canonicalJson(value: unknown): string {
  function canonical(current: unknown): unknown {
    if (Array.isArray(current)) return current.map(canonical);
    if (typeof current !== "object" || current === null) return current;
    return Object.fromEntries(
      Object.entries(current)
        .sort(([left], [right]) => left.localeCompare(right))
        .map(([key, item]) => [key, canonical(item)]),
    );
  }
  return JSON.stringify(canonical(value));
}

function sameInput(left: ScheduleInput, right: ScheduleInput): boolean {
  return (
    left.name === right.name &&
    left.targetRef === right.targetRef &&
    left.targetType === right.targetType &&
    left.preset === right.preset &&
    left.timeOfDay === right.timeOfDay &&
    (left.dayOfWeek ?? "") === (right.dayOfWeek ?? "") &&
    left.timezone === right.timezone &&
    left.sessionPolicy === right.sessionPolicy &&
    left.notificationPolicy === right.notificationPolicy &&
    canonicalJson(left.input) === canonicalJson(right.input)
  );
}

export function verifyScheduleReadback(
  submitted: ScheduleInput,
  mutationResult: Schedule,
  readback: Schedule | undefined,
): Schedule {
  if (
    !readback ||
    readback.ref !== mutationResult.ref ||
    readback.version < mutationResult.version ||
    !sameInput(scheduleInput(readback), submitted)
  )
    throw new AppProblem({
      status: 502,
      code: "SCHEDULE_READBACK_MISMATCH",
      retryable: true,
      kind: "unavailable",
    });
  return readback;
}

export function verifyScheduleCommandReadback(
  mutationResult: Schedule,
  readback: Schedule | undefined,
): Schedule {
  if (
    !readback ||
    readback.ref !== mutationResult.ref ||
    readback.version < mutationResult.version ||
    readback.state !== mutationResult.state
  )
    throw new AppProblem({
      status: 502,
      code: "SCHEDULE_READBACK_MISMATCH",
      retryable: true,
      kind: "unavailable",
    });
  return readback;
}
